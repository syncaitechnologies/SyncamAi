# Downloads the licence-approved site-box models into a folder outside Git and
# verifies each file against the SHA-384 pinned in
# ai-services/src/syncam_ai/model_artifacts.py. Never commit model files.
param(
    [string]$Destination = "D:\private\models",
    # Development/demo only: this community weapon model has no stated licence.
    [switch]$IncludeDevWeaponModel
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
if ($IncludeDevWeaponModel) {
    $models += @{
        Name   = "weapon_dev_yolov8.onnx"
        Url    = "https://huggingface.co/Hadi959/weapon-detection-yolov8/resolve/main/best.onnx"
        Sha384 = "86f286064e66ee2f857ec35eb3e9cf9094e1e7e2594536ddcdd35176be6fed910dfe3683d7ee7254f4e8ea6c5594cde7"
        Note   = "community pistol/knife model (DEVELOPMENT ONLY, no licence)"
    }
}

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
