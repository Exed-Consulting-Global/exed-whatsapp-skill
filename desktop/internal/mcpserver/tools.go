package mcpserver

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Exed-Consulting-Global/exed-whatsapp-skill/desktop/internal/wastore"
)

// boolOr returns *p if set, else def. Used for tri-state bool params where the
// documented default is true.
func boolOr(p *bool, def bool) bool {
	if p == nil {
		return def
	}
	return *p
}

func intOr(v, def int) int {
	if v == 0 {
		return def
	}
	return v
}

// registerTools adds every tool to the server.
func (s *Server) registerTools(srv *mcp.Server) {
	// -------- Connection --------
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "conectar_whatsapp",
		Description: "Conecta ao WhatsApp (connect). Se já conectado, informa. Se não pareado, retorna um QR code (imagem PNG) para escanear em WhatsApp > Aparelhos conectados > Conectar aparelho.",
	}, s.handleConnect)

	// -------- Read tools (funcionam mesmo offline; leem o SQLite local) --------
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "listar_conversas",
		Description: "Lista conversas (list_chats). Lê o banco local; funciona mesmo desconectado.",
	}, s.handleListChats)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "listar_mensagens",
		Description: "Lista/busca mensagens com contexto opcional (list_messages). Lê o banco local.",
	}, s.handleListMessages)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "buscar_contatos",
		Description: "Busca contatos por nome ou telefone (search_contacts). Lê o banco local.",
	}, s.handleSearchContacts)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "obter_conversa",
		Description: "Obtém metadados de uma conversa por JID (get_chat). Lê o banco local.",
	}, s.handleGetChat)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "conversa_por_contato",
		Description: "Obtém a conversa direta (DM) de um contato pelo telefone (get_direct_chat_by_contact). Lê o banco local.",
	}, s.handleDirectChat)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "conversas_do_contato",
		Description: "Lista todas as conversas que envolvem um contato (get_contact_chats). Lê o banco local.",
	}, s.handleContactChats)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "ultima_interacao",
		Description: "Mostra a mensagem mais recente envolvendo um contato (get_last_interaction). Lê o banco local.",
	}, s.handleLastInteraction)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "contexto_da_mensagem",
		Description: "Mostra o contexto ao redor de uma mensagem (get_message_context). Lê o banco local.",
	}, s.handleMessageContext)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "baixar_midia",
		Description: "Baixa a mídia de uma mensagem e retorna o caminho local (download_media). Requer conexão para baixar mídias ainda não salvas.",
	}, s.handleDownloadMedia)

	// -------- Send tools (gated: preparar_envio -> enviar) --------
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "preparar_envio",
		Description: "Prepara um envio (prepare send) de texto e/ou arquivo. Retorna um preview e um token. NÃO envia nada; use 'enviar' com o token após aprovação do usuário.",
	}, s.handlePrepare)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "preparar_envio_arquivo",
		Description: "Atalho para preparar o envio de um arquivo (prepare send_file). Igual a preparar_envio com caminho_arquivo. Retorna preview + token; use 'enviar'.",
	}, s.handlePrepareFile)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "preparar_envio_audio",
		Description: "Atalho para preparar o envio de um áudio como nota de voz (prepare send_audio). Retorna preview + token; use 'enviar'. Precisa de .ogg opus (ou ffmpeg instalado para converter).",
	}, s.handlePrepareAudio)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "enviar",
		Description: "Envia (send) o rascunho preparado. Só funciona com o token do último preparar_envio e se ele tiver menos de 10 minutos. Requer conexão.",
	}, s.handleSend)
}

// ---------------- Connection ----------------

