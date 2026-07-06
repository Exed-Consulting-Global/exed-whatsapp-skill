package wa

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"

	"github.com/skip2/go-qrcode"
)

// Client wraps a whatsmeow client + the message store, and owns the pairing
// state machine. All exported methods are safe for concurrent use.
type Client struct {
	wm        *whatsmeow.Client
	store     *MessageStore
	container *sqlstore.Container
	logger    waLog.Logger
	baseDir   string

	// mu guards the pairing/connection state machine below. It must NOT be held
	// while blocking on network I/O (Connect, GetQRChannel reads); we snapshot
	// what we need, release, then act.
	mu       sync.Mutex
	pairing  bool // a QR pairing flow is currently in progress
	lastCode string
}

// DataDir resolves the absolute base directory for all persistent state.
// Claude Desktop launches the binary with an arbitrary cwd, so we anchor to the
// user's config dir: <UserConfigDir>/ExedWhatsApp/store.
func DataDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("cannot resolve user config dir: %w", err)
	}
	return filepath.Join(base, "ExedWhatsApp", "store"), nil
}

// New builds the whatsmeow client from the sqlstore device and opens the
// message store, all rooted at an absolute data dir. It registers the event
// handler but does NOT connect — call Start for that.
func New(ctx context.Context) (*Client, error) {
	baseDir, err := DataDir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create store dir %q: %w", baseDir, err)
	}

	// Log to stderr only. stdout is the MCP transport and must stay clean JSON-RPC.
	logger := waLog.Stdout("WA", "INFO", true)
	dbLog := waLog.Stdout("WADB", "INFO", true)

	// whatsapp.db holds the whatsmeow device/session; absolute path.
	waDBPath := filepath.Join(baseDir, "whatsapp.db")
	container, err := sqlstore.New(ctx, "sqlite3", "file:"+waDBPath+"?_foreign_keys=on", dbLog)
	if err != nil {
		return nil, fmt.Errorf("failed to open whatsapp.db: %w", err)
	}

	deviceStore, err := container.GetFirstDevice(ctx)
	if err != nil {
		if err == sql.ErrNoRows {
			deviceStore = container.NewDevice()
			logger.Infof("Created new device")
		} else {
			return nil, fmt.Errorf("failed to get device: %w", err)
		}
	}

	wm := whatsmeow.NewClient(deviceStore, logger)
	if wm == nil {
		return nil, fmt.Errorf("failed to create whatsmeow client")
	}

	store, err := NewMessageStore(baseDir)
	if err != nil {
		return nil, fmt.Errorf("failed to open message store: %w", err)
	}

	c := &Client{
		wm:        wm,
		store:     store,
		container: container,
		logger:    logger,
		baseDir:   baseDir,
	}
	c.registerHandlers()
	return c, nil
}

// registerHandlers wires message/history-sync/connection events into the store.
// Message CONTENT is treated as data only; nothing here interprets it.
func (c *Client) registerHandlers() {
	c.wm.AddEventHandler(func(evt interface{}) {
		switch v := evt.(type) {
		case *events.Message:
			c.handleMessage(v)
		case *events.HistorySync:
			c.handleHistorySync(v)
		case *events.Connected:
			c.logger.Infof("Connected to WhatsApp")
		case *events.LoggedOut:
			c.logger.Warnf("Device logged out; call conectar_whatsapp to re-pair")
		}
	})
}

// Store exposes the message store to the read-query layer.
func (c *Client) Store() *MessageStore { return c.store }

// Start connects if already paired; if not paired it stays offline (pairing is
// driven later by ConnectWithQR / the conectar_whatsapp tool). Non-blocking.
func (c *Client) Start(ctx context.Context) error {
	if c.wm.Store.ID != nil {
		// Already paired — connect normally.
		if err := c.wm.Connect(); err != nil {
			return fmt.Errorf("connect failed: %w", err)
		}
		return nil
	}
	// Not paired: do nothing now. The conectar_whatsapp tool issues the QR.
	c.logger.Infof("No stored session; awaiting pairing via conectar_whatsapp")
	return nil
}

// IsConnected reports the live socket state.
func (c *Client) IsConnected() bool { return c.wm.IsConnected() }

// IsLoggedIn reports whether we have a paired session (Store.ID set).
func (c *Client) IsLoggedIn() bool { return c.wm.Store.ID != nil }

