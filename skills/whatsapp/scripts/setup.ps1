# Instalador da stack whatsapp-mcp para a skill exed-whatsapp (Windows nativo).
# Baixa a ponte PRÉ-COMPILADA do release do GitHub — não precisa de Go, gcc nem MSYS2.
# ⚠️ Beta: ainda não testado em Windows real — reporte problemas no repo do plugin.
#
# Uso (PowerShell):
#   powershell -ExecutionPolicy Bypass -File setup.ps1                  instala tudo
#   powershell -ExecutionPolicy Bypass -File setup.ps1 -TaskScheduler   (após o pareamento QR)
#       registra a ponte no Agendador de Tarefas para subir no logon
#
# Config: $env:WHATSAPP_MCP_DIR define o destino (default: %USERPROFILE%\whatsapp-mcp)

param([switch]$TaskScheduler)
$ErrorActionPreference = "Stop"

$McpDir = if ($env:WHATSAPP_MCP_DIR) { $env:WHATSAPP_MCP_DIR } else { Join-Path $env:USERPROFILE "whatsapp-mcp" }
$Upstream      = "https://github.com/lharries/whatsapp-mcp.git"
$PluginRepo    = "Exed-Consulting-Global/exed-whatsapp-skill"
$BridgeRelease = "bridge-v0.1.0"   # tag do release com os binários da ponte
$BridgeDir  = Join-Path $McpDir "whatsapp-bridge"
$BridgeExe  = Join-Path $BridgeDir "whatsapp-bridge.exe"
$ServerDir  = Join-Path $McpDir "whatsapp-mcp-server"
$Asset      = "whatsapp-bridge-windows-amd64.exe"

# --- modo -TaskScheduler: só registra o auto-start (rodar após o pareamento) ---
if ($TaskScheduler) {
  if (-not (Test-Path $BridgeExe)) { Write-Error "ponte não encontrada em $McpDir — rode setup.ps1 sem flags primeiro" }
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
foreach ($c in @("git", "gh", "uv")) {
  if (-not (Get-Command $c -ErrorAction SilentlyContinue)) {
    Write-Error "falta '$c' — instale: winget install Git.Git GitHub.cli astral-sh.uv"
  }
}
if (-not (Get-Command ffmpeg -ErrorAction SilentlyContinue)) {
  Write-Host "aviso: ffmpeg ausente (opcional; só para converter áudios ao enviar)"
}
& gh auth status *> $null
if ($LASTEXITCODE -ne 0) { Write-Error "'gh' não está autenticado (o release pode ser de repo privado). Rode: gh auth login" }

if (-not (Test-Path (Join-Path $McpDir ".git"))) {
  Write-Host "==> Clonando whatsapp-mcp em $McpDir (para o servidor MCP em Python)"
  git clone --depth 1 $Upstream $McpDir
  if ($LASTEXITCODE -ne 0) { Write-Error "git clone falhou" }
} else {
  Write-Host "==> Clone já existe em $McpDir — mantendo"
}

Write-Host "==> Baixando a ponte pré-compilada ($Asset, release $BridgeRelease)"
New-Item -ItemType Directory -Force -Path $BridgeDir | Out-Null
gh release download $BridgeRelease -R $PluginRepo --pattern $Asset --dir $BridgeDir --clobber
if ($LASTEXITCODE -ne 0) { Write-Error "download do binário falhou (release/tag existe? gh autenticado?)" }
Move-Item -Force (Join-Path $BridgeDir $Asset) $BridgeExe

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
Write-Host "✅ Stack instalada (sem compilar nada). Próximo passo — parear com o celular:"
Write-Host "    cd $BridgeDir; .\whatsapp-bridge.exe"
Write-Host "Escaneie o QR em WhatsApp > Configurações > Dispositivos conectados > Conectar dispositivo"
Write-Host "e deixe a ponte rodando. Depois abra uma sessão NOVA do Claude Code."
Write-Host "Após o pareamento, para auto-start no logon: setup.ps1 -TaskScheduler"
