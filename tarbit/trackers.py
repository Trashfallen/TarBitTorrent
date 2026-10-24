"""Публичные трекеры, которые подмешиваются в открытые раздачи.

Чем больше живых трекеров у раздачи, тем быстрее находятся сиды и пиры, особенно
для старых magnet-ссылок, где трекеров нет совсем. Список берется из
поддерживаемых сообществом источников и обновляется раз в 12 часов; без сети
работает встроенный список. В приватные раздачи (флаг private) ничего не
добавляется: там чужие трекеры запрещены.
"""

import json
import logging
import threading
import time
import urllib.request
from pathlib import Path

from . import APP_NAME, VERSION
from .paths import write_atomic

log = logging.getLogger(__name__)

SOURCES = [
    "https://raw.githubusercontent.com/ngosang/trackerslist/master/trackers_best.txt",
    "https://cdn.jsdelivr.net/gh/ngosang/trackerslist@master/trackers_best.txt",
    "https://newtrackon.com/api/stable",
]

BUILTIN = [
    "udp://tracker.opentrackr.org:1337/announce",
    "udp://open.demonii.com:1337/announce",
    "udp://open.stealth.si:80/announce",
    "udp://tracker.torrent.eu.org:451/announce",
    "udp://exodus.desync.com:6969/announce",
    "udp://explodie.org:6969/announce",
    "udp://tracker.theoks.net:6969/announce",
    "udp://tracker.qu.ax:6969/announce",
    "udp://tracker.dump.cl:6969/announce",
    "udp://tracker.0x7c0.com:6969/announce",
    "udp://tracker-udp.gbitt.info:80/announce",
    "udp://tracker.tiny-vps.com:6969/announce",
    "udp://opentracker.io:6969/announce",
    "udp://p4p.arenabg.com:1337/announce",
    "udp://tracker.moeking.me:6969/announce",
    "udp://tracker.cyberia.is:6969/announce",
    "udp://retracker01-msk-virt.corbina.net:80/announce",
    "http://tracker.opentrackr.org:1337/announce",
    "https://tracker.gbitt.info:443/announce",
    "http://open.acgnxtracker.com:80/announce",
    # локальные ретрекеры провайдеров: соседи по сети провайдера, самая быстрая отдача
    "http://retracker.local/announce",
]

REFRESH_EVERY = 12 * 3600
MAX_TRACKERS = 60


def parse_list(text: str) -> list[str]:
    out = []
    for line in text.splitlines():
        url = line.strip()
        if url and not url.startswith("#") and url.split("://", 1)[0] in ("udp", "http", "https"):
            out.append(url)
    return out


def merge(*lists) -> list[str]:
    seen, out = set(), []
    for lst in lists:
        for url in lst:
            key = url.rstrip("/").lower()
            if key not in seen:
                seen.add(key)
                out.append(url)
    return out


class TrackerPool:
    def __init__(self, cache: Path):
        self.cache = cache
        self._lock = threading.Lock()
        self._fetched: list[str] = []
        self.updated = 0.0
        try:
            d = json.loads(cache.read_text(encoding="utf-8"))
            self._fetched = parse_list("\n".join(d.get("trackers", [])))
            self.updated = float(d.get("updated", 0))
        except (OSError, ValueError):
            pass

    def get(self, custom: str = "") -> list[str]:
        with self._lock:
            fetched = list(self._fetched)
        return merge(parse_list(custom), fetched, BUILTIN)[:MAX_TRACKERS + len(parse_list(custom))]

    def stale(self) -> bool:
        return time.time() - self.updated > REFRESH_EVERY

    def refresh(self) -> bool:
        """Скачивает свежие списки. True - что-то получили."""
        got = []
        for url in SOURCES:
            try:
                req = urllib.request.Request(url, headers={"User-Agent": f"{APP_NAME}/{VERSION}"})
                with urllib.request.urlopen(req, timeout=15) as r:
                    got.append(parse_list(r.read(512 * 1024).decode("utf-8", "replace")))
            except Exception as e:  # сеть, блокировки, кривой ответ - пробуем следующий
                log.info("Список трекеров %s недоступен: %s", url, e)
        fresh = merge(*got)
        if not fresh:
            return False
        with self._lock:
            self._fetched = fresh
            self.updated = time.time()
        data = json.dumps({"updated": self.updated, "trackers": fresh}, indent=1).encode()
        write_atomic(self.cache, data)
        log.info("Список трекеров обновлен: %d шт.", len(fresh))
        return True
