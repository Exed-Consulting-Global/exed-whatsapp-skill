package wa

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"math/rand"
	"os"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/binary/proto"
)

// extractTextContent pulls plain text out of a message (bridge parity).
func extractTextContent(msg *waProto.Message) string {
	if msg == nil {
		return ""
	}
	if text := msg.GetConversation(); text != "" {
		return text
	} else if extendedText := msg.GetExtendedTextMessage(); extendedText != nil {
		return extendedText.GetText()
	}
	// Non-text messages are ignored for text extraction (media handled separately).
	return ""
}

// extractMediaInfo pulls media metadata out of a message (bridge parity).
func extractMediaInfo(msg *waProto.Message) (mediaType string, filename string, url string, mediaKey []byte, fileSHA256 []byte, fileEncSHA256 []byte, fileLength uint64) {
	if msg == nil {
		return "", "", "", nil, nil, nil, 0
	}

	if img := msg.GetImageMessage(); img != nil {
		return "image", "image_" + time.Now().Format("20060102_150405") + ".jpg",
			img.GetURL(), img.GetMediaKey(), img.GetFileSHA256(), img.GetFileEncSHA256(), img.GetFileLength()
	}
	if vid := msg.GetVideoMessage(); vid != nil {
		return "video", "video_" + time.Now().Format("20060102_150405") + ".mp4",
			vid.GetURL(), vid.GetMediaKey(), vid.GetFileSHA256(), vid.GetFileEncSHA256(), vid.GetFileLength()
	}
	if aud := msg.GetAudioMessage(); aud != nil {
		return "audio", "audio_" + time.Now().Format("20060102_150405") + ".ogg",
			aud.GetURL(), aud.GetMediaKey(), aud.GetFileSHA256(), aud.GetFileEncSHA256(), aud.GetFileLength()
	}
	if doc := msg.GetDocumentMessage(); doc != nil {
		filename := doc.GetFileName()
		if filename == "" {
			filename = "document_" + time.Now().Format("20060102_150405")
		}
		return "document", filename,
			doc.GetURL(), doc.GetMediaKey(), doc.GetFileSHA256(), doc.GetFileEncSHA256(), doc.GetFileLength()
	}

	return "", "", "", nil, nil, nil, 0
}

// extractDirectPathFromURL derives the whatsmeow direct path from a media URL
// (bridge parity).
func extractDirectPathFromURL(url string) string {
	parts := strings.SplitN(url, ".net/", 2)
	if len(parts) < 2 {
		return url
	}
	pathPart := parts[1]
	pathPart = strings.SplitN(pathPart, "?", 2)[0]
	return "/" + pathPart
}

// mediaDownloader implements whatsmeow.DownloadableMessage (bridge parity).
type mediaDownloader struct {
	URL           string
	DirectPath    string
	MediaKey      []byte
	FileLength    uint64
	FileSHA256    []byte
	FileEncSHA256 []byte
	MediaType     whatsmeow.MediaType
}

func (d *mediaDownloader) GetDirectPath() string             { return d.DirectPath }
func (d *mediaDownloader) GetURL() string                    { return d.URL }
func (d *mediaDownloader) GetMediaKey() []byte               { return d.MediaKey }
func (d *mediaDownloader) GetFileLength() uint64             { return d.FileLength }
func (d *mediaDownloader) GetFileSHA256() []byte             { return d.FileSHA256 }
func (d *mediaDownloader) GetFileEncSHA256() []byte          { return d.FileEncSHA256 }
func (d *mediaDownloader) GetMediaType() whatsmeow.MediaType { return d.MediaType }

// analyzeOggOpus extracts duration and a synthetic waveform from an Ogg Opus
// file (bridge parity). Used when sending voice messages.
func analyzeOggOpus(data []byte) (duration uint32, waveform []byte, err error) {
	if len(data) < 4 || string(data[0:4]) != "OggS" {
		return 0, nil, fmt.Errorf("not a valid Ogg file (missing OggS signature)")
	}

	var lastGranule uint64
	var sampleRate uint32 = 48000
	var preSkip uint16 = 0
	var foundOpusHead bool

	for i := 0; i < len(data); {
		if i+27 >= len(data) {
			break
		}
		if string(data[i:i+4]) != "OggS" {
			i++
			continue
		}

		granulePos := binary.LittleEndian.Uint64(data[i+6 : i+14])
		pageSeqNum := binary.LittleEndian.Uint32(data[i+18 : i+22])
		numSegments := int(data[i+26])

		if i+27+numSegments >= len(data) {
			break
		}
		segmentTable := data[i+27 : i+27+numSegments]

		pageSize := 27 + numSegments
		for _, segLen := range segmentTable {
			pageSize += int(segLen)
		}

		if !foundOpusHead && pageSeqNum <= 1 {
			pageData := data[i : i+pageSize]
			headPos := bytes.Index(pageData, []byte("OpusHead"))
			if headPos >= 0 && headPos+12 < len(pageData) {
				headPos += 8
				if headPos+12 <= len(pageData) {
					preSkip = binary.LittleEndian.Uint16(pageData[headPos+10 : headPos+12])
					sampleRate = binary.LittleEndian.Uint32(pageData[headPos+12 : headPos+16])
					foundOpusHead = true
				}
			}
		}

		if granulePos != 0 {
			lastGranule = granulePos
		}

		i += pageSize
	}

	if lastGranule > 0 {
		durationSeconds := float64(lastGranule-uint64(preSkip)) / float64(sampleRate)
		duration = uint32(math.Ceil(durationSeconds))
	} else {
		durationEstimate := float64(len(data)) / 2000.0
		duration = uint32(durationEstimate)
	}

	if duration < 1 {
		duration = 1
	} else if duration > 300 {
		duration = 300
	}

	waveform = placeholderWaveform(duration)
	return duration, waveform, nil
}

