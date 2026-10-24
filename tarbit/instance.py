"""Одна копия программы: вторая передает свои аргументы первой и выходит.

Так работают двойной клик по .torrent и magnet-ссылки в браузере, когда программа
уже запущена. Именованный мьютекс ловит одновременный запуск нескольких копий
(выделили пять файлов и нажали Enter), файл instance.json хранит порт и токен.
"""

import ctypes
import json
import os
import time
import urllib.request
from pathlib import Path

from . import APP_NAME
from .paths import write_atomic

ERROR_ALREADY_EXISTS = 183
_mutex = None


def acquire() -> bool:
    """True - мы первая копия."""
    global _mutex
    k32 = ctypes.windll.kernel32
    _mutex = k32.CreateMutexW(None, False, f"Local\\{APP_NAME}")
    return k32.GetLastError() != ERROR_ALREADY_EXISTS


def release():
    global _mutex
    if _mutex:
        ctypes.windll.kernel32.CloseHandle(_mutex)
        _mutex = None


def _file(data_dir: Path) -> Path:
    return data_dir / "instance.json"


def write(data_dir: Path, port: int, token: str):
    write_atomic(_file(data_dir), json.dumps({"port": port, "token": token, "pid": os.getpid()}).encode())


def remove(data_dir: Path):
    _file(data_dir).unlink(missing_ok=True)


def forward(data_dir: Path, args: list[str], show: bool, wait: float = 15) -> bool:
    """Отдает аргументы запущенной копии. Ждет до wait секунд, пока она поднимется."""
    deadline = time.time() + wait
    while True:
        try:
            info = json.loads(_file(data_dir).read_text(encoding="utf-8"))
            base = f"http://127.0.0.1:{info['port']}/api/"
            hdr = {"X-Token": info["token"], "Content-Type": "application/json"}
            if args:
                _post(base + "open", hdr, {"args": args})
            if show or not args:
                _post(base + "show", hdr, {})
            return True
        except Exception:
            if time.time() > deadline:
                return False
            time.sleep(0.3)


def _post(url, headers, body):
    req = urllib.request.Request(url, data=json.dumps(body).encode(), headers=headers, method="POST")
    with urllib.request.urlopen(req, timeout=5) as r:
        r.read()