func (s *Server) handleConnect(ctx context.Context, _ *mcp.CallToolRequest, _ emptyIn) (*mcp.CallToolResult, any, error) {
	if s.wa.IsConnected() && s.wa.IsLoggedIn() {
		return textResult("WhatsApp já está conectado."), nil, nil
	}

	res, err := s.wa.ConnectWithQR(ctx)
	if err != nil {
		return errResult(fmt.Sprintf("Não foi possível conectar: %v", err)), nil, nil
	}
	if res.AlreadyConnected {
		return textResult("WhatsApp conectado com sucesso."), nil, nil
	}

	// Return the QR as an image plus instructions.
	content := []mcp.Content{
		&mcp.TextContent{Text: "Escaneie este QR no WhatsApp > Aparelhos conectados > Conectar aparelho. Ele expira em ~20s; se falhar, chame conectar_whatsapp de novo."},
		&mcp.ImageContent{Data: res.PNG, MIMEType: "image/png"},
	}
	return &mcp.CallToolResult{Content: content}, nil, nil
}

// ---------------- Read handlers ----------------

func (s *Server) handleListChats(ctx context.Context, _ *mcp.CallToolRequest, in listChatsIn) (*mcp.CallToolResult, any, error) {
	sortBy := in.SortBy
	if sortBy == "" {
		sortBy = "last_active"
	}
	chats, err := s.queries.ListChats(in.Query, intOr(in.Limit, 20), in.Page, boolOr(in.IncludeLastMessage, true), sortBy)
	if err != nil {
		return errResult(fmt.Sprintf("Erro ao listar conversas: %v", err)), nil, nil
	}
	return textResult(wastore.FormatChatsList(chats)), nil, nil
}

func (s *Server) handleListMessages(ctx context.Context, _ *mcp.CallToolRequest, in listMessagesIn) (*mcp.CallToolResult, any, error) {
	out, err := s.queries.ListMessages(
		in.After, in.Before, in.SenderPhoneNumber, in.ChatJID, in.Query,
		intOr(in.Limit, 20), in.Page,
		boolOr(in.IncludeContext, true), intOr(in.ContextBefore, 1), intOr(in.ContextAfter, 1),
	)
	if err != nil {
		return errResult(fmt.Sprintf("Erro ao listar mensagens: %v", err)), nil, nil
	}
	return textResult(out), nil, nil
}

func (s *Server) handleSearchContacts(ctx context.Context, _ *mcp.CallToolRequest, in searchContactsIn) (*mcp.CallToolResult, any, error) {
	contacts, err := s.queries.SearchContacts(in.Query)
	if err != nil {
		return errResult(fmt.Sprintf("Erro ao buscar contatos: %v", err)), nil, nil
	}
	if len(contacts) == 0 {
		return textResult("Nenhum contato encontrado."), nil, nil
	}
	var b strings.Builder
	for _, c := range contacts {
		name := c.Name
		if name == "" {
			name = "(sem nome)"
		}
		fmt.Fprintf(&b, "%s | Telefone: %s | JID: %s\n", name, c.PhoneNumber, c.JID)
	}
	return textResult(b.String()), nil, nil
}

func (s *Server) handleGetChat(ctx context.Context, _ *mcp.CallToolRequest, in getChatIn) (*mcp.CallToolResult, any, error) {
	chat, err := s.queries.GetChat(in.ChatJID, boolOr(in.IncludeLastMessage, true))
	if err != nil {
		return errResult(fmt.Sprintf("Erro ao obter conversa: %v", err)), nil, nil
	}
	if chat == nil {
		return textResult("Conversa não encontrada."), nil, nil
	}
	return textResult(wastore.FormatChat(*chat)), nil, nil
}

func (s *Server) handleDirectChat(ctx context.Context, _ *mcp.CallToolRequest, in directChatIn) (*mcp.CallToolResult, any, error) {
	chat, err := s.queries.GetDirectChatByContact(in.SenderPhoneNumber)
	if err != nil {
		return errResult(fmt.Sprintf("Erro ao obter conversa: %v", err)), nil, nil
	}
	if chat == nil {
		return textResult("Nenhuma conversa direta encontrada para esse contato."), nil, nil
	}
	return textResult(wastore.FormatChat(*chat)), nil, nil
}

