"""Значок в трее: открыть окно, пауза/продолжение, «черепаха», выход."""

import threading
import time

import pystray
from PIL import Image

from . import APP_NAME
from .paths import resource_dir


def human_speed(v: float) -> str:
    for unit in ("Б/с", "КБ/с", "МБ/с", "ГБ/с"):
        if v < 1024 or unit == "ГБ/с":
            return f"{v:.0f} {unit}" if unit == "Б/с" else f"{v:.1f} {unit}"
        v /= 1024
    return ""


class Tray:
    def __init__(self, app):
        self.app = app
        img = Image.open(resource_dir() / "ui" / "icon-64.png")
        menu = pystray.Menu(
            pystray.MenuItem(f"Открыть {APP_NAME}", lambda: app.show_window(), default=True),
            pystray.Menu.SEPARATOR,
            pystray.MenuItem("Возобновить все", lambda: app.engine.resume_all()),
            pystray.MenuItem("Приостановить все", lambda: app.engine.pause_all()),
            pystray.MenuItem("Ограничить скорость", lambda: app.engine.set_alt_speed(not app.cfg["alt_enabled"]),
                             checked=lambda item: app.cfg["alt_enabled"]),
            pystray.Menu.SEPARATOR,
            pystray.MenuItem("Выход", lambda: app.quit()),
        )
        self.icon = pystray.Icon(APP_NAME, img, APP_NAME, menu)
        self._stop = threading.Event()

    def notify(self, title: str, text: str):
        try:
            self.icon.notify(text, title)
        except Exception:
            pass

    def _tooltip_loop(self):
        while not self._stop.wait(2):
            try:
                s = self.app.engine.session_info()
                self.icon.title = f"{APP_NAME}\n↓ {human_speed(s['dl'])}   ↑ {human_speed(s['ul'])}"[:127]
            except Exception:
                pass

    def run(self):
        """Блокирует поток до выхода из программы."""
        def setup(icon):
            icon.visible = True
            threading.Thread(target=self._tooltip_loop, daemon=True).start()
        self.icon.run(setup)

    def stop(self):
        self._stop.set()
        try:
            self.icon.stop()
        except Exception:
            pass
        time.sleep(0.1)
