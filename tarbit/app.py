"""Запуск программы: одна копия, движок, интерфейс, трей."""

import argparse
import logging
import os
import sys
import threading
from logging.handlers import RotatingFileHandler
from pathlib import Path

from . import APP_NAME, VERSION, instance, window, winintegration
from .config import Config
from .engine import Engine
from .paths import FROZEN, data_dir, exe_path
from .server import Server
from .update import Updater, cleanup_old, wait_for_exit

log = logging.getLogger(APP_NAME)

ENGINE_KEYS = {"listen_port", "dl_limit", "ul_limit", "alt_dl_limit", "alt_ul_limit", "alt_enabled",
               "max_connections", "max_active_downloads", "max_active_seeds", "encryption", "upnp",
               "lsd", "dht", "pex", "identity"}


def setup_logging(d: Path):
    (d / "logs").mkdir(exist_ok=True)
    h = RotatingFileHandler(d / "logs" / "tarbit.log", maxBytes=1 << 20, backupCount=3, encoding="utf-8")
    h.setFormatter(logging.Formatter("%(asctime)s %(levelname)s %(name)s: %(message)s"))
    root = logging.getLogger()
    root.setLevel(logging.INFO)
    root.addHandler(h)
    if not FROZEN:
        root.addHandler(logging.StreamHandler())


class App:
    def __init__(self, data: Path):
        self.data = data
        self.cfg = Config(data / "settings.json")
        self.tray = None
        self._quit_once = threading.Lock()
        self._folder_lock = threading.Lock()
        self.engine = Engine(self.cfg, data, notify=self.notify)
        self.updater = Updater(self.quit, self._restart_args)
        self.server = Server(self)
        self.server.start()
        instance.write(data, self.server.port, self.server.token)
        exe = exe_path()
        if exe:
            try:
                winintegration.register(str(exe))
            except OSError as e:
                log.warning("Регистрация в Windows: %s", e)
        self.updater.start_background(lambda: self.cfg["check_updates"])
        log.info("%s %s запущен", APP_NAME, VERSION)

    # ---------- аргументы и окно ----------

    def handle_args(self, args: list[str]) -> bool:
        """Добавляет раздачи из аргументов. True - нужно показать окно (ждут подтверждения)."""
        show = False
        for a in args:
            try:
                if a.lower().startswith("magnet:"):
                    res = self.server.prepare(magnet=a)
                elif a.lower().endswith(".torrent") and os.path.isfile(a):
                    res = self.server.prepare(data=Path(a).read_bytes())
                else:
                    continue
                show = show or "pending" in res
                if "added" in res and self.tray:
                    added = res["added"]
                    verb = "Уже есть" if added.get("duplicate") else "Добавлено"
                    self.notify(verb, added.get("name", ""))
            except Exception as e:
                log.warning("Не удалось добавить %s: %s", a[:80], e)
                self.notify("Не удалось добавить", str(e))
        return show

    def show_window(self):
        window.open_window(self.server.url, self.data / "webview")

    def notify(self, title: str, text: str):
        if self.tray:
            self.tray.notify(title, text)

    def pick_folder(self, initial: str) -> str:
        """Системный диалог выбора папки (поверх всех окон)."""
        with self._folder_lock:
            import tkinter as tk
            from tkinter import filedialog
            root = tk.Tk()
            root.withdraw()
            root.attributes("-topmost", True)
            try:
                path = filedialog.askdirectory(parent=root, initialdir=initial, title="Папка для загрузок")
            finally:
                root.destroy()
            return os.path.normpath(path) if path else ""

    # ---------- настройки и Windows ----------

    def on_settings_changed(self, changed: dict):
        if changed.keys() & ENGINE_KEYS:
            self.engine.apply_settings()
        if changed.get("extra_trackers") or "custom_trackers" in changed:
            threading.Thread(target=self.engine.trackers.refresh, daemon=True).start()

    def set_autostart(self, on: bool):
        exe = exe_path()
        if exe:
            winintegration.set_autostart(str(exe), on)

    def integration_state(self) -> dict:
        st = {"frozen": FROZEN, "autostart": False, "magnet": False, "torrent": False}
        try:
            st["autostart"] = winintegration.autostart_enabled()
            st.update(winintegration.associations())
        except OSError:
            pass
        return st

    def associate(self) -> dict:
        exe = exe_path()
        if not exe:
            raise RuntimeError("доступно только в собранной программе")
        winintegration.claim_defaults(str(exe))
        if not all(winintegration.associations().values()):
            winintegration.open_default_apps()
        return self.integration_state()

    def _restart_args(self) -> list[str]:
        return [] if window.app_windows() else ["--minimized"]

    # ---------- жизненный цикл ----------

    def run(self):
        from .tray import Tray
        self.tray = Tray(self)
        self.tray.run()

    def quit(self):
        if not self._quit_once.acquire(blocking=False):
            return
        log.info("Выход")
        for w in window.app_windows():
            window.close(w)
        if self.tray:
            self.tray.stop()

    def shutdown(self):
        instance.remove(self.data)
        try:
            self.server.stop()
        except Exception:
            pass
        self.engine.shutdown()


def main(argv=None) -> int:
    p = argparse.ArgumentParser(prog=APP_NAME)
    p.add_argument("items", nargs="*", help="magnet-ссылки и файлы .torrent")
    p.add_argument("--minimized", action="store_true", help="запуститься в трее без окна")
    p.add_argument("--after-update", type=int, default=0, metavar="PID")
    args, _unknown = p.parse_known_args(argv)

    data = data_dir()
    if args.after_update:
        wait_for_exit(args.after_update)
        cleanup_old()
    first = instance.acquire()
    if not first and instance.forward(data, args.items, show=not args.items and not args.minimized):
        return 0

    setup_logging(data)
    try:
        app = App(data)
    except Exception:
        log.exception("Запуск не удался")
        raise
    need_window = app.handle_args(args.items)
    if need_window or not args.minimized:
        app.show_window()
    try:
        app.run()
    except Exception:
        log.exception("Сбой")
        raise
    finally:
        app.shutdown()
        instance.release()
    return 0


if __name__ == "__main__":
    sys.exit(main())