func (s *Server) handleContactChats(ctx context.Context, _ *mcp.CallToolRequest, in contactChatsIn) (*mcp.CallToolResult, any, error) {
	chats, err := s.queries.GetContactChats(in.JID, intOr(in.Limit, 20), in.Page)
	if err != nil {
		return errResult(fmt.Sprintf("Erro ao listar conversas do contato: %v", err)), nil, nil
	}
	return textResult(wastore.FormatChatsList(chats)), nil, nil
}

func (s *Server) handleLastInteraction(ctx context.Context, _ *mcp.CallToolRequest, in lastInteractionIn) (*mcp.CallToolResult, any, error) {
	out, err := s.queries.GetLastInteraction(in.JID)
	if err != nil {
		return errResult(fmt.Sprintf("Erro ao obter última interação: %v", err)), nil, nil
	}
	if out == "" {
		return textResult("Nenhuma interação encontrada para esse contato."), nil, nil
	}
	return textResult(out), nil, nil
}

func (s *Server) handleMessageContext(ctx context.Context, _ *mcp.CallToolRequest, in messageContextIn) (*mcp.CallToolResult, any, error) {
	mc, err := s.queries.GetMessageContext(in.MessageID, intOr(in.Before, 5), intOr(in.After, 5))
	if err != nil {
		return errResult(fmt.Sprintf("Erro ao obter contexto: %v", err)), nil, nil
	}
	var all []wastore.Message
	all = append(all, mc.Before...)
	all = append(all, mc.Message)
	all = append(all, mc.After...)
	return textResult(s.queries.FormatMessagesList(all, true)), nil, nil
}

func (s *Server) handleDownloadMedia(ctx context.Context, _ *mcp.CallToolRequest, in downloadMediaIn) (*mcp.CallToolResult, any, error) {
	path, err := s.wa.Download(ctx, in.MessageID, in.ChatJID)
	if err != nil {
		return errResult(fmt.Sprintf("Erro ao baixar mídia: %v", err)), nil, nil
	}
	return textResult(fmt.Sprintf("Mídia salva em: %s", path)), nil, nil
}

// ---------------- Send: prepare handlers ----------------

func (s *Server) handlePrepare(ctx context.Context, _ *mcp.CallToolRequest, in prepareIn) (*mcp.CallToolResult, any, error) {
	if in.Destinatario == "" {
		return errResult("Informe o destinatário."), nil, nil
	}
	if in.Texto == "" && in.CaminhoArquivo == "" {
		return errResult("Informe ao menos 'texto' ou 'caminho_arquivo'."), nil, nil
	}
	if in.CaminhoArquivo != "" {
		if msg, ok := validateMediaPath(in.CaminhoArquivo, in.EnviarComoAudio); !ok {
			return errResult(msg), nil, nil
		}
	}
	return s.setDraft(in.Destinatario, in.Texto, in.CaminhoArquivo, in.EnviarComoAudio), nil, nil
}

func (s *Server) handlePrepareFile(ctx context.Context, _ *mcp.CallToolRequest, in prepareFileIn) (*mcp.CallToolResult, any, error) {
	if in.Destinatario == "" || in.CaminhoArquivo == "" {
		return errResult("Informe destinatário e caminho_arquivo."), nil, nil
	}
	if msg, ok := validateMediaPath(in.CaminhoArquivo, false); !ok {
		return errResult(msg), nil, nil
	}
	return s.setDraft(in.Destinatario, in.Texto, in.CaminhoArquivo, false), nil, nil
}

func (s *Server) handlePrepareAudio(ctx context.Context, _ *mcp.CallToolRequest, in prepareAudioIn) (*mcp.CallToolResult, any, error) {
	if in.Destinatario == "" || in.CaminhoArquivo == "" {
		return errResult("Informe destinatário e caminho_arquivo."), nil, nil
	}
	if msg, ok := validateMediaPath(in.CaminhoArquivo, true); !ok {
		return errResult(msg), nil, nil
	}
	return s.setDraft(in.Destinatario, "", in.CaminhoArquivo, true), nil, nil
}

