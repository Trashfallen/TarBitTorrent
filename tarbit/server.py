"""Локальный HTTP-сервер: отдает интерфейс и JSON API для него.

Слушает только 127.0.0.1. Все /api/* требуют токен (заголовок X-Token): он
выдается окну программы в адресе, чтобы другие сайты в браузере не могли
управлять клиентом.
"""

import base64
import json
import logging
import mimetypes
import os
import secrets
import threading
import time
import uuid
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import unquote, urlparse

from . import APP_NAME, VERSION
from .engine import open_in_explorer
from .paths import resource_dir

log = logging.getLogger(__name__)

MAX_BODY = 64 * 1024 * 1024


class Pending:
    """Раздачи, ждущие подтверждения в окне добавления."""

    def __init__(self):
        self._lock = threading.Lock()
        self.items: dict[str, dict] = {}

    def put(self, info: dict, magnet: str = "", data: bytes = b"") -> dict:
        pid = uuid.uuid4().hex[:12]
        item = dict(info, pid=pid, created=time.time(), magnet=magnet, data=data)
        with self._lock:
            self.items[pid] = item
        return self.public(item)

    def pop(self, pid: str) -> dict | None:
        with self._lock:
            return self.items.pop(pid, None)

    def all(self) -> list[dict]:
        with self._lock:
            return [self.public(i) for i in self.items.values()]

    @staticmethod
    def public(item: dict) -> dict:
        return {k: v for k, v in item.items() if k not in ("data", "magnet")} | {"magnet": bool(item.get("magnet"))}


