# -*- mode: python ; coding: utf-8 -*-
# Сборка одного exe: build.cmd (или .venv\Scripts\pyinstaller TarBitTorrent.spec)

a = Analysis(
    ["run.py"],
    pathex=[],
    binaries=[],
    datas=[("tarbit/ui", "tarbit/ui")],
    hiddenimports=["pystray._win32"],
    excludes=["unittest", "pydoc", "doctest", "test", "lib2to3", "xmlrpc", "pytest"],
    noarchive=False,
)
pyz = PYZ(a.pure)

exe = EXE(
    pyz,
    a.scripts,
    a.binaries,
    a.datas,
    [],
    name="TarBitTorrent",
    icon="assets/icon.ico",
    version="build/version_info.txt",
    console=False,
    upx=False,
    debug=False,
    strip=False,
    runtime_tmpdir=None,
)