// ---------------- Send: dispatch ----------------

func (s *Server) handleSend(ctx context.Context, _ *mcp.CallToolRequest, in sendIn) (*mcp.CallToolResult, any, error) {
	if !s.wa.IsConnected() {
		return errResult("Não conectado ao WhatsApp. Chame conectar_whatsapp primeiro."), nil, nil
	}

	s.mu.Lock()
	d := s.pending
	// Validate token/age under the lock, then clear so a token is single-use.
	if d == nil {
		s.mu.Unlock()
		return errResult("Nenhum rascunho preparado. Chame preparar_envio primeiro."), nil, nil
	}
	if in.Token != d.token {
		s.mu.Unlock()
		return errResult("Token não confere com o último rascunho. Chame preparar_envio novamente e use o token retornado."), nil, nil
	}
	if time.Since(d.createdAt) > draftTTL {
		s.pending = nil
		s.mu.Unlock()
		return errResult("O rascunho expirou (mais de 10 minutos). Chame preparar_envio novamente."), nil, nil
	}
	// Consume the draft.
	s.pending = nil
	s.mu.Unlock()

	// Resolve the media path: for audio, convert to .ogg opus if needed.
	mediaPath := d.mediaPath
	if mediaPath != "" && d.asAudio && !strings.HasSuffix(strings.ToLower(mediaPath), ".ogg") {
		converted, err := convertToOpusOgg(mediaPath)
		if err != nil {
			return errResult(fmt.Sprintf("Não foi possível converter o áudio para .ogg opus. Você provavelmente precisa instalar o ffmpeg. Detalhe: %v", err)), nil, nil
		}
		mediaPath = converted
		defer os.Remove(mediaPath) // temp file
	}

	ok, msg := s.wa.Send(d.recipient, d.text, mediaPath)
	if !ok {
		return errResult(msg), nil, nil
	}
	return textResult(msg), nil, nil
}

// ---------------- media helpers ----------------

// validateMediaPath checks the file exists and is readable. For audio, if the
// file isn't already .ogg it verifies ffmpeg is available now, so the user gets
// a clear error at prepare time rather than after approving the send. Returns
// (message, ok); message is only meaningful when ok is false.
func validateMediaPath(path string, asAudio bool) (string, bool) {
	if !filepath.IsAbs(path) {
		return "Use um caminho absoluto para o arquivo.", false
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Sprintf("Arquivo não encontrado: %s", path), false
	}
	if info.IsDir() {
		return fmt.Sprintf("O caminho é um diretório, não um arquivo: %s", path), false
	}
	if asAudio && !strings.HasSuffix(strings.ToLower(path), ".ogg") {
		if _, err := exec.LookPath("ffmpeg"); err != nil {
			return "Para enviar como áudio, forneça um arquivo .ogg (opus) ou instale o ffmpeg para conversão automática.", false
		}
	}
	return "", true
}

// convertToOpusOgg converts an arbitrary audio file to a temporary .ogg opus
// file using ffmpeg. If ffmpeg is not installed, it returns an error (surfaced
// to the model as a clear message). Mirrors the Python audio.convert helper.
func convertToOpusOgg(inputPath string) (string, error) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		return "", fmt.Errorf("ffmpeg não encontrado no PATH")
	}

	tmp, err := os.CreateTemp("", "exed-wa-*.ogg")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	tmp.Close()

	// -y overwrite, libopus mono 48k — the format WhatsApp voice notes expect.
	cmd := exec.Command(ffmpeg, "-y", "-i", inputPath,
		"-c:a", "libopus", "-ac", "1", "-ar", "48000", "-b:a", "32k",
		tmpPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	return tmpPath, nil
}
