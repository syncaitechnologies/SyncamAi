# Downloads the licence-approved site-box models into a folder outside Git and
# verifies each file against the SHA-384 pinned in
# ai-services/src/syncam_ai/model_artifacts.py. Never commit model files.
param(
    [string]$Destination = "D:\private\models"
)

$ErrorActionPreference = "Stop"
$models = @(
    @{
        Name   = "yolox_s.onnx"
        Url    = "https://github.com/Megvii-BaseDetection/YOLOX/releases/download/0.1.1rc0/yolox_s.onnx"
        Sha384 = "58e4d3a9701466eb6e3158c17dfe9a11dfe97c7dbaa033bc4f8fe01c6e8c637c3b4972ac764c8709fcae291b1fc72f19"
        Note   = "YOLOX-s, Apache-2.0: people and vehicles"
    }
)

New-Item -ItemType Directory -Force $Destination | Out-Null
foreach ($model in $models) {
    $target = Join-Path $Destination $model.Name
    if (-not (Test-Path $target)) {
        Write-Host "Downloading $($model.Note)..."
        Invoke-WebRequest -Uri $model.Url -OutFile $target -UseBasicParsing
    }
    $actual = (Get-FileHash $target -Algorithm SHA384).Hash.ToLower()
    if ($actual -ne $model.Sha384) {
        Remove-Item $target
        throw "Checksum mismatch for $($model.Name); the file was deleted."
    }
    Write-Host "OK  $target"
}
