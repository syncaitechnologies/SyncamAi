"""Entry point of syncam-box.exe (PyInstaller)."""

from syncam_ai.site_worker.__main__ import main

if __name__ == "__main__":
    raise SystemExit(main())
