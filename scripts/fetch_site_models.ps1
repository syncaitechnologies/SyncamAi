# Downloads the licence-approved site-box models into a folder outside Git and
# verifies each file against the SHA-384 pinned in
# ai-services/src/syncam_ai/model_artifacts.py. Never commit model files.
param(
    [string]$Destination = "D:\private\models",
    # Development/demo only: this community weapon model has no stated licence.
    [switch]$IncludeDevWeaponModel,
    # Intel Open Model Zoo face models (Apache-2.0) for staff attendance.
    [switch]$IncludeFaceModels
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

if ($IncludeFaceModels) {
    $omz = "https://storage.openvinotoolkit.org/repositories/open_model_zoo/2023.0/models_bin/1"
    $faces = @{
        "face-detection-retail-0004.xml"        = "a7f8d1d41998503c4f3cdd8c12275f04f1736e5142127edcb4c76c3e17188499390574095a5b2a9dd78d3d0f77d02034"
        "face-detection-retail-0004.bin"        = "394185d3e42c34d7f9d43229ec8f5755c07e19fd6469d23883e71707fdd8eb66d90cbd62248db90ff3ba1c94adac599b"
        "landmarks-regression-retail-0009.xml"  = "393eda465df2a886f7edd932e2f1cd6930ae91f85708f91b4cd60e411acfb86e9de9e083907901208e9e8af639846572"
        "landmarks-regression-retail-0009.bin"  = "0e6e830387f56783de313e0f7022b7808fd2e9e0b28b411a65608e2c1df0bcf939c65fb699fc51868df30232d6308515"
        "face-reidentification-retail-0095.xml" = "689fb39e94bbd0d22cfeeb9292d0b91ac753424a7ee2c0555070e4038a00bfcaad473b47303012b9a393027dbad0dbce"
        "face-reidentification-retail-0095.bin" = "6d51703e854509dafdab9dfc6b932136d301d95be6d52c9c23b5a4f5de1ffec80e8d38b87e0e9556ad36f0dded1588ed"
    }
    foreach ($file in $faces.Keys) {
        $model = [IO.Path]::GetFileNameWithoutExtension($file)
        $models += @{ Name = $file; Url = "$omz/$model/FP16/$file"; Sha384 = $faces[$file]; Note = "Intel $model (Apache-2.0)" }
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