class Server:
    def __init__(self, app):
        self.app = app
        self.token = secrets.token_urlsafe(24)
        self.pending = Pending()
        self.httpd = ThreadingHTTPServer(("127.0.0.1", 0), self._handler())
        self.httpd.daemon_threads = True
        self.port = self.httpd.server_address[1]

    def start(self):
        threading.Thread(target=self.httpd.serve_forever, name="http", daemon=True).start()
        log.info("Интерфейс на 127.0.0.1:%d", self.port)

    def stop(self):
        self.httpd.shutdown()

    @property
    def url(self) -> str:
        return f"http://127.0.0.1:{self.port}/#t={self.token}"

    # ---------- добавление ----------

    def prepare(self, magnet: str = "", data: bytes = b"", force_dialog=False) -> dict:
        """Разбирает раздачу. Если включено окно добавления - кладет в очередь, иначе сразу добавляет."""
        eng = self.app.engine
        atp = eng.parse_source(magnet, data)
        info = eng.describe(atp)
        if info["id"] in eng.handles:
            res = eng.add(magnet=magnet, data=data)   # допишет трекеры к уже имеющейся
            return {"added": res}
        if self.app.cfg["ask_on_add"] or force_dialog:
            return {"pending": self.pending.put(info, magnet=magnet, data=data)}
        return {"added": eng.add(magnet=magnet, data=data)}

    # ---------- обработчик ----------

    def _handler(self):
        server = self

        class Handler(BaseHTTPRequestHandler):
            protocol_version = "HTTP/1.1"
            server_version = f"{APP_NAME}/{VERSION}"

            def log_message(self, fmt, *args):
                pass

            def _send(self, code, body: bytes, ctype="application/json; charset=utf-8", extra=None):
                self.send_response(code)
                self.send_header("Content-Type", ctype)
                self.send_header("Content-Length", str(len(body)))
                self.send_header("Cache-Control", "no-store")
                self.send_header("X-Content-Type-Options", "nosniff")
                for k, v in (extra or {}).items():
                    self.send_header(k, v)
                self.end_headers()
                self.wfile.write(body)

            def _json(self, obj, code=200):
                self._send(code, json.dumps(obj, ensure_ascii=False, default=str).encode("utf-8"))

            def _body(self) -> dict:
                n = int(self.headers.get("Content-Length") or 0)
                if n > MAX_BODY:
                    raise ValueError("слишком большой запрос")
                raw = self.rfile.read(n) if n else b"{}"
                return json.loads(raw or b"{}")

            def _authorized(self) -> bool:
                return secrets.compare_digest(self.headers.get("X-Token", ""), server.token)

            def do_GET(self):
                path = urlparse(self.path).path
                if path.startswith("/api/"):
                    return self._api("GET", path)
                self._static(path)

            def do_POST(self):
                path = urlparse(self.path).path
                if not path.startswith("/api/"):
                    return self._send(404, b"{}")
                self._api("POST", path)

            def _static(self, path):
                if path in ("", "/"):
                    path = "/index.html"
                root = (resource_dir() / "ui").resolve()
                f = (root / unquote(path).lstrip("/")).resolve()
                if root not in f.parents or not f.is_file():
                    return self._send(404, b"not found", "text/plain")
                ctype = mimetypes.guess_type(f.name)[0] or "application/octet-stream"
                if ctype.startswith("text/") or ctype in ("application/javascript", "image/svg+xml"):
                    ctype += "; charset=utf-8"
                csp = ("default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; "
                       "script-src 'self'; connect-src 'self'")
                self._send(200, f.read_bytes(), ctype, {"Content-Security-Policy": csp})

            def _api(self, method, path):
                if not self._authorized():
                    return self._json({"error": "нет доступа"}, 403)
                try:
                    body = self._body() if method == "POST" else {}
                    res = server.route(method, path, body)
                    self._json(res if res is not None else {"ok": True})
                except KeyError as e:
                    self._json({"error": f"не найдено: {e}"}, 404)
                except Exception as e:
                    log.exception("API %s %s", method, path)
                    self._json({"error": str(e) or type(e).__name__}, 400)

        return Handler

    def route(self, method, path, body):
        app, eng = self.app, self.app.engine
        parts = path.strip("/").split("/")[1:]   # без "api"
        head = parts[0] if parts else ""

        if method == "GET":
            if head == "ping":
                return {"app": APP_NAME, "version": VERSION}
            if head == "state":
                return {"torrents": eng.list_torrents(), "session": eng.session_info(), "pending": self.pending.all(),
                        "update": app.updater.state(), "version": VERSION, "settings": app.cfg.snapshot(),
                        "integration": app.integration_state()}
            if head == "torrent" and len(parts) >= 2:
                key = parts[1]
                sub = parts[2] if len(parts) > 2 else ""
                if sub == "files":
                    return eng.files(key)
                if sub == "peers":
                    return eng.peers(key)
                if sub == "trackers":
                    return eng.tracker_list(key)
                return eng.details(key)
            if head == "settings":
                return app.cfg.snapshot()
            raise KeyError(path)

        # POST
        if head == "add":
            # {magnet} или {torrent: base64}; dialog: true - всегда через окно добавления
            magnet = (body.get("magnet") or "").strip()
            data = base64.b64decode(body["torrent"]) if body.get("torrent") else b""
            if not magnet and not data:
                raise ValueError("нет magnet-ссылки или файла")
            return self.prepare(magnet, data, force_dialog=bool(body.get("dialog")))
        if head == "pending" and len(parts) >= 3:
            item = self.pending.pop(parts[1])
            if item is None:
                raise KeyError(parts[1])
            if parts[2] == "cancel":
                return {"ok": True}
            return eng.add(magnet=item["magnet"], data=item["data"], save_path=body.get("save_path") or "",
                           paused=body.get("paused"), sequential=body.get("sequential"),
                           priorities=body.get("priorities"))
        if head == "torrent" and len(parts) >= 3:
            key, sub = parts[1], parts[2]
            if sub == "action":
                act = body.get("action", "")
                if act == "open_folder":
                    d = eng.details(key)
                    files = eng.files(key)
                    target = d["save_path"]
                    if len(files) == 1:
                        return open_in_explorer(os.path.join(target, files[0]["path"]), select=True)
                    if files:
                        root = files[0]["path"].replace("\\", "/").split("/")[0]
                        cand = os.path.join(target, root)
                        target = cand if os.path.isdir(cand) else target
                    return open_in_explorer(target)
                if act == "open_file":
                    d = eng.details(key)
                    f = next(x for x in eng.files(key) if x["index"] == int(body.get("index", -1)))
                    path = os.path.join(d["save_path"], f["path"])
                    return open_in_explorer(path, select=True)
                return eng.action(key, act, **{k: v for k, v in body.items() if k != "action"})
            if sub == "files":
                return eng.set_file_priorities(key, body.get("priorities") or {})
            if sub == "trackers":
                return eng.add_trackers(key, body.get("urls") or [])
        if head == "bulk":
            act = body.get("action", "")
            errors = []
            for key in body.get("ids") or []:
                try:
                    eng.action(key, act, **{k: v for k, v in body.items() if k not in ("action", "ids")})
                except Exception as e:
                    errors.append(f"{key}: {e}")
            return {"ok": not errors, "errors": errors}
        if head == "pause_all":
            return eng.pause_all()
        if head == "resume_all":
            return eng.resume_all()
        if head == "alt_speed":
            return eng.set_alt_speed(bool(body.get("on")))
        if head == "settings":
            if "autostart" in body:
                app.set_autostart(bool(body.pop("autostart")))
            changed = app.cfg.update(body)
            app.on_settings_changed(changed)
            return app.cfg.snapshot()
        if head == "pick_folder":
            return {"path": app.pick_folder(body.get("initial") or app.cfg.save_path())}
        if head == "open_path":
            return open_in_explorer(body.get("path") or app.cfg.save_path())
        if head == "update":
            if parts[1:] == ["check"]:
                app.updater.check()
                return app.updater.state()
            if parts[1:] == ["install"]:
                threading.Thread(target=app.updater.install, daemon=True).start()
                return app.updater.state()
        if head == "associate":
            return app.associate()
        if head == "open":                       # от второй копии программы
            if app.handle_args(body.get("args") or []):
                app.show_window()
            return {"ok": True}
        if head == "show":
            app.show_window()
            return {"ok": True}
        if head == "quit":
            threading.Thread(target=app.quit, daemon=True).start()
            return {"ok": True}
        raise KeyError(path)
