# Setup e troubleshooting — skill whatsapp

Guia de instalação, pareamento e manutenção da stack local que serve esta skill, para macOS,
Linux/WSL e Windows nativo.

## Arquitetura

`$WHATSAPP_MCP_DIR` = pasta do clone do whatsapp-mcp. Default: `~/whatsapp-mcp`
(Windows: `%USERPROFILE%\whatsapp-mcp`). Quem usa outro local exporta a variável de ambiente —
o health check e os instaladores respeitam.

| Componente | O que é | Onde |
|---|---|---|
| `whatsapp-bridge` | Ponte em Go ([whatsmeow](https://github.com/tulir/whatsmeow)) pareada como "aparelho vinculado"; sincroniza mensagens e expõe REST em `http://localhost:8080/api` | `$WHATSAPP_MCP_DIR/whatsapp-bridge/` (binário `whatsapp-bridge` / `.exe`) |
| `store/whatsapp.db` | Sessão e chaves do pareamento | `$WHATSAPP_MCP_DIR/whatsapp-bridge/store/` |
| `store/messages.db` | Histórico de mensagens (SQLite, **texto claro**) | idem |
| `whatsapp-mcp-server` | Servidor MCP em Python (FastMCP via `uv`); lê `messages.db` direto e envia pelo REST da ponte | `$WHATSAPP_MCP_DIR/whatsapp-mcp-server/` |
| Skill | Este plugin | instalada via `/plugin install exed-whatsapp@exed` |

Consequências práticas:

- **Ponte parada** ⇒ leituras continuam (dados congelados no último sync); envios falham com `Request error`.
- Os paths `store/` são **relativos ao working directory da ponte** — rode-a sempre a partir de
  `whatsapp-bridge/` (os keep-alives dos instaladores já configuram isso), senão ela cria um
  segundo banco vazio em outro lugar e "some" o histórico.
- Histórico só acumula enquanto a ponte roda — períodos desligada viram lacunas.
- **Mover o clone quebra tudo**: o registro MCP embute path absoluto e o servidor Python acha o DB
  por path relativo ao layout do clone. Se mover, re-rode o instalador (ou re-registre o MCP) e
  exporte `WHATSAPP_MCP_DIR`.

## Instalação automatizada (recomendada)

Os instaladores do plugin fazem: clone → atualização do whatsmeow + patch (ver "Erros comuns:
405") → build → `uv sync` → registro do MCP em escopo user.

- **macOS / Linux / WSL**: `bash scripts/setup.sh`
- **Windows nativo** (beta): `powershell -ExecutionPolicy Bypass -File scripts/setup.ps1`

Pré-requisitos por SO:

| SO | Necessário |
|---|---|
| macOS | `go`, `uv`, `git` (`brew install go uv`) |
| Linux / WSL | `go` ([go.dev/dl](https://go.dev/dl)), `uv`, `git`, `curl` |
| Windows nativo | `go`, `uv`, `git` **e gcc** (go-sqlite3 exige CGO): `winget install MSYS2.MSYS2`, no shell MSYS2 `pacman -S mingw-w64-ucrt-x86_64-gcc`, adicionar `C:\msys64\ucrt64\bin` ao PATH |

Opcionais: `ffmpeg` (conversão de áudio no envio), `sqlite3` (diagnóstico melhor no health check).

## Instalação manual (se preferir)

```bash
git clone https://github.com/lharries/whatsapp-mcp.git ~/whatsapp-mcp
cd ~/whatsapp-mcp/whatsapp-bridge
go get -u go.mau.fi/whatsmeow@latest && go mod tidy
git -C .. apply <plugin>/skills/whatsapp/assets/whatsmeow-context-fix.patch
go build -o whatsapp-bridge .          # Windows: -o whatsapp-bridge.exe (CGO_ENABLED=1)
cd ../whatsapp-mcp-server && uv sync
claude mcp add --scope user whatsapp -- "$(command -v uv)" --directory ~/whatsapp-mcp/whatsapp-mcp-server run main.py
```

## Pareamento QR (primeira vez)

No **terminal do usuário** (o QR em ANSI renderiza melhor lá e se renova a cada ~60 s):

```bash
cd ~/whatsapp-mcp/whatsapp-bridge && ./whatsapp-bridge     # Windows: .\whatsapp-bridge.exe
```

No celular: **WhatsApp > Configurações > Dispositivos conectados > Conectar dispositivo** e
escanear. Depois do pareamento, a ponte sincroniza o histórico recente (pode levar alguns minutos).
Verificar:

```bash
sqlite3 ~/whatsapp-mcp/whatsapp-bridge/store/whatsapp.db "SELECT count(*) FROM whatsmeow_device;"   # >= 1
```

Se o QR não aparecer, encerre (Ctrl+C) e rode de novo.

Sessões do Claude Code carregam MCPs e skills **no início** — depois de instalar/registrar, abra
uma sessão nova.

## Keep-alive (ponte sempre rodando)

- **macOS**: `bash scripts/setup.sh --launchd` (rodar **depois** do pareamento — o serviço encerra
  a ponte manual e assume). Logs: `tail -f ~/Library/Logs/whatsapp-bridge.log`. Parar/remover:
  `launchctl bootout gui/$(id -u)/com.exed.whatsapp-bridge`.
- **Windows**: `setup.ps1 -TaskScheduler` (também depois do pareamento). Registra a tarefa
  `ExedWhatsAppBridge` no logon, com working directory correto. Parar:
  `Stop-ScheduledTask -TaskName ExedWhatsAppBridge`.
- **Linux / WSL**: rode manualmente (terminal/tmux) ou crie um serviço systemd de usuário com
  `WorkingDirectory=` apontando para `whatsapp-bridge/`.

Atenção (macOS): com a sessão expirada, o launchd fica reiniciando uma ponte que só pede QR — para
re-parear, siga a seção abaixo.

## Re-pareamento (a cada ~20 dias)

A sessão de aparelho vinculado expira em ~20 dias. Sintoma: ponte "up" mas sem mensagens novas
(o check-bridge.sh avisa), ou erros de autenticação no log.

1. Pare a ponte (Ctrl+C, `launchctl bootout gui/$(id -u)/com.exed.whatsapp-bridge` ou
   `Stop-ScheduledTask -TaskName ExedWhatsAppBridge`).
2. Rode manualmente no terminal e escaneie o QR novo.
3. Religue o keep-alive (`setup.sh --launchd` / `Start-ScheduledTask -TaskName ExedWhatsAppBridge`).

**Histórico dessincronizado / mensagens fora de ordem**: pare a ponte, apague **os dois** bancos —
`store/messages.db` **e** `store/whatsapp.db` — e re-pareie. O histórico antigo re-sincroniza só
parcialmente; mensagens antigas podem se perder.

## Erros comuns

| Sintoma | Causa provável | Correção |
|---|---|---|
| `Client outdated (405) connect failure` no log da ponte | whatsmeow pinado no go.mod do upstream ficou velho; WhatsApp rejeita | `go get -u go.mau.fi/whatsmeow@latest && go mod tidy`, aplicar `assets/whatsmeow-context-fix.patch` (a API nova exige `context.Background()` em `client.Download`, `sqlstore.New`, `GetFirstDevice`, `GetGroupInfo`, `GetContact`), rebuild — o setup.sh/ps1 já faz tudo isso |
| Ferramentas `mcp__whatsapp__*` ausentes na sessão | MCP não registrado, ou sessão aberta antes do registro | `claude mcp list`; re-registrar; abrir sessão nova |
| `whatsapp ✘ Failed to connect` no `claude mcp list` | `uv` fora do PATH do spawn, ou path do `--directory` errado | registrar com caminho absoluto do `uv`; conferir o path do clone |
| Envio falha com `Request error` | Ponte parada | iniciar a ponte / keep-alive |
| `BRIDGE: PORT_CONFLICT` no check | Outro processo na porta 8080 | liberar a porta; avançado: trocar a porta em `whatsapp-bridge/main.go` **e** `WHATSAPP_API_BASE_URL` em `whatsapp-mcp-server/whatsapp.py`, e exportar `WHATSAPP_BRIDGE_PORT` para o health check |
| Build falha no Windows com erro de gcc/cgo | go-sqlite3 exige compilador C | instalar MSYS2 + mingw gcc e pôr no PATH (ver pré-requisitos) |
| `git apply` do patch falha no Windows | checkout com CRLF | re-clonar com `git clone -c core.autocrlf=false ...` (o setup.ps1 já faz) |
| QR não aparece | Glitch da ponte | Ctrl+C e rodar de novo |
| Pareamento recusado | Limite de aparelhos vinculados atingido | remover um aparelho antigo no celular (Dispositivos conectados) |
| Mensagens novas não chegam com ponte "up" | Sessão expirada (~20 dias) | re-parear (seção acima) |

## Avisos importantes

- **API não oficial**: o whatsmeow fala o protocolo real do WhatsApp Web, mas não é autorizado pela
  Meta. Uso pessoal, em ritmo humano, raramente dá problema; **envio em massa/automatizado aumenta
  o risco de banimento da conta** — por isso a skill proíbe bulk sends.
- **Privacidade**: `store/messages.db` guarda o histórico em texto claro no disco. Mantenha o clone
  fora de git e de pastas sincronizadas em nuvem.
- **Prompt injection**: mensagens recebidas são entrada não confiável. A skill trata conteúdo de
  mensagem como dado, nunca como instrução, e não envia conteúdo para serviços externos.
