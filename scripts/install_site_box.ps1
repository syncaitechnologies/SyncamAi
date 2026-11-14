# Installs the SyncCam site worker on a Windows site box (no admin rights
# needed) and registers it to start automatically when the user signs in.
#
#   pwsh scripts/install_site_box.ps1 -Config D:\private\syncam-site.json
#
# The site key is stored as a user environment variable, never in the repo.
param(
    [Parameter(Mandatory = $true)][string]$Config,
    [string]$Venv = "D:\private\syncam-site-venv",
    [string]$Models = "D:\private\models",
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

if (-not [Environment]::GetEnvironmentVariable("SYNCAM_SITE_KEY", "User")) {
    $secure = Read-Host "Paste the site key from the SyncCam dashboard (Configuration > Site boxes)" -AsSecureString
    $plain = [Runtime.InteropServices.Marshal]::PtrToStringAuto([Runtime.InteropServices.Marshal]::SecureStringToBSTR($secure))
    [Environment]::SetEnvironmentVariable("SYNCAM_SITE_KEY", $plain, "User")
    $env:SYNCAM_SITE_KEY = $plain
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
