"""Торрент-движок: сессия libtorrent, раздачи, сохранение состояния и поиск пиров.

Как программа ищет сиды и пиры:
- все каналы сразу: трекеры (все и во всех уровнях), DHT, PEX, LSD (соседи по
  локальной сети), входящие соединения через UPnP/NAT-PMP, TCP и uTP;
- в открытые раздачи подмешиваются живые публичные трекеры и ретрекер провайдера;
- DHT стартует с шести узлов-маршрутизаторов, а таблица узлов сохраняется между
  запусками, поэтому после перезапуска поиск идет сразу, без «прогрева»;
- список известных пиров каждой раздачи тоже сохраняется (fastresume);
- «охотник»: пока у раздачи мало пиров или нет данных, она переобъявляется на всех
  трекерах и в DHT с нарастающим интервалом 15 с -> 5 мин, без ожидания интервала
  трекера; если DHT опустел, узлы-маршрутизаторы добавляются заново.
"""

import ctypes
import logging
import os
import subprocess
import threading
import time
from collections import deque
from pathlib import Path

import libtorrent as lt

from . import APP_NAME, VERSION
from .paths import write_atomic
from .trackers import TrackerPool

log = logging.getLogger(__name__)

DHT_ROUTERS = [
    ("dht.libtorrent.org", 25401),
    ("router.bittorrent.com", 6881),
    ("router.utorrent.com", 6881),
    ("dht.transmissionbt.com", 6881),
    ("dht.aelitis.com", 6881),
    ("router.bitcomet.com", 6881),
]

# источник пира (peer_info.source)
SRC_TRACKER, SRC_DHT, SRC_PEX, SRC_LSD, SRC_RESUME, SRC_INCOMING = 1, 2, 4, 8, 16, 32

# приоритеты файлов
PRIO_SKIP, PRIO_LOW, PRIO_NORMAL, PRIO_HIGH = 0, 1, 4, 7

HUNT_STEPS = [15, 30, 60, 120, 300]   # интервалы переобъявления, секунды
HUNT_MIN_PEERS = 8                    # меньше - ищем активнее
HUNT_NO_DATA = 60                     # столько секунд без данных - тоже ищем

ALERTS = (lt.alert.category_t.error_notification
          | lt.alert.category_t.status_notification
          | lt.alert.category_t.storage_notification
          | lt.alert.category_t.port_mapping_notification
          | lt.alert.category_t.stats_notification)

S = lt.torrent_status.states
CHECKING_STATES = {S.checking_files, S.checking_resume_data}


def tid(ih) -> str:
    """Идентификатор раздачи: v1-хеш, для чистых v2-раздач - v2."""
    return str(ih.v1) if ih.has_v1() else str(ih.v2)


def error_text(err) -> str:
    """Текст ошибки libtorrent по-русски, насколько это возможно."""
    if isinstance(err, dict):
        value, category = err.get("value", 0), err.get("category", "")
    else:
        value, category = err.value(), err.category().name()
    if not value:
        return ""
    if category == "system":
        try:
            return ctypes.FormatError(value).strip()
        except Exception:
            return f"системная ошибка {value}"
    if category == "http":
        return f"HTTP {value}"
    if category == "libtorrent":
        msg = lt.libtorrent_category().message(value)
        return KNOWN_LT_ERRORS.get(value, msg)
    if category in ("asio.netdb", "netdb"):
        return "сервер не найден"
    if category == "asio.misc" and value == 2:
        return "соединение закрыто"
    return f"{category}: {value}"


KNOWN_LT_ERRORS = {
    36: "нет ответа (таймаут)",
    180: "недоступен с этого адреса",
    166: "трекер ответил ошибкой",
}


def _secs(v) -> int:
    return int(v.total_seconds()) if hasattr(v, "total_seconds") else int(v or 0)


