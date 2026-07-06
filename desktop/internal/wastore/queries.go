// Package wastore holds the read-only query layer, ported from the reference
// whatsapp-mcp-server/whatsapp.py. Every SQL statement and its semantics match
// the Python originals (list_messages, get_message_context, list_chats,
// search_contacts, get_contact_chats, get_last_interaction, get_chat,
// get_direct_chat_by_contact). Output formatting mirrors format_message /
// format_messages_list.
package wastore

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// Queries runs read-only queries against the messages.db connection.
type Queries struct {
	db *sql.DB
}

// New wraps an existing *sql.DB (the one owned by wa.MessageStore).
func New(db *sql.DB) *Queries { return &Queries{db: db} }

// Message mirrors whatsapp.py's Message dataclass.
type Message struct {
	Timestamp time.Time
	Sender    string
	Content   string
	IsFromMe  bool
	ChatJID   string
	ID        string
	ChatName  string
	MediaType string
}

// Chat mirrors whatsapp.py's Chat dataclass.
type Chat struct {
	JID             string
	Name            string
	LastMessageTime *time.Time
	LastMessage     string
	LastSender      string
	LastIsFromMe    bool
}

func (c Chat) IsGroup() bool { return strings.HasSuffix(c.JID, "@g.us") }

// Contact mirrors whatsapp.py's Contact dataclass.
type Contact struct {
	PhoneNumber string
	Name        string
	JID         string
}

// MessageContext mirrors whatsapp.py's MessageContext dataclass.
type MessageContext struct {
	Message Message
	Before  []Message
	After   []Message
}

// scanTime parses a timestamp column that SQLite may hand back as time.Time or
// as a string (go-sqlite3 returns time.Time for TIMESTAMP with the default DSN,
// but history rows written as strings are handled defensively).
func scanTime(v interface{}) (time.Time, bool) {
	switch t := v.(type) {
	case time.Time:
		return t, true
	case []byte:
		return parseTimeString(string(t))
	case string:
		return parseTimeString(t)
	case nil:
		return time.Time{}, false
	}
	return time.Time{}, false
}

