#!/usr/bin/env bash
# Health check da stack whatsapp-mcp (macOS / Linux / WSL / Git Bash no Windows).
# Saída parseável, uma informação por linha:
#   MCP_DIR: <path> | NOT_FOUND
#   BRIDGE: OK | DOWN | PORT_CONFLICT
#   DB: OK | MISSING
#   LAST_MESSAGE: <timestamp> (<idade>)  [se houver mensagens]
#   hint: <próxima ação sugerida>        [quando algo precisa de atenção]
# Exit 0 somente quando BRIDGE: OK e DB: OK.

set -u

PORT="${WHATSAPP_BRIDGE_PORT:-8080}"

# Localiza o clone do whatsapp-mcp: env var tem prioridade, depois locais comuns.
candidates=(
  "${WHATSAPP_MCP_DIR:-}"
  "$HOME/whatsapp-mcp"
  "$HOME/Engineering/git/whatsapp-mcp"
)
MCP_DIR=""
for d in "${candidates[@]}"; do
  if [[ -n "$d" && -d "$d/whatsapp-bridge" ]]; then MCP_DIR="$d"; break; fi
done

if [[ -z "$MCP_DIR" ]]; then
  echo "MCP_DIR: NOT_FOUND"
  echo "BRIDGE: DOWN"
  echo "DB: MISSING"
  echo "hint: clone do whatsapp-mcp não encontrado — rode scripts/setup.sh (macOS/Linux/WSL) ou scripts/setup.ps1 (Windows), ou exporte WHATSAPP_MCP_DIR"
  exit 1
fi
echo "MCP_DIR: $MCP_DIR"

MESSAGES_DB="$MCP_DIR/whatsapp-bridge/store/messages.db"
SESSION_DB="$MCP_DIR/whatsapp-bridge/store/whatsapp.db"
status=0

# Qualquer resposta HTTP (mesmo 4xx/5xx) prova que há um processo vivo na porta.
# Em falha de conexão o curl imprime 000 pelo -w — não acrescentar fallback no ||,
# senão o valor sai duplicado ("000\n000") e o parsing quebra.
http_code=$(curl -s -o /dev/null -w '%{http_code}' --max-time 2 "http://localhost:$PORT/api/send" 2>/dev/null)
[[ -z "$http_code" ]] && http_code="000"

bridge_process_running() {
  if command -v pgrep >/dev/null 2>&1; then
    pgrep -f 'whatsapp-bridge' >/dev/null 2>&1 || pgrep -f 'go run main.go' >/dev/null 2>&1
  elif command -v tasklist.exe >/dev/null 2>&1; then
    # Git Bash no Windows não tem pgrep; tasklist cobre.
    tasklist.exe 2>/dev/null | grep -qi 'whatsapp-bridge'
  else
    # Sem como inspecionar processos: se a porta respondeu, assume que é a ponte.
    return 0
  fi
}

if [[ "$http_code" == "000" ]]; then
  bridge="DOWN"
elif bridge_process_running; then
  bridge="OK"
else
  bridge="PORT_CONFLICT"
fi
echo "BRIDGE: $bridge"

if [[ -f "$MESSAGES_DB" && -f "$SESSION_DB" ]]; then
  echo "DB: OK"
else
  echo "DB: MISSING"
fi

if [[ -f "$MESSAGES_DB" ]] && command -v sqlite3 >/dev/null 2>&1; then
  last_raw=$(sqlite3 "$MESSAGES_DB" "SELECT MAX(timestamp) FROM messages;" 2>/dev/null || true)
  if [[ -n "$last_raw" ]]; then
    last_epoch=$(sqlite3 "$MESSAGES_DB" "SELECT CAST(strftime('%s', MAX(timestamp)) AS INTEGER) FROM messages;" 2>/dev/null || true)
    if [[ -n "$last_epoch" && "$last_epoch" != "0" ]]; then
      age_s=$(( $(date +%s) - last_epoch ))
      if   (( age_s < 3600 ));  then age="$(( age_s / 60 ))min atrás"
      elif (( age_s < 86400 )); then age="$(( age_s / 3600 ))h atrás"
      else                           age="$(( age_s / 86400 ))d atrás"
      fi
      echo "LAST_MESSAGE: $last_raw ($age)"
      # Ponte "viva" mas sem mensagem nova há dias = provável sessão expirada (~20 dias).
      if [[ "$bridge" == "OK" ]] && (( age_s > 172800 )); then
        echo "hint: nenhuma mensagem há mais de 2 dias com a ponte ativa — sessão pode ter expirado; veja re-pareamento em references/setup.md"
      fi
    else
      echo "LAST_MESSAGE: $last_raw (idade desconhecida)"
    fi
  else
    echo "LAST_MESSAGE: (banco vazio — ponte ainda sincronizando ou pareamento incompleto)"
  fi
fi

case "$bridge" in
  DOWN)
    echo "hint: inicie a ponte: cd $MCP_DIR/whatsapp-bridge && ./whatsapp-bridge (Windows: .\\whatsapp-bridge.exe) — ou via keep-alive, veja references/setup.md"
    status=1
    ;;
  PORT_CONFLICT)
    echo "hint: outro processo ocupa a porta $PORT — libere-a ou veja 'Erros comuns' em references/setup.md"
    status=1
    ;;
esac

if [[ ! -f "$MESSAGES_DB" || ! -f "$SESSION_DB" ]]; then
  echo "hint: bancos ausentes em $MCP_DIR/whatsapp-bridge/store — instalação/pareamento incompletos; rode scripts/setup.sh ou setup.ps1, ou veja references/setup.md"
  status=1
fi

exit $status
