"""Где лежат данные программы и ее собственные файлы (интерфейс, значок)."""

import os
import sys
from pathlib import Path

from . import APP_NAME

FROZEN = bool(getattr(sys, "frozen", False))


def resource_dir() -> Path:
    """Папка пакета tarbit: в собранном exe она распаковывается во временный каталог."""
    base = getattr(sys, "_MEIPASS", None)
    if base:
        return Path(base) / "tarbit"
    return Path(__file__).resolve().parent


def exe_path() -> Path | None:
    return Path(sys.executable).resolve() if FROZEN else None


def data_dir() -> Path:
    """%APPDATA%\\TarBitTorrent, для отладки можно переопределить TARBIT_HOME."""
    env = os.environ.get("TARBIT_HOME")
    if env:
        d = Path(env)
    else:
        d = Path(os.environ.get("APPDATA") or Path.home()) / APP_NAME
    d.mkdir(parents=True, exist_ok=True)
    return d


def default_downloads() -> Path:
    d = Path.home() / "Downloads"
    return d if d.is_dir() else Path.home()


def write_atomic(path: Path, data: bytes) -> None:
    """Пишет через временный файл, чтобы при сбое не остался обрезанный файл."""
    tmp = path.with_name(path.name + ".tmp")
    with open(tmp, "wb") as f:
        f.write(data)
        f.flush()
        os.fsync(f.fileno())
    os.replace(tmp, path)