// Close disconnects and closes the store.
func (c *Client) Close() {
	c.wm.Disconnect()
	_ = c.store.Close()
}

// QRResult is what ConnectWithQR returns to the MCP layer.
type QRResult struct {
	AlreadyConnected bool
	PNG              []byte // QR code PNG (only when a new code was issued)
	Code             string // raw QR string (for debugging/fallback)
}

// ConnectWithQR drives the pairing flow with full re-entrancy protection.
//
// Behavior:
//   - If already connected & logged in: returns {AlreadyConnected:true}.
//   - If a pairing flow is already running: returns an error telling the caller
//     to wait (prevents concurrent GetQRChannel calls, which whatsmeow forbids).
//   - Otherwise: obtains a fresh QR channel BEFORE Connect (as required), calls
//     Connect, waits for the first "code" event, renders it to PNG and returns.
//     A background goroutine keeps draining the channel; on "success" it clears
//     the pairing flag, on error/timeout it also clears the flag AND resets the
//     socket so a subsequent call can re-issue a QR.
//
// Re-issuing after expiry: whatsmeow's QR channel emits a fresh code on each
// rotation until it finally emits a terminal event (success/timeout/error) and
// closes. The background goroutine below caches the latest code in c.lastCode
// while the channel is alive; if the tool is called again while pairing is
// still in progress we surface that cached code instead of opening a second
// channel. Once the channel closes without success we Disconnect so the *next*
// tool call starts a clean flow (new GetQRChannel + Connect).
func (c *Client) ConnectWithQR(ctx context.Context) (*QRResult, error) {
	c.mu.Lock()

	// Fast paths under the lock.
	if c.wm.IsConnected() && c.wm.Store.ID != nil {
		c.mu.Unlock()
		return &QRResult{AlreadyConnected: true}, nil
	}
	if c.pairing {
		// A flow is already active. If we have a cached code, hand it back so
		// the user can scan the current QR; otherwise ask them to wait briefly.
		code := c.lastCode
		c.mu.Unlock()
		if code != "" {
			png, err := qrcode.Encode(code, qrcode.Medium, 512)
			if err != nil {
				return nil, fmt.Errorf("failed to render QR: %w", err)
			}
			return &QRResult{PNG: png, Code: code}, nil
		}
		return nil, fmt.Errorf("um pareamento já está em andamento; aguarde alguns segundos e tente novamente")
	}

	// If we already have a session but just aren't connected, a plain Connect
	// is enough — no QR needed.
	if c.wm.Store.ID != nil {
		c.mu.Unlock()
		if err := c.wm.Connect(); err != nil {
			return nil, fmt.Errorf("connect failed: %w", err)
		}
		return &QRResult{AlreadyConnected: true}, nil
	}

	// Begin a new pairing flow. GetQRChannel MUST be called before Connect.
	c.pairing = true
	c.lastCode = ""
	c.mu.Unlock()

	qrChan, err := c.wm.GetQRChannel(context.Background())
	if err != nil {
		// Most common cause: already logged in. In that case just Connect.
		c.mu.Lock()
		c.pairing = false
		c.mu.Unlock()
		if connErr := c.wm.Connect(); connErr != nil {
			return nil, fmt.Errorf("failed to get QR channel (%v) and connect (%v)", err, connErr)
		}
		return &QRResult{AlreadyConnected: true}, nil
	}

	if err := c.wm.Connect(); err != nil {
		c.mu.Lock()
		c.pairing = false
		c.mu.Unlock()
		return nil, fmt.Errorf("connect failed: %w", err)
	}

	// Channel to receive the first code (or a terminal event) synchronously.
	firstCode := make(chan string, 1)
	firstErr := make(chan error, 1)

	// Background goroutine owns the channel for its whole lifetime. It forwards
	// the first code to firstCode, then keeps caching subsequent codes and
	// watches for the terminal event.
	go func() {
		sentFirst := false
		for evt := range qrChan {
			switch evt.Event {
			case whatsmeow.QRChannelEventCode:
				c.mu.Lock()
				c.lastCode = evt.Code
				c.mu.Unlock()
				if !sentFirst {
					sentFirst = true
					firstCode <- evt.Code
				}
			case "success":
				c.mu.Lock()
				c.pairing = false
				c.lastCode = ""
				c.mu.Unlock()
				c.logger.Infof("Pairing successful")
				if !sentFirst {
					// Rare: paired without ever emitting a code.
					firstErr <- nil
				}
				return
			default:
				// "timeout", "error", or a pair error. Terminal.
				c.mu.Lock()
				c.pairing = false
				c.lastCode = ""
				c.mu.Unlock()
				c.logger.Warnf("Pairing ended without success: event=%s err=%v", evt.Event, evt.Error)
				// Reset the socket so the next ConnectWithQR starts fresh and can
				// obtain a new QR channel.
				c.wm.Disconnect()
				if !sentFirst {
					if evt.Error != nil {
						firstErr <- evt.Error
					} else {
						firstErr <- fmt.Errorf("pareamento encerrado (%s) sem sucesso", evt.Event)
					}
				}
				return
			}
		}
		// Channel closed without an explicit terminal case above.
		c.mu.Lock()
		if c.pairing {
			c.pairing = false
			c.lastCode = ""
		}
		c.mu.Unlock()
	}()

	// Wait for the first code (bounded) so the tool can return a QR image.
	select {
	case code := <-firstCode:
		png, err := qrcode.Encode(code, qrcode.Medium, 512)
		if err != nil {
			return nil, fmt.Errorf("failed to render QR: %w", err)
		}
		return &QRResult{PNG: png, Code: code}, nil
	case err := <-firstErr:
		if err == nil {
			return &QRResult{AlreadyConnected: true}, nil
		}
		return nil, err
	case <-time.After(30 * time.Second):
		return nil, fmt.Errorf("tempo esgotado aguardando o QR; tente conectar_whatsapp novamente")
	}
}

