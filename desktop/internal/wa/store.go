package wa

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// MessageStore is the SQLite-backed store for chats/messages.
//
// The schema below is kept IDENTICAL to the reference whatsapp-bridge
// (whatsapp-mcp/whatsapp-bridge/main.go) so that a `messages.db` produced by
// either program is fully interchangeable with the other.
type MessageStore struct {
	db      *sql.DB
	baseDir string // absolute store dir (holds messages.db + per-chat media)
}

// Message mirrors the bridge's internal message shape (used by StoreMessage
// callers). The richer read-side structs live in the wastore package.
type Message struct {
	Time      time.Time
	Sender    string
	Content   string
	IsFromMe  bool
	MediaType string
	Filename  string
}

// NewMessageStore opens (creating tables if needed) messages.db inside baseDir.
// baseDir MUST be an absolute path — Claude Desktop launches the binary with an
// arbitrary working directory, so relative "store/" paths (as used by the
// bridge) would break. The caller is responsible for MkdirAll-ing baseDir.
func NewMessageStore(baseDir string) (*MessageStore, error) {
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create store directory: %v", err)
	}

	dbPath := filepath.Join(baseDir, "messages.db")
	// _foreign_keys=on matches the bridge. Path is absolute via baseDir.
	db, err := sql.Open("sqlite3", "file:"+dbPath+"?_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("failed to open message database: %v", err)
	}

	// IDENTICAL schema to the bridge — do not change column order/types.
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS chats (
			jid TEXT PRIMARY KEY,
			name TEXT,
			last_message_time TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS messages (
			id TEXT,
			chat_jid TEXT,
			sender TEXT,
			content TEXT,
			timestamp TIMESTAMP,
			is_from_me BOOLEAN,
			media_type TEXT,
			filename TEXT,
			url TEXT,
			media_key BLOB,
			file_sha256 BLOB,
			file_enc_sha256 BLOB,
			file_length INTEGER,
			PRIMARY KEY (id, chat_jid),
			FOREIGN KEY (chat_jid) REFERENCES chats(jid)
		);
	`)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create tables: %v", err)
	}

	return &MessageStore{db: db, baseDir: baseDir}, nil
}

// DB exposes the underlying *sql.DB so the read-query layer (wastore) can run
// the ported whatsapp.py SELECTs against the same connection.
func (store *MessageStore) DB() *sql.DB { return store.db }

// BaseDir returns the absolute store directory (used to place downloaded media).
func (store *MessageStore) BaseDir() string { return store.baseDir }

// Close closes the database connection.
func (store *MessageStore) Close() error { return store.db.Close() }

// StoreChat upserts a chat row.
func (store *MessageStore) StoreChat(jid, name string, lastMessageTime time.Time) error {
	_, err := store.db.Exec(
		"INSERT OR REPLACE INTO chats (jid, name, last_message_time) VALUES (?, ?, ?)",
		jid, name, lastMessageTime,
	)
	return err
}

// StoreMessage upserts a message row. Mirrors the bridge exactly, including the
// "skip empty" guard.
func (store *MessageStore) StoreMessage(id, chatJID, sender, content string, timestamp time.Time, isFromMe bool,
	mediaType, filename, url string, mediaKey, fileSHA256, fileEncSHA256 []byte, fileLength uint64) error {
	// Only store if there's actual content or media.
	if content == "" && mediaType == "" {
		return nil
	}

	_, err := store.db.Exec(
		`INSERT OR REPLACE INTO messages
		(id, chat_jid, sender, content, timestamp, is_from_me, media_type, filename, url, media_key, file_sha256, file_enc_sha256, file_length)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, chatJID, sender, content, timestamp, isFromMe, mediaType, filename, url, mediaKey, fileSHA256, fileEncSHA256, fileLength,
	)
	return err
}

// GetMediaInfo fetches the media columns for a specific message (bridge parity).
func (store *MessageStore) GetMediaInfo(id, chatJID string) (mediaType, filename, url string, mediaKey, fileSHA256, fileEncSHA256 []byte, fileLength uint64, err error) {
	err = store.db.QueryRow(
		"SELECT media_type, filename, url, media_key, file_sha256, file_enc_sha256, file_length FROM messages WHERE id = ? AND chat_jid = ?",
		id, chatJID,
	).Scan(&mediaType, &filename, &url, &mediaKey, &fileSHA256, &fileEncSHA256, &fileLength)
	return
}

// mediaDirFor returns the absolute per-chat media directory (baseDir/<jid>),
// with ':' replaced by '_' exactly like the bridge's "store/<jid>" layout.
func (store *MessageStore) mediaDirFor(chatJID string) string {
	return filepath.Join(store.baseDir, strings.ReplaceAll(chatJID, ":", "_"))
}
