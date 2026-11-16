# PyInstaller build of syncam-box.exe (one folder). Run through installer/build.ps1.
# Licence files of the bundled libraries are kept (copy_metadata) so the
# installed folder carries their notices, including OpenCV's LGPL FFmpeg.
from PyInstaller.utils.hooks import collect_data_files, collect_dynamic_libs, collect_submodules, copy_metadata

datas = collect_data_files("tzdata")
# OpenVINO loads its device plugins (openvino_intel_cpu_plugin.dll, GPU, ...)
# at run time from openvino/libs, so dependency analysis misses them.
binaries = collect_dynamic_libs("openvino")
datas += collect_data_files("openvino", includes=["libs/*.xml"])
for package in ("opencv-python-headless", "openvino", "numpy", "cryptography", "tzdata", "syncam-ai-services"):
    datas += copy_metadata(package)

# NumPy imports some modules from C code (e.g. numpy._core._exceptions), which
# static analysis cannot see; collect them explicitly. Tests and f2py are left out.
numpy_modules = collect_submodules(
    "numpy", filter=lambda name: not any(part in name for part in (".tests", ".f2py", "._pyinstaller", ".distutils"))
)

a = Analysis(
    ["syncam_box.py"],
    hiddenimports=collect_submodules("syncam_ai") + numpy_modules,
    binaries=binaries,
    datas=datas,
    excludes=["tkinter", "matplotlib"],
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