// Send sends a text or media message. mediaPath == "" means text-only.
// Ported from the bridge's sendWhatsAppMessage (context.Background() args match
// the pinned whatsmeow version). Returns (ok, humanMessage).
func (c *Client) Send(recipient, message, mediaPath string) (bool, string) {
	if !c.wm.IsConnected() {
		return false, "Não conectado ao WhatsApp. Chame conectar_whatsapp primeiro."
	}

	recipientJID, err := ResolveRecipient(recipient)
	if err != nil {
		return false, err.Error()
	}

	msg := &waProto.Message{}

	if mediaPath != "" {
		mediaData, err := os.ReadFile(mediaPath)
		if err != nil {
			return false, fmt.Sprintf("Erro ao ler o arquivo de mídia: %v", err)
		}

		fileExt := strings.ToLower(mediaPath[strings.LastIndex(mediaPath, ".")+1:])
		var mediaType whatsmeow.MediaType
		var mimeType string

		switch fileExt {
		case "jpg", "jpeg":
			mediaType, mimeType = whatsmeow.MediaImage, "image/jpeg"
		case "png":
			mediaType, mimeType = whatsmeow.MediaImage, "image/png"
		case "gif":
			mediaType, mimeType = whatsmeow.MediaImage, "image/gif"
		case "webp":
			mediaType, mimeType = whatsmeow.MediaImage, "image/webp"
		case "ogg":
			mediaType, mimeType = whatsmeow.MediaAudio, "audio/ogg; codecs=opus"
		case "mp4":
			mediaType, mimeType = whatsmeow.MediaVideo, "video/mp4"
		case "avi":
			mediaType, mimeType = whatsmeow.MediaVideo, "video/avi"
		case "mov":
			mediaType, mimeType = whatsmeow.MediaVideo, "video/quicktime"
		default:
			mediaType, mimeType = whatsmeow.MediaDocument, "application/octet-stream"
		}

		resp, err := c.wm.Upload(context.Background(), mediaData, mediaType)
		if err != nil {
			return false, fmt.Sprintf("Erro ao enviar mídia (upload): %v", err)
		}

		switch mediaType {
		case whatsmeow.MediaImage:
			msg.ImageMessage = &waProto.ImageMessage{
				Caption:       proto.String(message),
				Mimetype:      proto.String(mimeType),
				URL:           &resp.URL,
				DirectPath:    &resp.DirectPath,
				MediaKey:      resp.MediaKey,
				FileEncSHA256: resp.FileEncSHA256,
				FileSHA256:    resp.FileSHA256,
				FileLength:    &resp.FileLength,
			}
		case whatsmeow.MediaAudio:
			var seconds uint32 = 30
			var waveform []byte
			if strings.Contains(mimeType, "ogg") {
				analyzedSeconds, analyzedWaveform, aerr := analyzeOggOpus(mediaData)
				if aerr == nil {
					seconds = analyzedSeconds
					waveform = analyzedWaveform
				} else {
					return false, fmt.Sprintf("Falha ao analisar arquivo Ogg Opus: %v", aerr)
				}
			}
			msg.AudioMessage = &waProto.AudioMessage{
				Mimetype:      proto.String(mimeType),
				URL:           &resp.URL,
				DirectPath:    &resp.DirectPath,
				MediaKey:      resp.MediaKey,
				FileEncSHA256: resp.FileEncSHA256,
				FileSHA256:    resp.FileSHA256,
				FileLength:    &resp.FileLength,
				Seconds:       proto.Uint32(seconds),
				PTT:           proto.Bool(true),
				Waveform:      waveform,
			}
		case whatsmeow.MediaVideo:
			msg.VideoMessage = &waProto.VideoMessage{
				Caption:       proto.String(message),
				Mimetype:      proto.String(mimeType),
				URL:           &resp.URL,
				DirectPath:    &resp.DirectPath,
				MediaKey:      resp.MediaKey,
				FileEncSHA256: resp.FileEncSHA256,
				FileSHA256:    resp.FileSHA256,
				FileLength:    &resp.FileLength,
			}
		case whatsmeow.MediaDocument:
			msg.DocumentMessage = &waProto.DocumentMessage{
				Title:         proto.String(mediaPath[strings.LastIndex(mediaPath, "/")+1:]),
				Caption:       proto.String(message),
				Mimetype:      proto.String(mimeType),
				URL:           &resp.URL,
				DirectPath:    &resp.DirectPath,
				MediaKey:      resp.MediaKey,
				FileEncSHA256: resp.FileEncSHA256,
				FileSHA256:    resp.FileSHA256,
				FileLength:    &resp.FileLength,
			}
		}
	} else {
		msg.Conversation = proto.String(message)
	}

	if _, err = c.wm.SendMessage(context.Background(), recipientJID, msg); err != nil {
		return false, fmt.Sprintf("Erro ao enviar mensagem: %v", err)
	}

	return true, fmt.Sprintf("Mensagem enviada para %s", recipient)
}

