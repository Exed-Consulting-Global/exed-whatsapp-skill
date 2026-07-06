#!/usr/bin/env bash
# Instalador da stack whatsapp-mcp para a skill exed-whatsapp (macOS / Linux / WSL).
#
# Uso:
#   bash setup.sh              instala tudo (clone, patch whatsmeow, build, registro MCP)
#   bash setup.sh --launchd    (macOS, rodar DEPOIS do pareamento QR) instala o serviço
#                              de auto-start e assume a ponte
#
# Config: WHATSAPP_MCP_DIR define o destino do clone (default: ~/whatsapp-mcp)

set -euo pipefail

MCP_DIR="${WHATSAPP_MCP_DIR:-$HOME/whatsapp-mcp}"
UPSTREAM="https://github.com/lharries/whatsapp-mcp.git"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PATCH_FILE="$SCRIPT_DIR/../assets/whatsmeow-context-fix.patch"
PLIST_TEMPLATE="$SCRIPT_DIR/../assets/com.exed.whatsapp-bridge.plist.template"

# --- modo --launchd: só instala o serviço (rodar após o pareamento) ---
if [[ "${1:-}" == "--launchd" ]]; then
  [[ "$(uname)" == "Darwin" ]] || { echo "ERRO: --launchd é só para macOS; no Linux/WSL rode a ponte manualmente ou crie um serviço systemd de usuário"; exit 1; }
  [[ -x "$MCP_DIR/whatsapp-bridge/whatsapp-bridge" ]] || { echo "ERRO: ponte não compilada em $MCP_DIR — rode setup.sh sem flags primeiro"; exit 1; }
  PLIST_DST="$HOME/Library/LaunchAgents/com.exed.whatsapp-bridge.plist"
  sed "s|__MCP_DIR__|$MCP_DIR|g; s|__HOME__|$HOME|g" "$PLIST_TEMPLATE" > "$PLIST_DST"
  pkill -x whatsapp-bridge 2>/dev/null || true
  launchctl bootout "gui/$(id -u)/com.exed.whatsapp-bridge" 2>/dev/null || true
  launchctl bootstrap "gui/$(id -u)" "$PLIST_DST"
  echo "OK: serviço com.exed.whatsapp-bridge instalado e iniciado (log: ~/Library/Logs/whatsapp-bridge.log)"
  exit 0
fi

echo "==> Pré-requisitos"
missing=()
for c in git go curl; do command -v "$c" >/dev/null 2>&1 || missing+=("$c"); done
UV_BIN="$(command -v uv || true)"
[[ -n "$UV_BIN" ]] || missing+=(uv)
if (( ${#missing[@]} )); then
  echo "ERRO: faltam: ${missing[*]}"
  echo "  macOS: brew install go uv"
  echo "  Linux/WSL: go via https://go.dev/dl + curl -LsSf https://astral.sh/uv/install.sh | sh"
  exit 1
fi
command -v ffmpeg  >/dev/null 2>&1 || echo "aviso: ffmpeg ausente (opcional; só para converter áudios ao enviar)"
command -v sqlite3 >/dev/null 2>&1 || echo "aviso: sqlite3 ausente (opcional; melhora o diagnóstico do health check)"

echo "==> Clone do whatsapp-mcp em $MCP_DIR"
if [[ -d "$MCP_DIR/.git" ]]; then
  echo "    já existe — mantendo"
else
  git clone "$UPSTREAM" "$MCP_DIR"
fi

cd "$MCP_DIR/whatsapp-bridge"
echo "==> Atualizando whatsmeow (o upstream pina uma versão que o WhatsApp rejeita — erro 'Client outdated 405')"
go get -u go.mau.fi/whatsmeow@latest
go mod tidy

if grep -q 'client.Download(context.Background()' main.go; then
  echo "==> Patch de context já aplicado — pulando"
else
  echo "==> Aplicando patch de context (assets/whatsmeow-context-fix.patch)"
  git -C "$MCP_DIR" apply "$PATCH_FILE"
fi

echo "==> Compilando a ponte"
go build -o whatsapp-bridge .

echo "==> Dependências Python (uv sync)"
cd "$MCP_DIR/whatsapp-mcp-server"
"$UV_BIN" sync

echo "==> Registro do servidor MCP no Claude Code (escopo user)"
if command -v claude >/dev/null 2>&1; then
  claude mcp remove whatsapp -s user >/dev/null 2>&1 || true
  claude mcp add --scope user whatsapp -- "$UV_BIN" --directory "$MCP_DIR/whatsapp-mcp-server" run main.py
else
  echo "aviso: CLI 'claude' não encontrada. Registre manualmente depois:"
  echo "  claude mcp add --scope user whatsapp -- $UV_BIN --directory $MCP_DIR/whatsapp-mcp-server run main.py"
fi

cat <<EOF

✅ Stack instalada. Próximo passo — parear com o celular:

    cd $MCP_DIR/whatsapp-bridge && ./whatsapp-bridge

Escaneie o QR em WhatsApp > Configurações > Dispositivos conectados > Conectar dispositivo
e deixe a ponte rodando. Depois abra uma sessão NOVA do Claude Code e diga:
"o que chegou no meu whatsapp?"

(macOS) Depois do pareamento, para a ponte subir sozinha no login:
    bash $SCRIPT_DIR/setup.sh --launchd
EOF
