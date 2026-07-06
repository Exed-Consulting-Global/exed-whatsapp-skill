#!/usr/bin/env bash
# Instalador da stack whatsapp-mcp para a skill exed-whatsapp (macOS / Linux / WSL).
# Baixa a ponte PRÉ-COMPILADA do release do GitHub — não precisa de Go nem compilador.
#
# Uso:
#   bash setup.sh              instala tudo (clone do server Python, download da ponte, registro MCP)
#   bash setup.sh --launchd    (macOS, rodar DEPOIS do pareamento QR) instala o serviço de auto-start
#
# Config por env:
#   WHATSAPP_MCP_DIR   destino do clone/binário (default: ~/whatsapp-mcp)

set -euo pipefail

MCP_DIR="${WHATSAPP_MCP_DIR:-$HOME/whatsapp-mcp}"
UPSTREAM="https://github.com/lharries/whatsapp-mcp.git"
PLUGIN_REPO="Exed-Consulting-Global/exed-whatsapp-skill"
BRIDGE_RELEASE="bridge-v0.1.1"   # tag do release que contém os binários da ponte
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PLIST_TEMPLATE="$SCRIPT_DIR/../assets/com.exed.whatsapp-bridge.plist.template"

# --- modo --launchd: só instala o serviço (rodar após o pareamento) ---
if [[ "${1:-}" == "--launchd" ]]; then
  [[ "$(uname)" == "Darwin" ]] || { echo "ERRO: --launchd é só para macOS; no Linux/WSL rode a ponte manualmente ou crie um serviço systemd de usuário"; exit 1; }
  [[ -x "$MCP_DIR/whatsapp-bridge/whatsapp-bridge" ]] || { echo "ERRO: ponte não encontrada em $MCP_DIR — rode setup.sh sem flags primeiro"; exit 1; }
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
for c in git gh uv curl; do command -v "$c" >/dev/null 2>&1 || missing+=("$c"); done
if (( ${#missing[@]} )); then
  echo "ERRO: faltam: ${missing[*]}"
  echo "  macOS:     brew install git gh uv"
  echo "  Linux/WSL: git + https://cli.github.com (gh) + curl -LsSf https://astral.sh/uv/install.sh | sh"
  exit 1
fi
UV_BIN="$(command -v uv)"
command -v ffmpeg  >/dev/null 2>&1 || echo "aviso: ffmpeg ausente (opcional; só para converter áudios ao enviar)"
command -v sqlite3 >/dev/null 2>&1 || echo "aviso: sqlite3 ausente (opcional; melhora o diagnóstico do health check)"

if ! gh auth status >/dev/null 2>&1; then
  echo "ERRO: 'gh' não está autenticado (o release da ponte pode ser de repo privado)."
  echo "      Rode: gh auth login"
  exit 1
fi

# Detecta plataforma para escolher o binário certo do release.
os="$(uname -s)"; arch="$(uname -m)"
case "$os" in
  Darwin) os="darwin" ;;
  Linux)  os="linux" ;;
  *) echo "ERRO: SO '$os' não suportado por este instalador; no Windows use setup.ps1"; exit 1 ;;
esac
case "$arch" in
  arm64|aarch64) arch="arm64" ;;
  x86_64|amd64)  arch="amd64" ;;
  *) echo "ERRO: arquitetura '$arch' sem binário publicado"; exit 1 ;;
esac
ASSET="whatsapp-bridge-$os-$arch"

echo "==> Clone do whatsapp-mcp em $MCP_DIR (para o servidor MCP em Python)"
if [[ -d "$MCP_DIR/.git" ]]; then
  echo "    já existe — mantendo"
else
  git clone --depth 1 "$UPSTREAM" "$MCP_DIR"
fi

echo "==> Baixando a ponte pré-compilada ($ASSET, release $BRIDGE_RELEASE)"
mkdir -p "$MCP_DIR/whatsapp-bridge"
gh release download "$BRIDGE_RELEASE" -R "$PLUGIN_REPO" --pattern "$ASSET" --dir "$MCP_DIR/whatsapp-bridge" --clobber
mv -f "$MCP_DIR/whatsapp-bridge/$ASSET" "$MCP_DIR/whatsapp-bridge/whatsapp-bridge"
chmod +x "$MCP_DIR/whatsapp-bridge/whatsapp-bridge"

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

✅ Stack instalada (sem compilar nada). Próximo passo — parear com o celular:

    cd $MCP_DIR/whatsapp-bridge && ./whatsapp-bridge

Escaneie o QR em WhatsApp > Configurações > Dispositivos conectados > Conectar dispositivo
e deixe a ponte rodando. Depois abra uma sessão NOVA do Claude Code e diga:
"o que chegou no meu whatsapp?"

(macOS) Depois do pareamento, para a ponte subir sozinha no login:
    bash $SCRIPT_DIR/setup.sh --launchd
EOF