// placeholderWaveform generates a natural-looking 64-byte waveform for WhatsApp
// voice messages (bridge parity).
func placeholderWaveform(duration uint32) []byte {
	const waveformLength = 64
	waveform := make([]byte, waveformLength)

	rand.Seed(int64(duration))

	baseAmplitude := 35.0
	frequencyFactor := float64(minInt(int(duration), 120)) / 30.0

	for i := range waveform {
		pos := float64(i) / float64(waveformLength)
		val := baseAmplitude * math.Sin(pos*math.Pi*frequencyFactor*8)
		val += (baseAmplitude / 2) * math.Sin(pos*math.Pi*frequencyFactor*16)
		val += (rand.Float64() - 0.5) * 15
		fadeInOut := math.Sin(pos * math.Pi)
		val = val * (0.7 + 0.3*fadeInOut)
		val = val + 50
		if val < 0 {
			val = 0
		} else if val > 100 {
			val = 100
		}
		waveform[i] = byte(val)
	}

	return waveform
}

func minInt(x, y int) int {
	if x < y {
		return x
	}
	return y
}

// download implements the media-download flow (bridge parity), but writes into
// the absolute per-chat media dir instead of a cwd-relative "store/".
func (c *Client) download(ctx context.Context, messageID, chatJID string) (ok bool, mediaType, filename, absPath string, err error) {
	mt, fn, url, mediaKey, fileSHA256, fileEncSHA256, fileLength, gerr := c.store.GetMediaInfo(messageID, chatJID)
	if gerr != nil {
		// Fall back to the minimal columns, mirroring the bridge.
		gerr = c.store.db.QueryRow(
			"SELECT media_type, filename FROM messages WHERE id = ? AND chat_jid = ?",
			messageID, chatJID,
		).Scan(&mt, &fn)
		if gerr != nil {
			return false, "", "", "", fmt.Errorf("failed to find message: %v", gerr)
		}
	}

	if mt == "" {
		return false, "", "", "", fmt.Errorf("not a media message")
	}

	chatDir := c.store.mediaDirFor(chatJID)
	if err := os.MkdirAll(chatDir, 0755); err != nil {
		return false, "", "", "", fmt.Errorf("failed to create chat directory: %v", err)
	}

	localPath := chatDir + string(os.PathSeparator) + fn
	// chatDir is already absolute (built from the absolute baseDir).
	absPath = localPath

	// If we already downloaded this file, return it as-is.
	if _, statErr := os.Stat(localPath); statErr == nil {
		return true, mt, fn, absPath, nil
	}

	if url == "" || len(mediaKey) == 0 || len(fileSHA256) == 0 || len(fileEncSHA256) == 0 || fileLength == 0 {
		return false, "", "", "", fmt.Errorf("incomplete media information for download")
	}

	var waMediaType whatsmeow.MediaType
	switch mt {
	case "image":
		waMediaType = whatsmeow.MediaImage
	case "video":
		waMediaType = whatsmeow.MediaVideo
	case "audio":
		waMediaType = whatsmeow.MediaAudio
	case "document":
		waMediaType = whatsmeow.MediaDocument
	default:
		return false, "", "", "", fmt.Errorf("unsupported media type: %s", mt)
	}

	downloader := &mediaDownloader{
		URL:           url,
		DirectPath:    extractDirectPathFromURL(url),
		MediaKey:      mediaKey,
		FileLength:    fileLength,
		FileSHA256:    fileSHA256,
		FileEncSHA256: fileEncSHA256,
		MediaType:     waMediaType,
	}

	// whatsmeow v0.0.0-20260630180629 takes context.Context as first arg.
	mediaData, derrDL := c.wm.Download(ctx, downloader)
	if derrDL != nil {
		return false, "", "", "", fmt.Errorf("failed to download media: %v", derrDL)
	}

	if err := os.WriteFile(localPath, mediaData, 0644); err != nil {
		return false, "", "", "", fmt.Errorf("failed to save media file: %v", err)
	}

	return true, mt, fn, absPath, nil
}