class Engine:
    def __init__(self, cfg, data_dir: Path, notify=None):
        self.cfg = cfg
        self.dir = data_dir
        self.resume_dir = data_dir / "resume"
        self.resume_dir.mkdir(exist_ok=True)
        self.state_file = data_dir / "session.state"
        self.notify = notify or (lambda title, text: None)
        self.trackers = TrackerPool(data_dir / "trackers.json")

        self._lock = threading.RLock()
        self.handles: dict[str, lt.torrent_handle] = {}
        self._hunt: dict[str, dict] = {}
        self._was_downloading: set[str] = set()
        self._pending_resume = 0
        self._resume_cv = threading.Condition()
        self._stop = threading.Event()
        self._last_bootstrap = time.time()

        self.stats = {"dht_nodes": 0, "peers": 0, "ext_ip": "", "upnp": "", "listen": "",
                      "listen_error": "", "incoming": False}
        self.history: deque = deque(maxlen=300)          # (dl, ul) раз в секунду
        self.torrent_history: dict[str, deque] = {}

        params = lt.session_params()
        if self.state_file.exists():
            try:
                params = lt.read_session_params(self.state_file.read_bytes())
            except Exception as e:
                log.warning("Состояние сессии не прочитано: %s", e)
        params.settings = self._settings()
        self.ses = lt.session(params)
        self._bootstrap_dht()
        self._load_resume()

        self._threads = [
            threading.Thread(target=self._alert_loop, name="alerts", daemon=True),
            threading.Thread(target=self._tick_loop, name="tick", daemon=True),
            threading.Thread(target=self._trackers_loop, name="trackers", daemon=True),
        ]
        for t in self._threads:
            t.start()

    # ---------- настройки ----------

    def _settings(self) -> dict:
        c = self.cfg
        port = c["listen_port"]
        if c["identity"] == "qbittorrent":
            ua, fp = "qBittorrent/5.0.5", "-qB5050-"
        else:
            ua = f"{APP_NAME}/{VERSION} libtorrent/{lt.__version__}"
            fp = lt.generate_fingerprint("LT", *[int(x) for x in lt.__version__.split(".")[:4]])
        enc = {"prefer": 1, "require": 0, "off": 2}[c["encryption"]]
        alt = c["alt_enabled"]
        dl = (c["alt_dl_limit"] if alt else c["dl_limit"]) * 1024
        ul = (c["alt_ul_limit"] if alt else c["ul_limit"]) * 1024
        return {
            "user_agent": ua,
            "peer_fingerprint": fp,
            "alert_mask": ALERTS,
            "listen_interfaces": f"0.0.0.0:{port},[::]:{port}",
            "listen_system_port_fallback": True,
            "enable_dht": c["dht"],
            "enable_lsd": c["lsd"],
            "enable_upnp": c["upnp"],
            "enable_natpmp": c["upnp"],
            "dht_bootstrap_nodes": ",".join(f"{h}:{p}" for h, p in DHT_ROUTERS),
            "dht_extended_routing_table": True,
            "dht_aggressive_lookups": True,
            "enable_outgoing_utp": True,
            "enable_incoming_utp": True,
            "enable_outgoing_tcp": True,
            "enable_incoming_tcp": True,
            "announce_to_all_trackers": True,
            "announce_to_all_tiers": True,
            "prefer_udp_trackers": True,
            "max_concurrent_http_announces": 100,
            "tracker_completion_timeout": 30,
            "tracker_receive_timeout": 15,
            "stop_tracker_timeout": 3,
            "auto_scrape_interval": 600,
            "auto_scrape_min_interval": 120,
            "connections_limit": c["max_connections"],
            "connection_speed": 60,
            "torrent_connect_boost": 40,
            "max_peerlist_size": 8000,
            "max_paused_peerlist_size": 2000,
            "peer_connect_timeout": 10,
            "seeding_outgoing_connections": True,
            "active_downloads": c["max_active_downloads"],
            "active_seeds": c["max_active_seeds"],
            "active_limit": max(c["max_active_downloads"] + c["max_active_seeds"], 20),
            "active_tracker_limit": 1600,
            "active_dht_limit": 400,
            "active_lsd_limit": 200,
            "dont_count_slow_torrents": True,
            "in_enc_policy": enc,
            "out_enc_policy": enc,
            "allowed_enc_level": 3,   # и обфускация, и RC4
            "prefer_rc4": c["encryption"] == "require",
            "download_rate_limit": dl,
            "upload_rate_limit": ul,
            "send_buffer_watermark": 2 * 1024 * 1024,
        }

    def _bootstrap_dht(self):
        for host, port in DHT_ROUTERS:
            try:
                self.ses.add_dht_node((host, port))
            except Exception:
                pass

    def apply_settings(self):
        self.ses.apply_settings(self._settings())
        pex = self.cfg["pex"]
        with self._lock:
            for h in self.handles.values():
                if pex:
                    h.unset_flags(lt.torrent_flags.disable_pex)
                else:
                    h.set_flags(lt.torrent_flags.disable_pex)

    # ---------- раздачи ----------

    def _extra_trackers(self) -> list[str]:
        if not self.cfg["extra_trackers"]:
            return []
        return self.trackers.get(self.cfg["custom_trackers"])

    def _add_extra_trackers(self, atp):
        if atp.ti is not None and atp.ti.priv():
            return
        have = {u.rstrip("/").lower() for u in atp.trackers}
        tiers = list(atp.tracker_tiers) + [0] * (len(atp.trackers) - len(atp.tracker_tiers))
        top = (max(tiers) + 1) if tiers else 0
        urls, out_tiers = list(atp.trackers), tiers
        for url in self._extra_trackers():
            if url.rstrip("/").lower() not in have:
                urls.append(url)
                out_tiers.append(top)
        atp.trackers = urls
        atp.tracker_tiers = out_tiers

    @staticmethod
    def parse_source(magnet: str = "", data: bytes = b""):
        """magnet или содержимое .torrent -> add_torrent_params (без добавления)."""
        if magnet:
            return lt.parse_magnet_uri(magnet.strip())
        return lt.load_torrent_buffer(data)

    @staticmethod
    def describe(atp) -> dict:
        """Что внутри раздачи: для окна добавления."""
        d = {"name": atp.name or "", "size": 0, "files": [], "private": False, "has_metadata": False,
             "id": tid(atp.info_hashes) if atp.ti is None else tid(atp.ti.info_hashes())}
        ti = atp.ti
        if ti is not None:
            fs = ti.files()
            d.update(name=ti.name(), size=ti.total_size(), private=ti.priv(), has_metadata=True,
                     comment=ti.comment())
            for i in range(fs.num_files()):
                if fs.file_flags(i) & lt.file_storage.flag_pad_file:
                    continue
                d["files"].append({"index": i, "path": fs.file_path(i), "size": fs.file_size(i)})
            d["size"] = sum(f["size"] for f in d["files"])   # без выравнивающих pad-файлов
        return d

    def add(self, magnet: str = "", data: bytes = b"", save_path: str = "", paused=None,
            sequential=None, priorities: dict | None = None) -> dict:
        atp = self.parse_source(magnet, data)
        atp.save_path = save_path or self.cfg.save_path()
        Path(atp.save_path).mkdir(parents=True, exist_ok=True)
        self._add_extra_trackers(atp)
        paused = self.cfg["start_paused"] if paused is None else paused
        sequential = self.cfg["sequential_default"] if sequential is None else sequential
        flags = lt.torrent_flags.default_flags | lt.torrent_flags.update_subscribe
        if paused:
            flags = (flags | lt.torrent_flags.paused) & ~lt.torrent_flags.auto_managed
        if sequential:
            flags |= lt.torrent_flags.sequential_download
        if not self.cfg["pex"]:
            flags |= lt.torrent_flags.disable_pex
        atp.flags = flags
        if priorities and atp.ti is not None:
            n = atp.ti.files().num_files()
            atp.file_priorities = [int(priorities.get(str(i), priorities.get(i, PRIO_NORMAL))) for i in range(n)]
        with self._lock:
            existing = self._find(atp)
            if existing:
                h = self.handles[existing]
                for url in atp.trackers:
                    h.add_tracker({"url": url, "tier": 0})
                return {"id": existing, "duplicate": True, "name": h.status().name}
            h = self.ses.add_torrent(atp)
            key = tid(h.info_hashes())
            self.handles[key] = h
        self._save_resume(h)
        log.info("Добавлена раздача %s", key)
        return {"id": key, "duplicate": False, "name": h.status().name}

    def _find(self, atp) -> str | None:
        ih = atp.ti.info_hashes() if atp.ti is not None else atp.info_hashes
        key = tid(ih)
        return key if key in self.handles else None

    def get(self, key: str) -> lt.torrent_handle:
        with self._lock:
            h = self.handles.get(key)
        if h is None or not h.is_valid():
            raise KeyError(key)
        return h

    def remove(self, key: str, with_data=False):
        with self._lock:
            h = self.handles.pop(key, None)
            self._hunt.pop(key, None)
            self.torrent_history.pop(key, None)
        if h is None:
            return
        self.ses.remove_torrent(h, lt.session.delete_files if with_data else 0)
        (self.resume_dir / f"{key}.fastresume").unlink(missing_ok=True)

    def action(self, key: str, act: str, **kw):
        h = self.get(key)
        F = lt.torrent_flags
        if act == "pause":
            h.unset_flags(F.auto_managed)
            h.pause()
        elif act == "resume":
            h.set_flags(F.auto_managed)
            h.resume()
        elif act == "force":                 # запустить вне очереди
            h.unset_flags(F.auto_managed)
            h.resume()
        elif act == "recheck":
            h.force_recheck()
        elif act == "reannounce":
            self._announce(h)
            self._hunt.pop(key, None)
        elif act == "sequential":
            on = bool(kw.get("value", not (h.flags() & F.sequential_download)))
            (h.set_flags if on else h.unset_flags)(F.sequential_download)
        elif act in ("queue_top", "queue_up", "queue_down", "queue_bottom"):
            getattr(h, "queue_position_" + act.split("_", 1)[1])()
        elif act == "move":
            path = kw.get("path") or ""
            if path:
                h.move_storage(path)
        elif act == "remove":
            self.remove(key, with_data=bool(kw.get("with_data")))
            return
        else:
            raise ValueError(act)
        self._save_resume(h)

    def _announce(self, h):
        h.force_reannounce(0, -1, lt.torrent_handle.ignore_min_interval)
        if self.cfg["dht"]:
            h.force_dht_announce()
        try:
            h.scrape_tracker()
        except Exception:
            pass

    def set_file_priorities(self, key: str, prios: dict):
        h = self.get(key)
        cur = list(h.get_file_priorities())
        for i, p in prios.items():
            i = int(i)
            if 0 <= i < len(cur):
                cur[i] = max(0, min(7, int(p)))
        h.prioritize_files(cur)

    def add_trackers(self, key: str, urls: list[str]):
        h = self.get(key)
        for url in urls:
            url = url.strip()
            if url:
                h.add_tracker({"url": url, "tier": 0})
        self._announce(h)

    def pause_all(self):
        with self._lock:
            keys = list(self.handles)
        for k in keys:
            try:
                self.action(k, "pause")
            except KeyError:
                pass

    def resume_all(self):
        with self._lock:
            keys = list(self.handles)
        for k in keys:
            try:
                self.action(k, "resume")
            except KeyError:
                pass

    def set_alt_speed(self, on: bool):
        self.cfg.update({"alt_enabled": on})
        self.apply_settings()

    # ---------- состояние для интерфейса ----------

    @staticmethod
    def _ui_status(st, hunting: bool) -> str:
        paused = bool(st.flags & lt.torrent_flags.paused)
        if st.errc.value():
            return "error"
        if st.state in CHECKING_STATES:
            return "checking"
        if st.moving_storage:
            return "moving"
        if paused:
            if st.auto_managed:
                return "queued_seed" if st.is_finished else "queued"
            return "completed" if st.is_finished else "paused"
        if st.state == S.downloading_metadata:
            return "metadata"
        if st.is_finished:
            return "seeding"
        if st.num_peers == 0 or (st.download_payload_rate == 0 and hunting):
            return "searching"
        if st.download_payload_rate == 0:
            return "stalled"
        return "downloading"

    def summary(self, h) -> dict:
        st = h.status()
        key = tid(st.info_hashes)
        hunting = key in self._hunt and self._hunt[key].get("active", False)
        wanted, done = st.total_wanted, st.total_wanted_done
        dl = st.download_payload_rate
        left = max(wanted - done, 0)
        downloaded = max(st.all_time_download, st.total_done)
        return {
            "id": key,
            "name": st.name or key,
            "size": wanted,
            "done": done,
            "progress": st.progress,
            "status": self._ui_status(st, hunting),
            "dl": dl,
            "ul": st.upload_payload_rate,
            "seeds": st.num_seeds,
            "peers": st.num_peers - st.num_seeds,
            "seeds_total": max(st.num_complete, st.list_seeds),
            "peers_total": max(st.num_incomplete, st.list_peers - st.list_seeds, 0),
            "candidates": st.connect_candidates,
            "eta": int(left / dl) if dl > 0 and left else (0 if not left else -1),
            "ratio": (st.all_time_upload / downloaded) if downloaded else 0.0,
            "uploaded": st.all_time_upload,
            "downloaded": st.all_time_download,
            "added": st.added_time,
            "completed": st.completed_time,
            "save_path": st.save_path,
            "queue": st.queue_position if st.queue_position >= 0 else None,
            "sequential": bool(st.flags & lt.torrent_flags.sequential_download),
            "error": error_text(st.errc) if st.errc.value() else "",
            "has_metadata": st.has_metadata,
            "availability": st.distributed_copies,
            "hunting": hunting,
            "auto_managed": st.auto_managed,
        }

    def list_torrents(self) -> list[dict]:
        with self._lock:
            hs = list(self.handles.values())
        out = []
        for h in hs:
            if h.is_valid():
                out.append(self.summary(h))
        return out

    def details(self, key: str) -> dict:
        h = self.get(key)
        d = self.summary(h)
        st = h.status()
        ti = h.torrent_file()
        d.update({
            "magnet": lt.make_magnet_uri(h),
            "hash_v1": str(st.info_hashes.v1) if st.info_hashes.has_v1() else "",
            "hash_v2": str(st.info_hashes.v2) if st.info_hashes.has_v2() else "",
            "pieces": st.num_pieces,
            "piece_count": ti.num_pieces() if ti else 0,
            "piece_size": ti.piece_length() if ti else 0,
            "comment": ti.comment() if ti else "",
            "creator": ti.creator() if ti else "",
            "created": ti.creation_date() if ti else 0,
            "private": ti.priv() if ti else False,
            "connections": st.num_connections,
            "active_time": _secs(st.active_duration),
            "seeding_time": _secs(st.seeding_duration),
            "last_seen_complete": st.last_seen_complete,
            "wasted": st.total_failed_bytes + st.total_redundant_bytes,
            "hunt": dict(self._hunt.get(key, {})),
            "history": list(self.torrent_history.get(key, ())),
            "piece_map": self._piece_map(st, 300),
        })
        return d

    @staticmethod
    def _piece_map(st, cells: int) -> list[int]:
        """Полоса кусков: доля скачанного в каждой из cells ячеек, 0..9."""
        pieces = st.pieces
        n = len(pieces)
        if not n:
            return []
        cells = min(cells, n)
        out = []
        for c in range(cells):
            a, b = c * n // cells, (c + 1) * n // cells
            have = sum(1 for i in range(a, max(b, a + 1)) if pieces[i])
            out.append(round(9 * have / max(b - a, 1)))
        return out

    def files(self, key: str) -> list[dict]:
        h = self.get(key)
        ti = h.torrent_file()
        if ti is None:
            return []
        fs = ti.files()
        prog = h.file_progress(lt.torrent_handle.piece_granularity)
        prios = h.get_file_priorities()
        out = []
        for i in range(fs.num_files()):
            if fs.file_flags(i) & lt.file_storage.flag_pad_file:
                continue
            size = fs.file_size(i)
            out.append({"index": i, "path": fs.file_path(i), "size": size,
                        "done": prog[i] if i < len(prog) else 0,
                        "progress": (prog[i] / size) if size and i < len(prog) else 1.0,
                        "priority": prios[i] if i < len(prios) else PRIO_NORMAL})
        return out

    def peers(self, key: str) -> list[dict]:
        h = self.get(key)
        out = []
        for p in h.get_peer_info():
            src = p.source
            client = p.client
            if isinstance(client, bytes):
                client = client.decode("utf-8", "replace")
            ip = p.ip[0]
            if ip.startswith("::ffff:") and "." in ip:
                ip = ip[7:]
            out.append({
                "ip": ip, "port": p.ip[1],
                "client": client or "",
                "progress": p.progress,
                "dl": p.payload_down_speed, "ul": p.payload_up_speed,
                "downloaded": p.total_download, "uploaded": p.total_upload,
                "seed": bool(p.flags & lt.peer_info.seed),
                "encrypted": bool(p.flags & (lt.peer_info.rc4_encrypted | lt.peer_info.plaintext_encrypted)),
                "incoming": bool(src & SRC_INCOMING),
                "sources": [n for bit, n in ((SRC_TRACKER, "трекер"), (SRC_DHT, "DHT"), (SRC_PEX, "PEX"),
                                             (SRC_LSD, "LSD"), (SRC_RESUME, "сохраненный"),
                                             (SRC_INCOMING, "входящий")) if src & bit],
            })
        out.sort(key=lambda x: (-(x["dl"] + x["ul"]), -x["progress"]))
        return out

    def tracker_list(self, key: str) -> list[dict]:
        h = self.get(key)
        rows = []
        for t in h.trackers():
            seeds = peers = downloaded = -1
            nxt, msg, errors = 0, "", []
            working = bool(t.get("verified"))
            updating = False
            for ep in t.get("endpoints", []):
                for e in ep.get("info_hashes") or [ep]:
                    seeds = max(seeds, e.get("scrape_complete", -1))
                    peers = max(peers, e.get("scrape_incomplete", -1))
                    downloaded = max(downloaded, e.get("scrape_downloaded", -1))
                    updating = updating or bool(e.get("updating"))
                    err = e.get("last_error") or {}
                    # 180 - «недоступен с этого адреса»: обычное дело для loopback и IPv6
                    if err.get("value") and not (err.get("value") == 180 and err.get("category") == "libtorrent"):
                        errors.append(error_text(err))
                    elif e.get("start_sent") and not e.get("fails"):
                        working = True
                    msg = e.get("message") or msg
                    na = e.get("next_announce", 0)
                    if na > 0:
                        nxt = na if not nxt else min(nxt, na)
            status = "working" if working else "updating" if updating else "error" if errors else "idle"
            if status == "error":
                msg = msg or errors[0]
            rows.append({"url": t["url"], "tier": t.get("tier", 0), "status": status, "message": msg,
                         "seeds": seeds, "peers": peers, "downloaded": downloaded,
                         "next": max(int(nxt - time.time()), 0) if nxt else 0,
                         "fails": t.get("fails", 0)})
        # Псевдотрекеры: сколько пиров пришло из каждого механизма прямо сейчас
        counts = {"DHT": 0, "PEX": 0, "LSD": 0}
        for p in h.get_peer_info():
            if p.source & SRC_DHT:
                counts["DHT"] += 1
            if p.source & SRC_PEX:
                counts["PEX"] += 1
            if p.source & SRC_LSD:
                counts["LSD"] += 1
        enabled = {"DHT": self.cfg["dht"], "PEX": self.cfg["pex"], "LSD": self.cfg["lsd"]}
        ti = h.torrent_file()
        private = bool(ti and ti.priv())
        pseudo = [{"url": f"** [{k}] **", "pseudo": True, "connected": v,
                   "status": "disabled" if (private or not enabled[k]) else "working"}
                  for k, v in counts.items()]
        return pseudo + rows

    def session_info(self) -> dict:
        with self._lock:
            hs = list(self.handles.values())
        dl = ul = 0
        incoming = False
        for h in hs:
            if not h.is_valid():
                continue
            st = h.status()
            dl += st.download_payload_rate
            ul += st.upload_payload_rate
            incoming = incoming or st.has_incoming
        info = dict(self.stats)
        info.update(dl=dl, ul=ul, incoming=incoming or self.stats["incoming"],
                    port=self.ses.listen_port(), alt=self.cfg["alt_enabled"],
                    trackers_pool=len(self._extra_trackers()),
                    history=list(self.history)[-120:])
        return info

    # ---------- фоновые циклы ----------

    def _alert_loop(self):
        while not self._stop.is_set():
            if not self.ses.wait_for_alert(500):
                continue
            for a in self.ses.pop_alerts():
                try:
                    self._on_alert(a)
                except Exception:
                    log.exception("Ошибка обработки события %s", type(a).__name__)

    def _on_alert(self, a):
        if isinstance(a, lt.save_resume_data_alert):
            try:
                key = tid(a.handle.info_hashes())
                with self._lock:
                    still_here = key in self.handles
                if still_here:
                    write_atomic(self.resume_dir / f"{key}.fastresume", lt.write_resume_data_buf(a.params))
            finally:
                self._resume_done()
        elif isinstance(a, lt.save_resume_data_failed_alert):
            self._resume_done()
        elif isinstance(a, lt.session_stats_alert):
            v = a.values
            self.stats["dht_nodes"] = int(v.get("dht.dht_nodes", 0))
            self.stats["peers"] = int(v.get("peer.num_peers_connected", 0))
        elif isinstance(a, lt.metadata_received_alert):
            self._save_resume(a.handle)
        elif isinstance(a, lt.torrent_finished_alert):
            key = tid(a.handle.info_hashes())
            self._save_resume(a.handle)
            if key in self._was_downloading:
                self._was_downloading.discard(key)
                if self.cfg["notify_finished"]:
                    self.notify("Загрузка завершена", a.handle.status().name)
        elif isinstance(a, lt.external_ip_alert):
            self.stats["ext_ip"] = str(a.external_address)
        elif isinstance(a, lt.portmap_alert):
            self.stats["upnp"] = f"проброшен порт {a.external_port}"
        elif isinstance(a, lt.portmap_error_alert):
            if not self.stats["upnp"]:
                self.stats["upnp"] = "роутер не дал пробросить порт"
        elif isinstance(a, lt.listen_succeeded_alert):
            self.stats["listen"] = "ok"
        elif isinstance(a, lt.listen_failed_alert):
            self.stats["listen_error"] = a.message()
            log.warning("Порт: %s", a.message())
        elif isinstance(a, (lt.torrent_error_alert, lt.file_error_alert)):
            log.warning("%s", a.message())

    def _resume_done(self):
        with self._resume_cv:
            self._pending_resume = max(self._pending_resume - 1, 0)
            self._resume_cv.notify_all()

    def _save_resume(self, h, only_if_modified=False):
        flags = lt.torrent_handle.save_info_dict
        if only_if_modified:
            if not h.need_save_resume_data():
                return
            flags |= lt.torrent_handle.only_if_modified
        with self._resume_cv:
            self._pending_resume += 1
        h.save_resume_data(flags)

    def _tick_loop(self):
        last_resume = time.time()
        while not self._stop.wait(1.0):
            try:
                self.ses.post_session_stats()
                self._tick()
                if time.time() - last_resume > 180:
                    last_resume = time.time()
                    self._save_all(only_if_modified=True)
                    self._save_state()
            except Exception:
                log.exception("Сбой фонового цикла")

    def _tick(self):
        now = time.time()
        with self._lock:
            items = list(self.handles.items())
        total_dl = total_ul = 0
        ratio_limit = self.cfg["seed_ratio_limit"]
        hunter = self.cfg["peer_hunter"]
        for key, h in items:
            if not h.is_valid():
                continue
            st = h.status()
            total_dl += st.download_payload_rate
            total_ul += st.upload_payload_rate
            hist = self.torrent_history.setdefault(key, deque(maxlen=120))
            hist.append((st.download_payload_rate, st.upload_payload_rate))
            paused = bool(st.flags & lt.torrent_flags.paused)
            if not st.is_finished and not paused:
                self._was_downloading.add(key)
            if ratio_limit and st.is_finished and not paused:
                downloaded = max(st.all_time_download, st.total_done)
                if downloaded and st.all_time_upload / downloaded >= ratio_limit:
                    self.action(key, "pause")
                    continue
            if hunter:
                self._hunt_step(key, h, st, now)
        self.history.append((total_dl, total_ul))
        if self.cfg["dht"] and self.stats["dht_nodes"] < 10 and now - self._last_bootstrap > 60:
            self._last_bootstrap = now
            self._bootstrap_dht()

    def _hunt_step(self, key, h, st, now):
        info = self._hunt.setdefault(key, {"active": False, "step": 0, "last": 0.0,
                                           "since_data": now, "announces": 0})
        if st.download_payload_rate > 0:
            info["since_data"] = now
        paused = bool(st.flags & lt.torrent_flags.paused)
        busy = st.state in CHECKING_STATES or paused
        if busy:
            info["active"] = False
            return
        if st.is_finished:
            # раздающим хватает обычного расписания трекеров
            starving = False
        else:
            starving = (st.num_peers < HUNT_MIN_PEERS
                        or now - info["since_data"] > HUNT_NO_DATA
                        or st.state == S.downloading_metadata)
        if not starving:
            info.update(active=False, step=0)
            return
        info["active"] = True
        interval = HUNT_STEPS[min(info["step"], len(HUNT_STEPS) - 1)]
        if now - info["last"] >= interval:
            self._announce(h)
            info["last"] = now
            info["step"] += 1
            info["announces"] += 1
            # раз в несколько попыток досыпаем свежие публичные трекеры
            if info["step"] % 3 == 1:
                self._top_up_trackers(h)

    def _top_up_trackers(self, h):
        ti = h.torrent_file()
        if ti is not None and ti.priv():
            return
        have = {t["url"].rstrip("/").lower() for t in h.trackers()}
        top = max((t.get("tier", 0) for t in h.trackers()), default=0)
        for url in self._extra_trackers():
            if url.rstrip("/").lower() not in have:
                h.add_tracker({"url": url, "tier": top})

    def _trackers_loop(self):
        while not self._stop.is_set():
            if self.cfg["extra_trackers"] and self.trackers.stale():
                self.trackers.refresh()
            self._stop.wait(600)

    # ---------- загрузка и сохранение ----------

    def _load_resume(self):
        files = sorted(self.resume_dir.glob("*.fastresume"))
        for f in files:
            try:
                atp = lt.read_resume_data(f.read_bytes())
                h = self.ses.add_torrent(atp)
                self.handles[tid(h.info_hashes())] = h
            except Exception as e:
                log.warning("Не восстановлена раздача %s: %s", f.name, e)
        if files:
            log.info("Восстановлено раздач: %d", len(self.handles))

    def _save_all(self, only_if_modified=False):
        with self._lock:
            hs = list(self.handles.values())
        for h in hs:
            if h.is_valid():
                self._save_resume(h, only_if_modified)

    def _save_state(self):
        try:
            write_atomic(self.state_file, lt.write_session_params_buf(self.ses.session_state()))
        except Exception as e:
            log.warning("Состояние сессии не сохранено: %s", e)

    def shutdown(self):
        """Сохраняет все и останавливает сессию. Ждет не дольше 8 секунд."""
        self.ses.pause()
        self._save_all()
        deadline = time.time() + 8
        with self._resume_cv:
            while self._pending_resume > 0 and time.time() < deadline:
                self._resume_cv.wait(0.2)
        self._save_state()
        self._stop.set()
        log.info("Движок остановлен")


def open_in_explorer(path: str, select: bool = False):
    if select and os.path.exists(path):
        subprocess.Popen(["explorer.exe", "/select,", os.path.normpath(path)])
    elif os.path.isdir(path):
        os.startfile(path)  # noqa: S606 - локальная папка пользователя
    else:
        parent = os.path.dirname(path)
        if os.path.isdir(parent):
            os.startfile(parent)
