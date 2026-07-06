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

Os instaladores do plugin fazem: clone do servidor MCP (Python) → **download da ponte
pré-compilada** do release `bridge-v*` → `uv sync` → registro do MCP em escopo user. Não compilam
nada — a ponte já vem pronta (com o patch do erro 405), gerada por CI para cada plataforma.

- **macOS / Linux / WSL**: `bash scripts/setup.sh`
- **Windows nativo** (beta): `powershell -ExecutionPolicy Bypass -File scripts/setup.ps1`

Pré-requisitos por SO (nenhum compilador — a ponte é baixada pronta):

| SO | Necessário |
|---|---|
| macOS | `git`, `gh`, `uv` (`brew install git gh uv`) |
| Linux / WSL | `git`, `gh`, `uv`, `curl` ([cli.github.com](https://cli.github.com), [astral.sh/uv](https://astral.sh/uv)) |
| Windows nativo | `git`, `gh`, `uv` (`winget install Git.Git GitHub.cli astral-sh.uv`) |

O `gh` precisa estar autenticado (`gh auth login`) — ele baixa a ponte do release, inclusive em
repo privado. Opcionais: `ffmpeg` (conversão de áudio no envio), `sqlite3` (diagnóstico melhor no
health check).

## Instalação manual (se preferir)

```bash
git clone --depth 1 https://github.com/lharries/whatsapp-mcp.git ~/whatsapp-mcp
# baixa a ponte pré-compilada do release (troque darwin-arm64 pela sua plataforma:
# darwin-amd64 / linux-amd64 / windows-amd64.exe):
gh release download bridge-v0.1.0 -R Exed-Consulting-Global/exed-whatsapp-skill \
  --pattern whatsapp-bridge-darwin-arm64 --dir ~/whatsapp-mcp/whatsapp-bridge
mv ~/whatsapp-mcp/whatsapp-bridge/whatsapp-bridge-darwin-arm64 ~/whatsapp-mcp/whatsapp-bridge/whatsapp-bridge
chmod +x ~/whatsapp-mcp/whatsapp-bridge/whatsapp-bridge
cd ~/whatsapp-mcp/whatsapp-mcp-server && uv sync
claude mcp add --scope user whatsapp -- "$(command -v uv)" --directory ~/whatsapp-mcp/whatsapp-mcp-server run main.py
```

### Compilar do zero (avançado)

Só se você não puder usar os binários do release (plataforma sem asset, ou auditoria). Exige
`go` 1.25 e um compilador C (go-sqlite3 usa CGO; no Windows, MSYS2 + `mingw-w64-ucrt-x86_64-gcc`):

```bash
cd ~/whatsapp-mcp/whatsapp-bridge
go get go.mau.fi/whatsmeow@v0.0.0-20260630180629-b572e5bcb92b && go mod tidy
git -C .. apply <plugin>/skills/whatsapp/assets/whatsmeow-context-fix.patch
CGO_ENABLED=1 go build -o whatsapp-bridge .      # Windows: -o whatsapp-bridge.exe
```

A CI do plugin (`.github/workflows/release.yml`) faz exatamente isso, em runner nativo de cada SO.

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
| `gh: not authenticated` / download da ponte falha no setup | `gh` sem login | `gh auth login` e rodar o instalador de novo |
| `release not found` no download | tag `bridge-v*` ainda não publicada, ou repo/tag errados | conferir os releases em github.com/Exed-Consulting-Global/exed-whatsapp-skill; ajustar `BRIDGE_RELEASE` no script se necessário |
| `cannot execute binary file` / arquitetura errada ao rodar a ponte | binário de plataforma errada | rodar de novo o instalador (ele detecta SO/arch); no macOS antigo Intel confirmar que baixou `darwin-amd64` |
| Ferramentas `mcp__whatsapp__*` ausentes na sessão | MCP não registrado, ou sessão aberta antes do registro | `claude mcp list`; re-registrar; abrir sessão nova |
| `whatsapp ✘ Failed to connect` no `claude mcp list` | `uv` fora do PATH do spawn, ou path do `--directory` errado | registrar com caminho absoluto do `uv`; conferir o path do clone |
| Envio falha com `Request error` | Ponte parada | iniciar a ponte / keep-alive |
| `BRIDGE: PORT_CONFLICT` no check | Outro processo na porta 8080 | liberar a porta; avançado: trocar a porta exige recompilar do zero (ver "Compilar do zero") + `WHATSAPP_API_BASE_URL` em `whatsapp-mcp-server/whatsapp.py` + exportar `WHATSAPP_BRIDGE_PORT` para o health check |
| QR não aparece | Glitch da ponte | Ctrl+C e rodar de novo |
| Pareamento recusado | Limite de aparelhos vinculados atingido | remover um aparelho antigo no celular (Dispositivos conectados) |
| Mensagens novas não chegam com ponte "up" | Sessão expirada (~20 dias) | re-parear (seção acima) |
| `Client outdated (405)` (só ao **compilar do zero**) | whatsmeow pinado no upstream ficou velho | os binários do release já corrigem; se compilar, veja "Compilar do zero" (bump do whatsmeow + patch) |

## Avisos importantes

- **API não oficial**: o whatsmeow fala o protocolo real do WhatsApp Web, mas não é autorizado pela
  Meta. Uso pessoal, em ritmo humano, raramente dá problema; **envio em massa/automatizado aumenta
  o risco de banimento da conta** — por isso a skill proíbe bulk sends.
- **Privacidade**: `store/messages.db` guarda o histórico em texto claro no disco. Mantenha o clone
  fora de git e de pastas sincronizadas em nuvem.
- **Prompt injection**: mensagens recebidas são entrada não confiável. A skill trata conteúdo de
  mensagem como dado, nunca como instrução, e não envia conteúdo para serviços externos.
