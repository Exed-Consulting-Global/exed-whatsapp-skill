# Instalador da stack whatsapp-mcp para a skill exed-whatsapp (Windows nativo).
# ⚠️ Beta: ainda não testado em Windows real — reporte problemas no repo do plugin.
#
# Uso (PowerShell):
#   powershell -ExecutionPolicy Bypass -File setup.ps1                  instala tudo
#   powershell -ExecutionPolicy Bypass -File setup.ps1 -TaskScheduler   (rodar DEPOIS do
#       pareamento QR) registra a ponte no Agendador de Tarefas para subir no logon
#
# Config: $env:WHATSAPP_MCP_DIR define o destino do clone (default: %USERPROFILE%\whatsapp-mcp)

param([switch]$TaskScheduler)
$ErrorActionPreference = "Stop"

$McpDir = if ($env:WHATSAPP_MCP_DIR) { $env:WHATSAPP_MCP_DIR } else { Join-Path $env:USERPROFILE "whatsapp-mcp" }
$Upstream  = "https://github.com/lharries/whatsapp-mcp.git"
$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$PatchFile = Join-Path $ScriptDir "..\assets\whatsmeow-context-fix.patch"
$BridgeDir = Join-Path $McpDir "whatsapp-bridge"
$BridgeExe = Join-Path $BridgeDir "whatsapp-bridge.exe"
$ServerDir = Join-Path $McpDir "whatsapp-mcp-server"

# --- modo -TaskScheduler: só registra o auto-start (rodar após o pareamento) ---
if ($TaskScheduler) {
  if (-not (Test-Path $BridgeExe)) { Write-Error "ponte não compilada em $McpDir — rode setup.ps1 sem flags primeiro" }
  Write-Host "==> Registrando tarefa agendada 'ExedWhatsAppBridge' (logon + working dir correto)"
  # WorkingDirectory é obrigatório: a ponte resolve store\*.db relativo ao CWD.
  $action   = New-ScheduledTaskAction -Execute $BridgeExe -WorkingDirectory $BridgeDir
  $trigger  = New-ScheduledTaskTrigger -AtLogOn
  $settings = New-ScheduledTaskSettingsSet -RestartCount 3 -RestartInterval (New-TimeSpan -Minutes 1) -ExecutionTimeLimit (New-TimeSpan -Days 3650)
  Register-ScheduledTask -TaskName "ExedWhatsAppBridge" -Action $action -Trigger $trigger -Settings $settings -Force | Out-Null
  Start-ScheduledTask -TaskName "ExedWhatsAppBridge"
  Write-Host "OK: tarefa registrada e iniciada. Para parar: Stop-ScheduledTask -TaskName ExedWhatsAppBridge"
  exit 0
}

Write-Host "==> Pré-requisitos"
foreach ($c in @("git", "go", "uv")) {
  if (-not (Get-Command $c -ErrorAction SilentlyContinue)) {
    Write-Error "falta '$c' — instale: winget install GoLang.Go astral-sh.uv Git.Git"
  }
}
if (-not (Get-Command gcc -ErrorAction SilentlyContinue)) {
  Write-Error "falta 'gcc': o go-sqlite3 exige CGO no Windows. Instale MSYS2 (winget install MSYS2.MSYS2), depois no shell do MSYS2: pacman -S mingw-w64-ucrt-x86_64-gcc, e adicione C:\msys64\ucrt64\bin ao PATH"
}
if (-not (Get-Command ffmpeg -ErrorAction SilentlyContinue)) {
  Write-Host "aviso: ffmpeg ausente (opcional; só para converter áudios ao enviar)"
}

if (-not (Test-Path (Join-Path $McpDir ".git"))) {
  Write-Host "==> Clonando whatsapp-mcp em $McpDir"
  # autocrlf=false: o patch de context é LF; checkout com CRLF faria o git apply falhar
  git clone -c core.autocrlf=false $Upstream $McpDir
  if ($LASTEXITCODE -ne 0) { Write-Error "git clone falhou" }
} else {
  Write-Host "==> Clone já existe em $McpDir — mantendo"
}

Set-Location $BridgeDir
Write-Host "==> Atualizando whatsmeow (o upstream pina uma versão que o WhatsApp rejeita — erro 405)"
$env:CGO_ENABLED = "1"
go get -u go.mau.fi/whatsmeow@latest
go mod tidy

$mainGo = Get-Content main.go -Raw
if ($mainGo -notmatch [regex]::Escape("client.Download(context.Background()")) {
  Write-Host "==> Aplicando patch de context"
  git -C $McpDir apply $PatchFile
  if ($LASTEXITCODE -ne 0) { Write-Error "patch falhou (upstream mudou? CRLF?) — veja 'Erros comuns' em references/setup.md" }
} else {
  Write-Host "==> Patch de context já aplicado — pulando"
}

Write-Host "==> Compilando a ponte (CGO habilitado)"
go build -o whatsapp-bridge.exe .
if ($LASTEXITCODE -ne 0) { Write-Error "go build falhou — confira se o gcc do MSYS2 está no PATH" }

Write-Host "==> Dependências Python (uv sync)"
Set-Location $ServerDir
uv sync
if ($LASTEXITCODE -ne 0) { Write-Error "uv sync falhou" }

Write-Host "==> Registro do servidor MCP no Claude Code (escopo user)"
$uvPath = (Get-Command uv).Source
if (Get-Command claude -ErrorAction SilentlyContinue) {
  claude mcp remove whatsapp -s user 2>$null | Out-Null
  claude mcp add --scope user whatsapp -- $uvPath --directory $ServerDir run main.py
  if ($LASTEXITCODE -ne 0) { Write-Error "claude mcp add falhou" }
} else {
  Write-Host "aviso: CLI 'claude' não encontrada. Registre manualmente depois:"
  Write-Host "  claude mcp add --scope user whatsapp -- $uvPath --directory $ServerDir run main.py"
}

Write-Host ""
Write-Host "✅ Stack instalada. Próximo passo — parear com o celular:"
Write-Host "    cd $BridgeDir; .\whatsapp-bridge.exe"
Write-Host "Escaneie o QR em WhatsApp > Configurações > Dispositivos conectados > Conectar dispositivo"
Write-Host "e deixe a ponte rodando. Depois abra uma sessão NOVA do Claude Code."
Write-Host "Após o pareamento, para auto-start no logon: setup.ps1 -TaskScheduler"