// Download downloads media for a stored message and returns the absolute path.
func (c *Client) Download(ctx context.Context, messageID, chatJID string) (string, error) {
	ok, _, _, absPath, err := c.download(ctx, messageID, chatJID)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("falha ao baixar mídia")
	}
	return absPath, nil
}

// ResolveRecipient parses a recipient that is either a JID (contains '@') or a
// bare phone number (country code, no '+') into a whatsmeow JID.
func ResolveRecipient(recipient string) (types.JID, error) {
	if strings.Contains(recipient, "@") {
		jid, err := types.ParseJID(recipient)
		if err != nil {
			return types.JID{}, fmt.Errorf("erro ao interpretar JID: %v", err)
		}
		return jid, nil
	}
	return types.JID{User: recipient, Server: "s.whatsapp.net"}, nil
}

// --- Event handling (ported from the bridge) ---

func (c *Client) handleMessage(msg *events.Message) {
	chatJID := msg.Info.Chat.String()
	sender := msg.Info.Sender.User

	name := c.chatName(msg.Info.Chat, chatJID, nil, sender)

	if err := c.store.StoreChat(chatJID, name, msg.Info.Timestamp); err != nil {
		c.logger.Warnf("Failed to store chat: %v", err)
	}

	content := extractTextContent(msg.Message)
	mediaType, filename, url, mediaKey, fileSHA256, fileEncSHA256, fileLength := extractMediaInfo(msg.Message)

	if content == "" && mediaType == "" {
		return
	}

	if err := c.store.StoreMessage(
		msg.Info.ID, chatJID, sender, content, msg.Info.Timestamp, msg.Info.IsFromMe,
		mediaType, filename, url, mediaKey, fileSHA256, fileEncSHA256, fileLength,
	); err != nil {
		c.logger.Warnf("Failed to store message: %v", err)
	}
}

