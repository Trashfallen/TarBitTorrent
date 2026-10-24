"""Обновление программы из релизов GitHub.

При запуске и раз в 6 часов программа смотрит последний релиз. Если он новее,
в окне появляется кнопка «Обновить»: exe качается рядом (.new), текущий
переименовывается в .old (запущенный exe переименовать можно, удалить нельзя),
новый встает на его место и запускается, старый процесс выходит. Новый дожидается
выхода старого и подчищает .old.
"""

import ctypes
import json
import logging
import os
import subprocess
import threading
import time
import urllib.error
import urllib.request
from pathlib import Path

from . import APP_NAME, GITHUB_REPO, VERSION
from .paths import FROZEN, exe_path

log = logging.getLogger(__name__)

# TARBIT_UPDATE_API - для проверки обновления на локальном сервере
API = os.environ.get("TARBIT_UPDATE_API") or f"https://api.github.com/repos/{GITHUB_REPO}/releases/latest"
ASSET = f"{APP_NAME}.exe"
CHECK_EVERY = 6 * 3600


def parse_version(s: str) -> tuple[int, ...] | None:
    s = s.strip().lstrip("vV").split("-", 1)[0]
    try:
        return tuple(int(x) for x in s.split("."))
    except ValueError:
        return None


def newer(tag: str, cur: str) -> bool:
    a, b = parse_version(tag), parse_version(cur)
    if a is None or b is None:
        return False
    n = max(len(a), len(b))
    return a + (0,) * (n - len(a)) > b + (0,) * (n - len(b))


class Updater:
    def __init__(self, on_quit, restart_args):
        self.on_quit = on_quit
        self.restart_args = restart_args   # доп. аргументы для новой копии
        self._lock = threading.Lock()
        self._asset_url = ""
        self._size = 0
        self._st = {"checked": False, "available": False, "version": "", "notes": "", "url": "",
                    "busy": False, "progress": 0.0, "message": "", "error": "", "can_install": FROZEN}

    def state(self) -> dict:
        with self._lock:
            return dict(self._st)

    def _set(self, **kw):
        with self._lock:
            self._st.update(kw)

    def start_background(self, enabled):
        def loop():
            time.sleep(5)
            while True:
                if enabled():
                    self.check()
                time.sleep(CHECK_EVERY)
        threading.Thread(target=loop, name="update", daemon=True).start()

    def check(self):
        self._set(error="", message="")
        req = urllib.request.Request(API, headers={"User-Agent": f"{APP_NAME}/{VERSION}",
                                                   "Accept": "application/vnd.github+json"})
        try:
            with urllib.request.urlopen(req, timeout=20) as r:
                rel = json.load(r)
        except urllib.error.HTTPError as e:
            if e.code == 404:
                self._set(checked=True, available=False, message="Релизов пока нет")
            else:
                self._set(checked=True, error=f"GitHub ответил {e.code}")
            return
        except Exception as e:
            self._set(checked=True, error=f"GitHub недоступен: {e}")
            return
        tag = rel.get("tag_name", "")
        asset = next((a for a in rel.get("assets", []) if a.get("name", "").lower() == ASSET.lower()), None)
        is_newer = newer(tag, VERSION)
        with self._lock:
            self._asset_url = asset["browser_download_url"] if asset else ""
            self._size = asset.get("size", 0) if asset else 0
        msg = ""
        if is_newer and not asset:
            msg = f"В релизе {tag} нет файла {ASSET}"
        elif not is_newer:
            msg = "Установлена последняя версия"
        self._set(checked=True, available=bool(is_newer and asset), version=tag,
                  notes=rel.get("body", ""), url=rel.get("html_url", ""), message=msg)
        if is_newer and asset:
            log.info("Доступна новая версия %s", tag)

    def _download(self, url: str, dest: Path):
        req = urllib.request.Request(url, headers={"User-Agent": f"{APP_NAME}/{VERSION}"})
        got = 0
        with urllib.request.urlopen(req, timeout=60) as r, open(dest, "wb") as f:
            size = int(r.headers.get("Content-Length") or self._size or 0)
            while True:
                chunk = r.read(256 * 1024)
                if not chunk:
                    break
                f.write(chunk)
                got += len(chunk)
                if size:
                    self._set(progress=got / size)
        with open(dest, "rb") as f:
            head = f.read(2)
        if got < 1 << 20 or head != b"MZ":
            raise RuntimeError("скачанный файл не похож на программу")

    def install(self):
        with self._lock:
            if self._st["busy"]:
                return
            self._st.update(busy=True, error="", progress=0.0, message="Скачиваю обновление")
            url = self._asset_url
        exe = exe_path()
        try:
            if not FROZEN or exe is None:
                raise RuntimeError("обновление работает только в собранной программе")
            if not url:
                raise RuntimeError("в релизе нет " + ASSET)
            new, old = exe.with_name(exe.name + ".new"), exe.with_name(exe.name + ".old")
            self._download(url, new)
            self._set(message="Устанавливаю", progress=1.0)
            old.unlink(missing_ok=True)
            os.replace(exe, old)
            try:
                os.replace(new, exe)
            except OSError:
                os.replace(old, exe)
                raise
            # без сброса окружения новая копия возьмет временную папку старой (_MEI...),
            # а старая при выходе попытается ее удалить
            env = dict(os.environ, PYINSTALLER_RESET_ENVIRONMENT="1")
            subprocess.Popen([str(exe), "--after-update", str(os.getpid()), *self.restart_args()],
                             cwd=str(exe.parent), close_fds=True, env=env)
        except Exception as e:
            log.exception("Обновление не удалось")
            self._set(busy=False, error=str(e), message="")
            return
        log.info("Обновление установлено, перезапуск")
        self._set(message="Перезапускаюсь")
        time.sleep(0.5)
        self.on_quit()


def wait_for_exit(pid: int, timeout: float = 30):
    SYNCHRONIZE = 0x00100000
    k32 = ctypes.windll.kernel32
    h = k32.OpenProcess(SYNCHRONIZE, False, pid)
    if not h:
        return
    k32.WaitForSingleObject(h, int(timeout * 1000))
    k32.CloseHandle(h)


def cleanup_old():
    exe = exe_path()
    if exe is None:
        return
    for _ in range(40):
        try:
            exe.with_name(exe.name + ".old").unlink(missing_ok=True)
            break
        except OSError:
            time.sleep(0.25)
    try:
        exe.with_name(exe.name + ".new").unlink(missing_ok=True)
    except OSError:
        pass

