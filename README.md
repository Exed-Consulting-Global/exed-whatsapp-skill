# exed-whatsapp — WhatsApp pessoal no Claude Code

Plugin do Claude Code que permite ao Claude **ler, resumir, buscar e responder** as mensagens do
seu WhatsApp pessoal — com um guard-rail central: **nenhuma mensagem é enviada sem você aprovar o
rascunho explicitamente**.

Cada usuário roda a própria stack, 100% local: uma ponte em Go
([whatsapp-mcp](https://github.com/lharries/whatsapp-mcp), MIT) pareada como "aparelho vinculado"
do seu WhatsApp + um servidor MCP em Python. Suas mensagens ficam na sua máquina; nada vai para a
nuvem.

## Instalação

**1. Instale o plugin** — pelo terminal (funciona em qualquer ambiente):

```bash
claude plugin marketplace add git@github.com:Exed-Consulting-Global/exed-whatsapp-skill.git
claude plugin install exed-whatsapp@exed
```

Ou, numa sessão interativa do `claude` no terminal: `/plugin marketplace add ...` + `/plugin install ...`
(o comando `/plugin` não existe em todos os ambientes — no app desktop, use a CLI acima).

O repo pode ficar **privado**: a URL SSH usa a sua chave do GitHub — basta ter acesso à org. Se o
repo for público, o atalho `Exed-Consulting-Global/exed-whatsapp-skill` também funciona no lugar
da URL.

**2. Instale a stack local** — abra uma sessão nova e peça: *"configura meu whatsapp"* — o Claude
roda o instalador do plugin para você. Ou rode manualmente:

- **macOS / Linux / WSL**: `bash <plugin>/skills/whatsapp/scripts/setup.sh`
- **Windows nativo**: `powershell -ExecutionPolicy Bypass -File <plugin>\skills\whatsapp\scripts\setup.ps1`
  (⚠️ beta — ainda não testado em Windows real; feedback bem-vindo)

O instalador clona o servidor MCP (Python), **baixa a ponte já compilada** do release do GitHub
(nada de Go/gcc) e registra o servidor MCP no Claude Code. Os binários da ponte são gerados por
CI (GitHub Actions) para mac/Windows/Linux e anexados ao release `bridge-v*` — já com o patch do
erro `Client outdated 405` aplicado.

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

A ponte vem **pré-compilada** — sem Go, sem gcc, sem MSYS2. Só ferramentas leves:

| SO | Necessário | Instalar |
|---|---|---|
| macOS | `git`, `gh`, `uv` | `brew install git gh uv` |
| Linux / WSL | `git`, `gh`, `uv`, `curl` | gerenciador do sistema + [cli.github.com](https://cli.github.com) + [astral.sh/uv](https://astral.sh/uv) |
| Windows nativo | `git`, `gh`, `uv` | `winget install Git.Git GitHub.cli astral-sh.uv` |

O `gh` precisa estar autenticado (`gh auth login`) — é o que baixa a ponte do release, inclusive
em repo privado. `ffmpeg` é opcional (áudios com conversão automática); `sqlite3` melhora o
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
