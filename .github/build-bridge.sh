#!/usr/bin/env bash
# Compila a ponte whatsapp-bridge corrigida. Chamado pela CI em cada SO (bash no
# Unix, msys2 bash no Windows) — um único ponto de verdade para o build.
# Uso: build-bridge.sh <nome-do-asset>
# Requer no ambiente: UPSTREAM_REPO, UPSTREAM_COMMIT, WHATSMEOW_VERSION.
set -euo pipefail

ASSET="$1"
: "${UPSTREAM_REPO:?}"; : "${UPSTREAM_COMMIT:?}"; : "${WHATSMEOW_VERSION:?}"

ROOT="$(pwd)"
PATCH="$ROOT/skills/whatsapp/assets/whatsmeow-context-fix.patch"

# LF consistente: o patch é LF; garante que o clone do upstream também seja, senão
# no Windows o contexto do patch não bate.
git config --global core.autocrlf false

rm -rf upstream
git clone "$UPSTREAM_REPO" upstream
cd upstream
git checkout --quiet "$UPSTREAM_COMMIT"
git apply "$PATCH"

cd whatsapp-bridge
go get "go.mau.fi/whatsmeow@$WHATSMEOW_VERSION"
go mod tidy
# -o com nome simples (sem path) evita o go do Windows tropeçar em path POSIX do msys2.
CGO_ENABLED=1 go build -trimpath -o "$ASSET" .
mv -f "$ASSET" "$ROOT/$ASSET"
echo "built: $ROOT/$ASSET"
