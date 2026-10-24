"""Окно программы - Edge в режиме приложения (--app): без вкладок и адресной строки.

Edge есть в любой Windows 10/11. Отдельный профиль в папке данных, чтобы окно
не смешивалось с обычным браузером пользователя.
"""

import ctypes
import ctypes.wintypes as wt
import os
import subprocess
import time
import webbrowser
import winreg
from pathlib import Path

from . import APP_NAME

user32 = ctypes.windll.user32
EnumWindowsProc = ctypes.WINFUNCTYPE(wt.BOOL, wt.HWND, wt.LPARAM)
_last_launch = 0.0


def find_edge() -> str:
    for root in (winreg.HKEY_LOCAL_MACHINE, winreg.HKEY_CURRENT_USER):
        try:
            with winreg.OpenKey(root, r"SOFTWARE\Microsoft\Windows\CurrentVersion\App Paths\msedge.exe") as k:
                p = winreg.QueryValue(k, None)
                if p and os.path.isfile(p):
                    return p
        except OSError:
            pass
    for env in ("ProgramFiles(x86)", "ProgramFiles", "LOCALAPPDATA"):
        base = os.environ.get(env)
        if base:
            p = os.path.join(base, "Microsoft", "Edge", "Application", "msedge.exe")
            if os.path.isfile(p):
                return p
    return ""


def app_windows() -> list[int]:
    """Окна Edge, у которых заголовок заканчивается на имя программы."""
    found = []

    def cb(hwnd, _):
        if not user32.IsWindowVisible(hwnd):
            return True
        cls = ctypes.create_unicode_buffer(64)
        user32.GetClassNameW(hwnd, cls, 64)
        if cls.value != "Chrome_WidgetWin_1":
            return True
        n = user32.GetWindowTextLengthW(hwnd)
        buf = ctypes.create_unicode_buffer(n + 1)
        user32.GetWindowTextW(hwnd, buf, n + 1)
        if buf.value.endswith(APP_NAME):
            found.append(hwnd)
        return True

    user32.EnumWindows(EnumWindowsProc(cb), 0)
    return found


def focus(hwnd: int):
    SW_RESTORE = 9
    if user32.IsIconic(hwnd):
        user32.ShowWindow(hwnd, SW_RESTORE)
    # без этого Windows иногда только мигает кнопкой на панели задач
    user32.keybd_event(0x12, 0, 0, 0)
    user32.SetForegroundWindow(hwnd)
    user32.keybd_event(0x12, 0, 2, 0)


def close(hwnd: int):
    WM_CLOSE = 0x0010
    user32.PostMessageW(hwnd, WM_CLOSE, 0, 0)


def open_window(url: str, profile: Path, reuse: bool = True):
    global _last_launch
    wins = app_windows()
    if not wins and time.time() - _last_launch < 6:
        return   # Edge еще открывает окно, запущенное только что
    if wins and reuse:
        focus(wins[0])
        for w in wins[1:]:
            close(w)
        return
    for w in wins:   # окна прошлого запуска знают старый токен
        close(w)
    edge = find_edge()
    if edge:
        profile.mkdir(parents=True, exist_ok=True)
        try:
            subprocess.Popen([
                edge,
                f"--app={url}",
                f"--user-data-dir={profile}",
                "--window-size=1360,860",
                "--no-first-run",
                "--no-default-browser-check",
                "--disable-features=Translate,msEdgeSidebarV2,msUndersideButton,msImplicitSignin",
            ], creationflags=subprocess.CREATE_NO_WINDOW)
            _last_launch = time.time()
            return
        except OSError:
            pass
    webbrowser.open(url)
