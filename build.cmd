@echo off
rem Builds dist\TarBitTorrent.exe (one file, no console).
rem Needs Python 3.12 (py -3.12): libtorrent has no wheels for newer Python yet.
setlocal
cd /d "%~dp0"
if not exist .venv\Scripts\python.exe (
  py -3.12 -m venv .venv || exit /b 1
)
.venv\Scripts\python -m pip install -q -r requirements.txt || exit /b 1
.venv\Scripts\python tools\version_info.py || exit /b 1
.venv\Scripts\pyinstaller --noconfirm --clean --log-level WARN TarBitTorrent.spec || exit /b 1
echo.
echo Built: dist\TarBitTorrent.exe
