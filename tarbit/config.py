"""Настройки программы: settings.json в папке данных."""

import json
import random
import threading
from pathlib import Path

from .paths import default_downloads, write_atomic

DEFAULTS = {
    "save_path": "",              # пусто - папка «Загрузки»
    "listen_port": 0,             # 0 - выбрать случайный при первом запуске
    "dl_limit": 0,                # КиБ/с, 0 - без ограничений
    "ul_limit": 0,
    "alt_dl_limit": 1024,         # «черепаха»
    "alt_ul_limit": 256,
    "alt_enabled": False,
    "max_connections": 800,
    "max_active_downloads": 8,
    "max_active_seeds": 15,
    "extra_trackers": True,       # подмешивать публичные трекеры в открытые раздачи
    "custom_trackers": "",        # свои трекеры, по одному на строку
    "peer_hunter": True,          # переобъявляться, пока мало пиров
    "encryption": "prefer",       # prefer | require | off
    "upnp": True,
    "lsd": True,
    "dht": True,
    "pex": True,
    "identity": "native",         # native | qbittorrent
    "ask_on_add": True,
    "start_paused": False,
    "sequential_default": False,
    "seed_ratio_limit": 0.0,      # 0 - раздавать без ограничений
    "notify_finished": True,
    "theme": "dark",              # dark | light | system
    "check_updates": True,
}

CHOICES = {
    "encryption": ("prefer", "require", "off"),
    "identity": ("native", "qbittorrent"),
    "theme": ("dark", "light", "system"),
}


class Config:
    def __init__(self, path: Path):
        self.path = path
        self._lock = threading.Lock()
        self._data = dict(DEFAULTS)
        if path.exists():
            try:
                saved = json.loads(path.read_text(encoding="utf-8"))
                for k, v in saved.items():
                    if k in DEFAULTS:
                        self._data[k] = v
            except (OSError, ValueError):
                pass
        if not self._data["listen_port"]:
            self._data["listen_port"] = random.randint(20000, 60000)
        self.save()

    def __getitem__(self, key):
        with self._lock:
            return self._data[key]

    def snapshot(self) -> dict:
        with self._lock:
            d = dict(self._data)
        d["save_path_effective"] = self.save_path()
        return d

    def save_path(self) -> str:
        with self._lock:
            p = self._data["save_path"]
        return p or str(default_downloads())

    def update(self, changes: dict) -> dict:
        """Применяет только известные ключи с приведением типов, возвращает то, что поменялось."""
        changed = {}
        with self._lock:
            for k, v in changes.items():
                if k not in DEFAULTS:
                    continue
                v = _coerce(k, v, DEFAULTS[k])
                if v is None or self._data[k] == v:
                    continue
                self._data[k] = v
                changed[k] = v
        if changed:
            self.save()
        return changed

    def save(self):
        with self._lock:
            data = json.dumps(self._data, ensure_ascii=False, indent=2).encode("utf-8")
        write_atomic(self.path, data)


def _coerce(key, value, default):
    try:
        if isinstance(default, bool):
            return bool(value)
        if isinstance(default, int):
            v = int(value)
            if key == "listen_port" and not 1024 <= v <= 65535:
                return None
            return max(v, 0)
        if isinstance(default, float):
            return max(float(value), 0.0)
        v = str(value)
        if key in CHOICES and v not in CHOICES[key]:
            return None
        return v
    except (TypeError, ValueError):
        return None
