# Run by SyncCamSetup.exe (as Administrator) after the files are installed,
# and with -Remove when SyncCam Box is uninstalled.
#
# - Restricts %ProgramData%\SyncCam to SYSTEM and Administrators: the vault is
#   encrypted with machine-scope DPAPI, which any local account could open.
# - Registers the "SyncCam Box" task: runs as SYSTEM at computer start-up
#   (nobody has to sign in), restarts every minute if it stops.
param(
    [string]$Exe = "",
    # Only create and lock the box folder (run before the box writes its vault).
    [switch]$LockOnly,
    [switch]$Remove
)

$ErrorActionPreference = "Stop"
$taskName = "SyncCam Box"
$boxHome = Join-Path $env:ProgramData "SyncCam"

if ($Remove) {
    foreach ($name in @($taskName, "SyncCam Box Update")) {
        Unregister-ScheduledTask -TaskName $name -Confirm:$false -ErrorAction SilentlyContinue
    }
    Get-Process -Name "syncam-box" -ErrorAction SilentlyContinue | Stop-Process -Force
    # The box folder (pairing, recorder passwords, models) is kept so a
    # reinstall continues where it left off. Delete it to remove everything.
    exit 0
}

New-Item -ItemType Directory -Force $boxHome | Out-Null
# Lock the folder itself, then let everything already inside take its
# permissions. Never grant with /T: icacls ignores an (OI)(CI) grant on a
# file but still strips its inherited permissions, which left the settings
# of a reinstalled or updated box readable by nobody, not even SYSTEM.
& icacls.exe $boxHome /inheritance:r /grant:r "*S-1-5-18:(OI)(CI)F" "*S-1-5-32-544:(OI)(CI)F" /Q | Out-Null
if ($LASTEXITCODE -ne 0) { throw "Could not restrict access to $boxHome" }
if (Get-ChildItem -LiteralPath $boxHome -Force -ErrorAction SilentlyContinue) {
    # Take ownership first, so files an older installer left with no
    # permissions can be repaired too.
    & takeown.exe /F $boxHome /R /A /D Y | Out-Null
    & icacls.exe (Join-Path $boxHome "*") /reset /T /C /Q | Out-Null
    if ($LASTEXITCODE -ne 0) { throw "Could not repair the permissions inside $boxHome" }
}
if ($LockOnly) { exit 0 }
if (-not $Exe -or -not (Test-Path $Exe)) { throw "syncam-box.exe not found: $Exe" }

$log = Join-Path $boxHome "logs\syncam-box.log"
$action = New-ScheduledTaskAction -Execute $Exe -Argument "run --log-file `"$log`"" -WorkingDirectory (Split-Path -Parent $Exe)
$trigger = New-ScheduledTaskTrigger -AtStartup
$principal = New-ScheduledTaskPrincipal -UserId "S-1-5-18" -LogonType ServiceAccount -RunLevel Highest
$settings = New-ScheduledTaskSettingsSet -RestartCount 999 -RestartInterval (New-TimeSpan -Minutes 1) `
    -ExecutionTimeLimit ([TimeSpan]::Zero) -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries `
    -StartWhenAvailable -MultipleInstances IgnoreNew
Register-ScheduledTask -TaskName $taskName -Action $action -Trigger $trigger -Principal $principal -Settings $settings -Force | Out-Null
Start-ScheduledTask -TaskName $taskName
Write-Host "SyncCam Box is running and starts with the computer."
