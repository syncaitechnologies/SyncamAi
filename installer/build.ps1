# Builds SyncCamSetup.exe: a PyInstaller bundle of the site box wrapped by
# Inno Setup. Used by .github/workflows/site-box-installer.yml; also runs
# locally on Windows with Python 3.12 and Inno Setup 6 installed.
#
#   powershell -ExecutionPolicy Bypass -File installer\build.ps1 [-Python python] [-Version 0.1.0]
#
# Output: dist\SyncCamSetup-<version>.exe and dist\SyncCamSetup.exe (same file,
# stable name for the dashboard's download link). No model files are bundled.
param(
    [string]$Python = "python",
    [string]$Version = ""
)

$ErrorActionPreference = "Stop"
function Invoke-Checked([string]$File, [string[]]$Arguments) {
    & $File @Arguments
    if ($LASTEXITCODE -ne 0) { throw "$File $($Arguments -join ' ') failed with exit code $LASTEXITCODE" }
}

$repo = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$build = Join-Path $repo "build"
$dist = Join-Path $repo "dist"
$versionFile = Join-Path $repo "ai-services\src\syncam_ai\site_worker\version.py"
$sourceVersion = ([regex]::Match((Get-Content $versionFile -Raw), 'VERSION = "([0-9.]+)"')).Groups[1].Value
if (-not $sourceVersion) { throw "Could not read VERSION from $versionFile" }
if (-not $Version) { $Version = $sourceVersion }
if ($Version -ne $sourceVersion) { throw "Version $Version does not match version.py ($sourceVersion)" }

$venv = Join-Path $build "venv"
if (-not (Test-Path (Join-Path $venv "Scripts\python.exe"))) { Invoke-Checked $Python @("-m", "venv", $venv) }
$py = Join-Path $venv "Scripts\python.exe"
Invoke-Checked $py @("-m", "pip", "install", "--quiet", "--upgrade", "pip")
Invoke-Checked $py @("-m", "pip", "install", "--quiet", "-r", (Join-Path $PSScriptRoot "requirements-build.txt"), "$repo\ai-services[site-worker]")

$bundle = Join-Path $build "pyinstaller\dist\syncam-box"
Push-Location $PSScriptRoot
try {
    Invoke-Checked $py @("-m", "PyInstaller", "--noconfirm", "--clean", "--log-level", "WARN",
        "--distpath", (Join-Path $build "pyinstaller\dist"), "--workpath", (Join-Path $build "pyinstaller\work"), "syncam-box.spec")
} finally {
    Pop-Location
}

$exe = Join-Path $bundle "syncam-box.exe"
Invoke-Checked $exe @("--version")
Invoke-Checked $exe @("selftest")

$iscc = @(
    (Get-Command iscc.exe -ErrorAction SilentlyContinue | Select-Object -ExpandProperty Source),
    (Join-Path ${env:ProgramFiles(x86)} "Inno Setup 6\ISCC.exe"),
    (Join-Path $env:ProgramFiles "Inno Setup 6\ISCC.exe"),
    (Join-Path $env:LOCALAPPDATA "Programs\Inno Setup 6\ISCC.exe")
) | Where-Object { $_ -and (Test-Path $_) } | Select-Object -First 1
if (-not $iscc) { throw "Inno Setup 6 (ISCC.exe) was not found." }

New-Item -ItemType Directory -Force $dist | Out-Null
Invoke-Checked $iscc @("/Q", "/DAppVersion=$Version", "/DBundleDir=$bundle", "/O$dist", (Join-Path $PSScriptRoot "SyncCamBox.iss"))
$installer = Join-Path $dist "SyncCamSetup-$Version.exe"
Copy-Item $installer (Join-Path $dist "SyncCamSetup.exe") -Force
$hash = (Get-FileHash $installer -Algorithm SHA256).Hash.ToLower()
Write-Host "Built $installer ($([math]::Round((Get-Item $installer).Length / 1MB)) MB, SHA-256 $hash)"
