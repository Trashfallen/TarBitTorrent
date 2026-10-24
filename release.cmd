@echo off
rem Builds a release into release\vVERSION: only TarBitTorrent.exe, it goes to GitHub as is.
rem The version is taken from tarbit\version.py; bump it there first.
setlocal
cd /d "%~dp0"
call "%~dp0build.cmd" || exit /b 1
for /f %%v in ('.venv\Scripts\python -c "from tarbit.version import VERSION; print(VERSION)"') do set "VERSION=%%v"
set "OUT=release\v%VERSION%"
if exist "%OUT%" rmdir /s /q "%OUT%"
mkdir "%OUT%" || exit /b 1
copy /y dist\TarBitTorrent.exe "%OUT%\" >nul || exit /b 1
echo.
echo Release v%VERSION% is ready: %OUT%\TarBitTorrent.exe
echo Next: git tag v%VERSION% ^&^& git push origin v%VERSION%
echo Then attach the exe at https://github.com/Trashfallen/TarBitTorrent/releases/new?tag=v%VERSION%
