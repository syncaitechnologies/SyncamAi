# PyInstaller build of syncam-box.exe (one folder). Run through installer/build.ps1.
# Licence files of the bundled libraries are kept (copy_metadata) so the
# installed folder carries their notices, including OpenCV's LGPL FFmpeg.
from PyInstaller.utils.hooks import collect_data_files, collect_submodules, copy_metadata

datas = collect_data_files("tzdata")
for package in ("opencv-python-headless", "openvino", "numpy", "cryptography", "tzdata", "syncam-ai-services"):
    datas += copy_metadata(package)

a = Analysis(
    ["syncam_box.py"],
    hiddenimports=collect_submodules("syncam_ai"),
    datas=datas,
    excludes=["tkinter", "unittest", "pydoc", "matplotlib"],
    noarchive=False,
)
pyz = PYZ(a.pure)
exe = EXE(
    pyz,
    a.scripts,
    [],
    exclude_binaries=True,
    name="syncam-box",
    console=True,
    upx=False,
)
coll = COLLECT(exe, a.binaries, a.datas, upx=False, name="syncam-box")