// chatName determines a display name for a chat (bridge's GetChatName parity).
func (c *Client) chatName(jid types.JID, chatJID string, conversation interface{}, sender string) string {
	var existingName string
	err := c.store.db.QueryRow("SELECT name FROM chats WHERE jid = ?", chatJID).Scan(&existingName)
	if err == nil && existingName != "" {
		return existingName
	}

	var name string
	if jid.Server == "g.us" {
		if conversation != nil {
			var displayName, convName *string
			v := reflect.ValueOf(conversation)
			if v.Kind() == reflect.Ptr && !v.IsNil() {
				v = v.Elem()
				if f := v.FieldByName("DisplayName"); f.IsValid() && f.Kind() == reflect.Ptr && !f.IsNil() {
					dn := f.Elem().String()
					displayName = &dn
				}
				if f := v.FieldByName("Name"); f.IsValid() && f.Kind() == reflect.Ptr && !f.IsNil() {
					n := f.Elem().String()
					convName = &n
				}
			}
			if displayName != nil && *displayName != "" {
				name = *displayName
			} else if convName != nil && *convName != "" {
				name = *convName
			}
		}
		if name == "" {
			groupInfo, err := c.wm.GetGroupInfo(context.Background(), jid)
			if err == nil && groupInfo.Name != "" {
				name = groupInfo.Name
			} else {
				name = fmt.Sprintf("Group %s", jid.User)
			}
		}
	} else {
		contact, err := c.wm.Store.Contacts.GetContact(context.Background(), jid)
		if err == nil && contact.FullName != "" {
			name = contact.FullName
		} else if sender != "" {
			name = sender
		} else {
			name = jid.User
		}
	}
	return name
}

func (c *Client) handleHistorySync(historySync *events.HistorySync) {
	for _, conversation := range historySync.Data.Conversations {
		if conversation.ID == nil {
			continue
		}
		chatJID := *conversation.ID

		jid, err := types.ParseJID(chatJID)
		if err != nil {
			c.logger.Warnf("Failed to parse JID %s: %v", chatJID, err)
			continue
		}

		name := c.chatName(jid, chatJID, conversation, "")

		messages := conversation.Messages
		if len(messages) == 0 {
			continue
		}

		latestMsg := messages[0]
		if latestMsg == nil || latestMsg.Message == nil {
			continue
		}
		var latestTS time.Time
		if ts := latestMsg.Message.GetMessageTimestamp(); ts != 0 {
			latestTS = time.Unix(int64(ts), 0)
		} else {
			continue
		}
		c.store.StoreChat(chatJID, name, latestTS)

		for _, m := range messages {
			if m == nil || m.Message == nil {
				continue
			}

			var content string
			if m.Message.Message != nil {
				if conv := m.Message.Message.GetConversation(); conv != "" {
					content = conv
				} else if ext := m.Message.Message.GetExtendedTextMessage(); ext != nil {
					content = ext.GetText()
				}
			}

			var mediaType, filename, url string
			var mediaKey, fileSHA256, fileEncSHA256 []byte
			var fileLength uint64
			if m.Message.Message != nil {
				mediaType, filename, url, mediaKey, fileSHA256, fileEncSHA256, fileLength = extractMediaInfo(m.Message.Message)
			}

			if content == "" && mediaType == "" {
				continue
			}

			var sender string
			isFromMe := false
			if m.Message.Key != nil {
				if m.Message.Key.FromMe != nil {
					isFromMe = *m.Message.Key.FromMe
				}
				if !isFromMe && m.Message.Key.Participant != nil && *m.Message.Key.Participant != "" {
					sender = *m.Message.Key.Participant
				} else if isFromMe {
					sender = c.wm.Store.ID.User
				} else {
					sender = jid.User
				}
			} else {
				sender = jid.User
			}

			msgID := ""
			if m.Message.Key != nil && m.Message.Key.ID != nil {
				msgID = *m.Message.Key.ID
			}

			var ts time.Time
			if t := m.Message.GetMessageTimestamp(); t != 0 {
				ts = time.Unix(int64(t), 0)
			} else {
				continue
			}

			if err := c.store.StoreMessage(
				msgID, chatJID, sender, content, ts, isFromMe,
				mediaType, filename, url, mediaKey, fileSHA256, fileEncSHA256, fileLength,
			); err != nil {
				c.logger.Warnf("Failed to store history message: %v", err)
			}
		}
	}
}
