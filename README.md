# exed-whatsapp — WhatsApp pessoal no Claude Code

Plugin do Claude Code que permite ao Claude **ler, resumir, buscar e responder** as mensagens do
seu WhatsApp pessoal — com um guard-rail central: **nenhuma mensagem é enviada sem você aprovar o
rascunho explicitamente**.

Cada usuário roda a própria stack, 100% local: uma ponte em Go
([whatsapp-mcp](https://github.com/lharries/whatsapp-mcp), MIT) pareada como "aparelho vinculado"
do seu WhatsApp + um servidor MCP em Python. Suas mensagens ficam na sua máquina; nada vai para a
nuvem.

## Instalação

**1. Instale o plugin** (numa sessão do Claude Code):

```
/plugin marketplace add Exed-Consulting-Global/exed-whatsapp-skill
/plugin install exed-whatsapp@exed
```

**2. Instale a stack local** — abra uma sessão nova e peça: *"configura meu whatsapp"* — o Claude
roda o instalador do plugin para você. Ou rode manualmente:

- **macOS / Linux / WSL**: `bash <plugin>/skills/whatsapp/scripts/setup.sh`
- **Windows nativo**: `powershell -ExecutionPolicy Bypass -File <plugin>\skills\whatsapp\scripts\setup.ps1`
  (⚠️ beta — ainda não testado em Windows real; feedback bem-vindo)

O instalador clona o whatsapp-mcp, atualiza o whatsmeow (upstream pina uma versão que o WhatsApp
rejeita com `Client outdated 405` — aplicamos um patch já validado), compila a ponte e registra o
servidor MCP no Claude Code.

**3. Pareie com o celular** (uma vez a cada ~20 dias):

```bash
cd ~/whatsapp-mcp/whatsapp-bridge && ./whatsapp-bridge     # Windows: .\whatsapp-bridge.exe
```

Escaneie o QR em **WhatsApp → Configurações → Dispositivos conectados → Conectar dispositivo** e
deixe a ponte rodando.

**4. Use** — abra uma sessão nova do Claude Code e fale naturalmente:

- *"o que chegou no meu zap hoje?"*
- *"resume o grupo X"*
- *"responde a Maria que fechamos pra sexta"* → o Claude mostra o rascunho e **só envia com seu OK**

## Keep-alive (opcional, recomendado)

A ponte só sincroniza mensagens enquanto roda. Para subir sozinha no login:

- **macOS**: `bash setup.sh --launchd` (depois do pareamento)
- **Windows**: `setup.ps1 -TaskScheduler` (depois do pareamento)

## Pré-requisitos

| SO | Necessário |
|---|---|
| macOS | `go`, `uv`, `git` (ex.: `brew install go uv`) |
| Linux / WSL | `go`, `uv`, `git`, `curl` |
| Windows nativo | `go`, `uv`, `git` **e um gcc** (go-sqlite3 exige CGO — ex.: `winget install MSYS2.MSYS2`) |

`ffmpeg` é opcional (só para enviar áudios com conversão automática). `sqlite3` melhora o
diagnóstico do health check.

## Avisos

- **API não oficial**: a ponte usa o protocolo do WhatsApp Web via biblioteca não autorizada pela
  Meta. Uso pessoal em ritmo humano raramente dá problema; automação em massa aumenta o risco de
  banimento — por isso a skill proíbe envios em massa.
- **Privacidade**: o histórico fica em SQLite **em texto claro** (`whatsapp-bridge/store/`).
  Mantenha o clone fora de git e de pastas sincronizadas em nuvem.
- **Prompt injection**: a skill trata conteúdo de mensagem como dado, nunca como instrução, e não
  envia conteúdo de mensagens para serviços externos.

## Estrutura

```
.claude-plugin/           manifests do plugin e do marketplace
skills/whatsapp/
├── SKILL.md              comportamento do Claude (fluxos, guard-rails)
├── references/setup.md   instalação detalhada, re-pareamento, erros comuns
├── scripts/
│   ├── check-bridge.sh   health check multiplataforma
│   ├── setup.sh          instalador macOS/Linux/WSL (+ --launchd)
│   └── setup.ps1         instalador Windows nativo (+ -TaskScheduler)
└── assets/
    ├── whatsmeow-context-fix.patch          correção do erro 405 (main.go)
    └── com.exed.whatsapp-bridge.plist.template   launchd (macOS)
```

O patch em `assets/` modifica o `main.go` do [whatsapp-mcp](https://github.com/lharries/whatsapp-mcp)
(licença MIT, © Luke Harries) para compatibilidade com o whatsmeow atual.