func parseTimeString(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05-07:00",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05Z07:00",
	}
	for _, l := range layouts {
		if t, err := time.Parse(l, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// --- Formatting (parity with whatsapp.py format_message / format_messages_list) ---

// getSenderName mirrors whatsapp.py get_sender_name: look up a display name in
// the chats table by exact JID, then by "contains phone part".
func (q *Queries) getSenderName(senderJID string) string {
	var name string
	err := q.db.QueryRow("SELECT name FROM chats WHERE jid = ? LIMIT 1", senderJID).Scan(&name)
	if err == nil && name != "" {
		return name
	}

	phonePart := senderJID
	if i := strings.Index(senderJID, "@"); i >= 0 {
		phonePart = senderJID[:i]
	}
	err = q.db.QueryRow("SELECT name FROM chats WHERE jid LIKE ? LIMIT 1", "%"+phonePart+"%").Scan(&name)
	if err == nil && name != "" {
		return name
	}
	return senderJID
}

// FormatMessage mirrors whatsapp.py format_message.
func (q *Queries) FormatMessage(m Message, showChatInfo bool) string {
	var b strings.Builder
	if showChatInfo && m.ChatName != "" {
		fmt.Fprintf(&b, "[%s] Chat: %s ", m.Timestamp.Format("2006-01-02 15:04:05"), m.ChatName)
	} else {
		fmt.Fprintf(&b, "[%s] ", m.Timestamp.Format("2006-01-02 15:04:05"))
	}

	contentPrefix := ""
	if m.MediaType != "" {
		contentPrefix = fmt.Sprintf("[%s - Message ID: %s - Chat JID: %s] ", m.MediaType, m.ID, m.ChatJID)
	}

	senderName := "Me"
	if !m.IsFromMe {
		senderName = q.getSenderName(m.Sender)
	}
	fmt.Fprintf(&b, "From: %s: %s%s\n", senderName, contentPrefix, m.Content)
	return b.String()
}

// FormatMessagesList mirrors whatsapp.py format_messages_list.
func (q *Queries) FormatMessagesList(messages []Message, showChatInfo bool) string {
	if len(messages) == 0 {
		return "No messages to display."
	}
	var b strings.Builder
	for _, m := range messages {
		b.WriteString(q.FormatMessage(m, showChatInfo))
	}
	return b.String()
}

// FormatChat renders a Chat similarly to how the Python server surfaces chats.
func FormatChat(c Chat) string {
	var b strings.Builder
	name := c.Name
	if name == "" {
		name = "(sem nome)"
	}
	kind := "DM"
	if c.IsGroup() {
		kind = "Grupo"
	}
	fmt.Fprintf(&b, "%s [%s] JID: %s", name, kind, c.JID)
	if c.LastMessageTime != nil {
		fmt.Fprintf(&b, " | Última atividade: %s", c.LastMessageTime.Format("2006-01-02 15:04:05"))
	}
	if c.LastMessage != "" {
		who := "outro"
		if c.LastIsFromMe {
			who = "eu"
		}
		fmt.Fprintf(&b, " | Última mensagem (%s): %s", who, c.LastMessage)
	}
	b.WriteString("\n")
	return b.String()
}

// FormatChatsList renders a slice of chats, or a friendly empty message.
func FormatChatsList(chats []Chat) string {
	if len(chats) == 0 {
		return "Nenhuma conversa encontrada. (Se você acabou de conectar, o histórico pode ainda estar sincronizando.)"
	}
	var b strings.Builder
	for _, c := range chats {
		b.WriteString(FormatChat(c))
	}
	return b.String()
}

// scanMessageRow scans the 8-column message projection used across queries:
// timestamp, sender, chat_name, content, is_from_me, chat_jid, id, media_type.
func scanMessageRow(rows *sql.Rows) (Message, error) {
	var (
		tsRaw     interface{}
		sender    sql.NullString
		chatName  sql.NullString
		content   sql.NullString
		isFromMe  sql.NullBool
		chatJID   sql.NullString
		id        sql.NullString
		mediaType sql.NullString
	)
	if err := rows.Scan(&tsRaw, &sender, &chatName, &content, &isFromMe, &chatJID, &id, &mediaType); err != nil {
		return Message{}, err
	}
	ts, _ := scanTime(tsRaw)
	return Message{
		Timestamp: ts,
		Sender:    sender.String,
		ChatName:  chatName.String,
		Content:   content.String,
		IsFromMe:  isFromMe.Bool,
		ChatJID:   chatJID.String,
		ID:        id.String,
		MediaType: mediaType.String,
	}, nil
}

// scanChatRow scans the 6-column chat projection used across queries:
// jid, name, last_message_time, last_message, last_sender, last_is_from_me.
func scanChatRow(rows *sql.Rows) (Chat, error) {
	var (
		jid          sql.NullString
		name         sql.NullString
		lastTimeRaw  interface{}
		lastMessage  sql.NullString
		lastSender   sql.NullString
		lastIsFromMe sql.NullBool
	)
	if err := rows.Scan(&jid, &name, &lastTimeRaw, &lastMessage, &lastSender, &lastIsFromMe); err != nil {
		return Chat{}, err
	}
	c := Chat{
		JID:          jid.String,
		Name:         name.String,
		LastMessage:  lastMessage.String,
		LastSender:   lastSender.String,
		LastIsFromMe: lastIsFromMe.Bool,
	}
	if t, ok := scanTime(lastTimeRaw); ok {
		c.LastMessageTime = &t
	}
	return c, nil
}

// --- Ported queries ---

// ListMessages ports whatsapp.py list_messages, including optional context.
// Returns the already-formatted string (matching the Python behavior which
// returns format_messages_list output).
func (q *Queries) ListMessages(after, before, senderPhone, chatJID, query string, limit, page int, includeContext bool, contextBefore, contextAfter int) (string, error) {
	parts := []string{
		"SELECT messages.timestamp, messages.sender, chats.name, messages.content, messages.is_from_me, chats.jid, messages.id, messages.media_type FROM messages",
		"JOIN chats ON messages.chat_jid = chats.jid",
	}
	var where []string
	var params []interface{}

	if after != "" {
		t, err := parseISO(after)
		if err != nil {
			return "", fmt.Errorf("formato de data inválido para 'after': %s. Use ISO-8601", after)
		}
		where = append(where, "messages.timestamp > ?")
		params = append(params, t)
	}
	if before != "" {
		t, err := parseISO(before)
		if err != nil {
			return "", fmt.Errorf("formato de data inválido para 'before': %s. Use ISO-8601", before)
		}
		where = append(where, "messages.timestamp < ?")
		params = append(params, t)
	}
	if senderPhone != "" {
		where = append(where, "messages.sender = ?")
		params = append(params, senderPhone)
	}
	if chatJID != "" {
		where = append(where, "messages.chat_jid = ?")
		params = append(params, chatJID)
	}
	if query != "" {
		where = append(where, "LOWER(messages.content) LIKE LOWER(?)")
		params = append(params, "%"+query+"%")
	}
	if len(where) > 0 {
		parts = append(parts, "WHERE "+strings.Join(where, " AND "))
	}

	offset := page * limit
	parts = append(parts, "ORDER BY messages.timestamp DESC", "LIMIT ? OFFSET ?")
	params = append(params, limit, offset)

	rows, err := q.db.Query(strings.Join(parts, " "), params...)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	var result []Message
	for rows.Next() {
		m, err := scanMessageRow(rows)
		if err != nil {
			return "", err
		}
		result = append(result, m)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}

	if includeContext && len(result) > 0 {
		var withContext []Message
		for _, m := range result {
			ctx, err := q.GetMessageContext(m.ID, contextBefore, contextAfter)
			if err != nil {
				// Mirror Python leniency: skip context we can't build.
				withContext = append(withContext, m)
				continue
			}
			withContext = append(withContext, ctx.Before...)
			withContext = append(withContext, ctx.Message)
			withContext = append(withContext, ctx.After...)
		}
		return q.FormatMessagesList(withContext, true), nil
	}

	return q.FormatMessagesList(result, true), nil
}

// GetMessageContext ports whatsapp.py get_message_context.
func (q *Queries) GetMessageContext(messageID string, before, after int) (*MessageContext, error) {
	var (
		tsRaw     interface{}
		sender    sql.NullString
		chatName  sql.NullString
		content   sql.NullString
		isFromMe  sql.NullBool
		chatsJID  sql.NullString
		id        sql.NullString
		msgChatID sql.NullString
		mediaType sql.NullString
	)
	err := q.db.QueryRow(`
		SELECT messages.timestamp, messages.sender, chats.name, messages.content, messages.is_from_me, chats.jid, messages.id, messages.chat_jid, messages.media_type
		FROM messages
		JOIN chats ON messages.chat_jid = chats.jid
		WHERE messages.id = ?`, messageID).
		Scan(&tsRaw, &sender, &chatName, &content, &isFromMe, &chatsJID, &id, &msgChatID, &mediaType)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("mensagem com ID %s não encontrada", messageID)
	}
	if err != nil {
		return nil, err
	}

	targetTS, _ := scanTime(tsRaw)
	target := Message{
		Timestamp: targetTS,
		Sender:    sender.String,
		ChatName:  chatName.String,
		Content:   content.String,
		IsFromMe:  isFromMe.Bool,
		ChatJID:   chatsJID.String,
		ID:        id.String,
		MediaType: mediaType.String,
	}

	// The Python code passes the raw timestamp string back into the < / >
	// comparisons. We pass the parsed time; SQLite compares TIMESTAMP columns
	// consistently either way.
	beforeMsgs, err := q.contextSide(msgChatID.String, targetTS, before, true)
	if err != nil {
		return nil, err
	}
	afterMsgs, err := q.contextSide(msgChatID.String, targetTS, after, false)
	if err != nil {
		return nil, err
	}

	return &MessageContext{Message: target, Before: beforeMsgs, After: afterMsgs}, nil
}

func (q *Queries) contextSide(chatJID string, pivot time.Time, limit int, isBefore bool) ([]Message, error) {
	cmp, order := "<", "DESC"
	if !isBefore {
		cmp, order = ">", "ASC"
	}
	sqlStr := fmt.Sprintf(`
		SELECT messages.timestamp, messages.sender, chats.name, messages.content, messages.is_from_me, chats.jid, messages.id, messages.media_type
		FROM messages
		JOIN chats ON messages.chat_jid = chats.jid
		WHERE messages.chat_jid = ? AND messages.timestamp %s ?
		ORDER BY messages.timestamp %s
		LIMIT ?`, cmp, order)

	rows, err := q.db.Query(sqlStr, chatJID, pivot, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Message
	for rows.Next() {
		m, err := scanMessageRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ListChats ports whatsapp.py list_chats.
func (q *Queries) ListChats(query string, limit, page int, includeLastMessage bool, sortBy string) ([]Chat, error) {
	parts := []string{`
		SELECT
			chats.jid,
			chats.name,
			chats.last_message_time,
			messages.content as last_message,
			messages.sender as last_sender,
			messages.is_from_me as last_is_from_me
		FROM chats`}

	if includeLastMessage {
		parts = append(parts, `
			LEFT JOIN messages ON chats.jid = messages.chat_jid
			AND chats.last_message_time = messages.timestamp`)
	}

	var where []string
	var params []interface{}
	if query != "" {
		where = append(where, "(LOWER(chats.name) LIKE LOWER(?) OR chats.jid LIKE ?)")
		params = append(params, "%"+query+"%", "%"+query+"%")
	}
	if len(where) > 0 {
		parts = append(parts, "WHERE "+strings.Join(where, " AND "))
	}

	orderBy := "chats.last_message_time DESC"
	if sortBy != "last_active" {
		orderBy = "chats.name"
	}
	parts = append(parts, "ORDER BY "+orderBy)

	offset := page * limit
	parts = append(parts, "LIMIT ? OFFSET ?")
	params = append(params, limit, offset)

	rows, err := q.db.Query(strings.Join(parts, " "), params...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []Chat
	for rows.Next() {
		c, err := scanChatRow(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, rows.Err()
}

// SearchContacts ports whatsapp.py search_contacts.
func (q *Queries) SearchContacts(query string) ([]Contact, error) {
	pattern := "%" + query + "%"
	rows, err := q.db.Query(`
		SELECT DISTINCT jid, name
		FROM chats
		WHERE (LOWER(name) LIKE LOWER(?) OR LOWER(jid) LIKE LOWER(?))
			AND jid NOT LIKE '%@g.us'
		ORDER BY name, jid
		LIMIT 50`, pattern, pattern)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []Contact
	for rows.Next() {
		var jid, name sql.NullString
		if err := rows.Scan(&jid, &name); err != nil {
			return nil, err
		}
		phone := jid.String
		if i := strings.Index(phone, "@"); i >= 0 {
			phone = phone[:i]
		}
		result = append(result, Contact{PhoneNumber: phone, Name: name.String, JID: jid.String})
	}
	return result, rows.Err()
}

// GetContactChats ports whatsapp.py get_contact_chats.
func (q *Queries) GetContactChats(jid string, limit, page int) ([]Chat, error) {
	rows, err := q.db.Query(`
		SELECT DISTINCT
			c.jid,
			c.name,
			c.last_message_time,
			m.content as last_message,
			m.sender as last_sender,
			m.is_from_me as last_is_from_me
		FROM chats c
		JOIN messages m ON c.jid = m.chat_jid
		WHERE m.sender = ? OR c.jid = ?
		ORDER BY c.last_message_time DESC
		LIMIT ? OFFSET ?`, jid, jid, limit, page*limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []Chat
	for rows.Next() {
		c, err := scanChatRow(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, rows.Err()
}

// GetLastInteraction ports whatsapp.py get_last_interaction, returning the
// formatted single-message string (or "" when none).
func (q *Queries) GetLastInteraction(jid string) (string, error) {
	rows, err := q.db.Query(`
		SELECT
			m.timestamp, m.sender, c.name, m.content, m.is_from_me, c.jid, m.id, m.media_type
		FROM messages m
		JOIN chats c ON m.chat_jid = c.jid
		WHERE m.sender = ? OR c.jid = ?
		ORDER BY m.timestamp DESC
		LIMIT 1`, jid, jid)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	if !rows.Next() {
		return "", nil
	}
	m, err := scanMessageRow(rows)
	if err != nil {
		return "", err
	}
	return q.FormatMessage(m, true), nil
}

// GetChat ports whatsapp.py get_chat.
func (q *Queries) GetChat(chatJID string, includeLastMessage bool) (*Chat, error) {
	query := `
		SELECT
			c.jid, c.name, c.last_message_time,
			m.content as last_message, m.sender as last_sender, m.is_from_me as last_is_from_me
		FROM chats c`
	if includeLastMessage {
		query += `
			LEFT JOIN messages m ON c.jid = m.chat_jid
			AND c.last_message_time = m.timestamp`
	}
	query += " WHERE c.jid = ?"

	rows, err := q.db.Query(query, chatJID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, nil
	}
	c, err := scanChatRow(rows)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// GetDirectChatByContact ports whatsapp.py get_direct_chat_by_contact.
func (q *Queries) GetDirectChatByContact(senderPhone string) (*Chat, error) {
	rows, err := q.db.Query(`
		SELECT
			c.jid, c.name, c.last_message_time,
			m.content as last_message, m.sender as last_sender, m.is_from_me as last_is_from_me
		FROM chats c
		LEFT JOIN messages m ON c.jid = m.chat_jid
			AND c.last_message_time = m.timestamp
		WHERE c.jid LIKE ? AND c.jid NOT LIKE '%@g.us'
		LIMIT 1`, "%"+senderPhone+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, nil
	}
	c, err := scanChatRow(rows)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// parseISO parses an ISO-8601 timestamp the way Python's datetime.fromisoformat
// would accept (date, date+time, with optional offset).
func parseISO(s string) (time.Time, error) {
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04",
		"2006-01-02 15:04",
		"2006-01-02",
	}
	for _, l := range layouts {
		if t, err := time.Parse(l, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid ISO-8601: %s", s)
}
