"""Связь с Windows: автозапуск, magnet-ссылки и файлы .torrent.

Все пишется в HKCU, права администратора не нужны. Программа регистрируется в
списке «Приложения по умолчанию»; сам выбор Windows 10/11 оставляет за
пользователем, поэтому кнопка в настройках открывает нужную страницу параметров.
"""

import ctypes
import os
import winreg

from . import APP_NAME

RUN_KEY = r"Software\Microsoft\Windows\CurrentVersion\Run"
CLASSES = r"Software\Classes"
PROG_TORRENT = f"{APP_NAME}.torrent"
PROG_MAGNET = f"{APP_NAME}.magnet"
CAPS = rf"Software\{APP_NAME}\Capabilities"


def _set(root, path, name, value, kind=winreg.REG_SZ):
    with winreg.CreateKeyEx(root, path, 0, winreg.KEY_SET_VALUE) as k:
        winreg.SetValueEx(k, name, 0, kind, value)


def _get(root, path, name=""):
    try:
        with winreg.OpenKey(root, path) as k:
            return winreg.QueryValueEx(k, name)[0]
    except OSError:
        return None


def _notify_shell():
    SHCNE_ASSOCCHANGED, SHCNF_IDLIST = 0x08000000, 0
    ctypes.windll.shell32.SHChangeNotify(SHCNE_ASSOCCHANGED, SHCNF_IDLIST, None, None)


# ---------- автозапуск ----------

def autostart_enabled() -> bool:
    return _get(winreg.HKEY_CURRENT_USER, RUN_KEY, APP_NAME) is not None


def set_autostart(exe: str, on: bool):
    if on:
        _set(winreg.HKEY_CURRENT_USER, RUN_KEY, APP_NAME, f'"{exe}" --minimized')
        return
    try:
        with winreg.OpenKey(winreg.HKEY_CURRENT_USER, RUN_KEY, 0, winreg.KEY_SET_VALUE) as k:
            winreg.DeleteValue(k, APP_NAME)
    except OSError:
        pass


# ---------- ассоциации ----------

def register(exe: str):
    """ProgID и Capabilities: программа появляется в списке приложений по умолчанию.

    Это безопасно делать при каждом запуске: так путь к exe остается актуальным,
    если программу перенесли. Чужие ассоциации не трогаются.
    """
    hk = winreg.HKEY_CURRENT_USER
    cmd = f'"{exe}" "%1"'
    icon = f'"{exe}",0'
    _set(hk, rf"{CLASSES}\{PROG_TORRENT}", "", "Торрент-файл")
    _set(hk, rf"{CLASSES}\{PROG_TORRENT}\DefaultIcon", "", icon)
    _set(hk, rf"{CLASSES}\{PROG_TORRENT}\shell\open\command", "", cmd)
    _set(hk, rf"{CLASSES}\{PROG_MAGNET}", "", "Magnet-ссылка")
    _set(hk, rf"{CLASSES}\{PROG_MAGNET}", "URL Protocol", "")
    _set(hk, rf"{CLASSES}\{PROG_MAGNET}\DefaultIcon", "", icon)
    _set(hk, rf"{CLASSES}\{PROG_MAGNET}\shell\open\command", "", cmd)
    _set(hk, rf"{CLASSES}\.torrent\OpenWithProgids", PROG_TORRENT, b"", winreg.REG_NONE)
    _set(hk, rf"{CLASSES}\Applications\{os.path.basename(exe)}\shell\open\command", "", cmd)
    _set(hk, CAPS, "ApplicationName", APP_NAME)
    _set(hk, CAPS, "ApplicationDescription", "Торрент-клиент")
    _set(hk, rf"{CAPS}\FileAssociations", ".torrent", PROG_TORRENT)
    _set(hk, rf"{CAPS}\URLAssociations", "magnet", PROG_MAGNET)
    _set(hk, r"Software\RegisteredApplications", APP_NAME, CAPS)


def claim_defaults(exe: str):
    """Ставит программу обработчиком там, где пользователь еще ничего не выбирал явно."""
    register(exe)
    hk = winreg.HKEY_CURRENT_USER
    cmd = f'"{exe}" "%1"'
    _set(hk, rf"{CLASSES}\.torrent", "", PROG_TORRENT)
    _set(hk, rf"{CLASSES}\magnet", "", "URL:Magnet")
    _set(hk, rf"{CLASSES}\magnet", "URL Protocol", "")
    _set(hk, rf"{CLASSES}\magnet\DefaultIcon", "", f'"{exe}",0')
    _set(hk, rf"{CLASSES}\magnet\shell\open\command", "", cmd)
    _notify_shell()


def associations() -> dict:
    """Кто сейчас открывает magnet и .torrent (с учетом явного выбора пользователя)."""
    hk = winreg.HKEY_CURRENT_USER
    base = r"Software\Microsoft\Windows\Shell\Associations\UrlAssociations\magnet\UserChoice"
    magnet = _get(hk, base, "ProgId")
    if magnet is None:
        cmd = _get(hk, rf"{CLASSES}\magnet\shell\open\command") or ""
        magnet = PROG_MAGNET if APP_NAME.lower() in cmd.lower() else (cmd and "другая программа")
    torrent = _get(hk, r"Software\Microsoft\Windows\CurrentVersion\Explorer\FileExts\.torrent\UserChoice", "ProgId")
    if torrent is None:
        torrent = _get(hk, rf"{CLASSES}\.torrent")
    return {"magnet": magnet == PROG_MAGNET, "torrent": torrent == PROG_TORRENT}


def open_default_apps():
    os.startfile(f"ms-settings:defaultapps?registeredAppUser={APP_NAME}")  # noqa: S606
