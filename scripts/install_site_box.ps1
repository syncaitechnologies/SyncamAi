# Installs the SyncCam site worker on a Windows site box (no admin rights
# needed) and registers it to start automatically when the user signs in.
#
#   pwsh scripts/install_site_box.ps1 -Config D:\private\syncam-site.json -PairingCode K7Q4-XM92
#
# The box pairs itself with the one-time code from the dashboard and keeps its
# site key encrypted (Windows DPAPI) in syncam-box.vault next to the config.
# A SYNCAM_SITE_KEY user environment variable, if set, is still used instead.
param(
    [Parameter(Mandatory = $true)][string]$Config,
    [string]$Venv = "D:\private\syncam-site-venv",
    [string]$Models = "D:\private\models",
    # The 8-character code from the dashboard; asked for when not given.
    [string]$PairingCode = "",
    [switch]$NoAutostart
)

$ErrorActionPreference = "Stop"
$repo = Resolve-Path (Join-Path $PSScriptRoot "..")
if (-not (Test-Path $Config)) { throw "Config file not found: $Config (start from ai-services/site-worker.example.json)" }

if (-not (Test-Path (Join-Path $Venv "Scripts\python.exe"))) {
    Write-Host "Creating Python 3.12 environment in $Venv"
    py -3.12 -m venv $Venv
}
$python = Join-Path $Venv "Scripts\python.exe"
& $python -m pip install --quiet --upgrade pip
& $python -m pip install --quiet -e "$repo\ai-services[site-worker]"

& (Join-Path $PSScriptRoot "fetch_site_models.ps1") -Destination $Models

$vault = Join-Path (Split-Path -Parent (Resolve-Path $Config)) "syncam-box.vault"
if (-not [Environment]::GetEnvironmentVariable("SYNCAM_SITE_KEY", "User") -and -not (Test-Path $vault)) {
    if (-not $PairingCode) {
        $PairingCode = Read-Host "Enter the pairing code from the SyncCam dashboard (Configuration > Site box > Pair a site box)"
    }
    & $python -m syncam_ai.site_worker pair --config $Config --code $PairingCode
    if ($LASTEXITCODE -ne 0) { throw "Pairing failed. Create a new code in the dashboard and run this script again." }
}

Write-Host "Checking cameras, model and backend..."
& $python -m syncam_ai.site_worker check --config $Config
if ($LASTEXITCODE -ne 0) { Write-Warning "The check reported problems; fix them before relying on alerts." }

if (-not $NoAutostart) {
    $action = New-ScheduledTaskAction -Execute $python -Argument "-m syncam_ai.site_worker run --config `"$Config`""
    $trigger = New-ScheduledTaskTrigger -AtLogOn -User $env:USERNAME
    $settings = New-ScheduledTaskSettingsSet -RestartCount 999 -RestartInterval (New-TimeSpan -Minutes 1) -ExecutionTimeLimit ([TimeSpan]::Zero) -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries
    Register-ScheduledTask -TaskName "SyncCam Site Worker" -Action $action -Trigger $trigger -Settings $settings -Force | Out-Null
    Start-ScheduledTask -TaskName "SyncCam Site Worker"
    Write-Host "SyncCam Site Worker is running and will start at every sign-in."
}
