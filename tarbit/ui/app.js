"use strict";

// ---------- токен и запросы ----------

const TOKEN = (() => {
  const m = location.hash.match(/t=([^&]+)/);
  if (m) {
    try { sessionStorage.setItem("tbt-token", m[1]); } catch (e) { /* без хранилища тоже работаем */ }
    history.replaceState(null, "", location.pathname);
    return m[1];
  }
  try { return sessionStorage.getItem("tbt-token") || ""; } catch (e) { return ""; }
})();

async function api(path, body) {
  const opt = { headers: { "X-Token": TOKEN } };
  if (body !== undefined) {
    opt.method = "POST";
    opt.headers["Content-Type"] = "application/json";
    opt.body = JSON.stringify(body);
  }
  const r = await fetch("/api/" + path, opt);
  let data = null;
  try { data = await r.json(); } catch (e) { /* пустой ответ */ }
  if (!r.ok) throw new Error((data && data.error) || ("HTTP " + r.status));
  return data;
}

const store = {
  get(k, d) { try { const v = localStorage.getItem("tbt-" + k); return v === null ? d : JSON.parse(v); } catch (e) { return d; } },
  set(k, v) { try { localStorage.setItem("tbt-" + k, JSON.stringify(v)); } catch (e) { /* ничего */ } },
};

// ---------- значки ----------

const P = {
  plus: '<path d="M12 5v14M5 12h14"/>',
  play: '<path d="M7 4.5v15l12-7.5z"/>',
  pause: '<path d="M8 5v14M16 5v14"/>',
  trash: '<path d="M4 7h16M10 11v6M14 11v6M6 7l1 13h10l1-13M9 7V4h6v3"/>',
  folder: '<path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"/>',
  gear: '<circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.7 1.7 0 0 0 .3 1.8l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.7 1.7 0 0 0-1.8-.3 1.7 1.7 0 0 0-1 1.5V21a2 2 0 1 1-4 0v-.1a1.7 1.7 0 0 0-1.1-1.5 1.7 1.7 0 0 0-1.8.3l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1a1.7 1.7 0 0 0 .3-1.8 1.7 1.7 0 0 0-1.5-1H3a2 2 0 1 1 0-4h.1a1.7 1.7 0 0 0 1.5-1.1 1.7 1.7 0 0 0-.3-1.8l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1a1.7 1.7 0 0 0 1.8.3H9a1.7 1.7 0 0 0 1-1.5V3a2 2 0 1 1 4 0v.1a1.7 1.7 0 0 0 1 1.5 1.7 1.7 0 0 0 1.8-.3l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1a1.7 1.7 0 0 0-.3 1.8V9a1.7 1.7 0 0 0 1.5 1H21a2 2 0 1 1 0 4h-.1a1.7 1.7 0 0 0-1.5 1z"/>',
  search: '<circle cx="11" cy="11" r="7"/><path d="m20 20-3.5-3.5"/>',
  sun: '<circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4"/>',
  moon: '<path d="M21 12.8A9 9 0 1 1 11.2 3a7 7 0 0 0 9.8 9.8z"/>',
  monitor: '<rect x="3" y="4" width="18" height="12" rx="2"/><path d="M8 20h8M12 16v4"/>',
  turtle: '<path d="M4 15h16M6 15a6 6 0 0 1 12 0M18 15l2-3M6 15l-1 3M18 15l1 3M20 12h1.5"/>',
  x: '<path d="M6 6l12 12M18 6 6 18"/>',
  down: '<path d="M12 4v14M6 12l6 6 6-6"/>',
  up: '<path d="M12 20V6M6 12l6-6 6 6"/>',
  check: '<path d="m5 12 5 5 9-10"/>',
  alert: '<circle cx="12" cy="12" r="9"/><path d="M12 8v5M12 16.5v.5"/>',
  radar: '<circle cx="12" cy="12" r="9"/><circle cx="12" cy="12" r="5"/><path d="M12 12 19 5"/>',
  clock: '<circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/>',
  refresh: '<path d="M20 12a8 8 0 1 1-2.3-5.7L20 8M20 3v5h-5"/>',
  magnet: '<path d="M6 3v8a6 6 0 0 0 12 0V3M6 7h4M14 7h4"/>',
  list: '<path d="M8 6h13M8 12h13M8 18h13M3 6h.01M3 12h.01M3 18h.01"/>',
  zap: '<path d="M13 2 4 14h7l-1 8 9-12h-7z"/>',
  layers: '<path d="m12 3 9 5-9 5-9-5zM3 13l9 5 9-5"/>',
  copy: '<rect x="8" y="8" width="12" height="12" rx="2"/><path d="M16 8V5a1 1 0 0 0-1-1H5a1 1 0 0 0-1 1v10a1 1 0 0 0 1 1h3"/>',
  move: '<path d="M5 12h14M13 6l6 6-6 6"/>',
  sort: '<path d="M7 4v16M3 16l4 4 4-4M17 20V4M13 8l4-4 4 4"/>',
  info: '<circle cx="12" cy="12" r="9"/><path d="M12 11v6M12 7.5v.5"/>',
  globe: '<circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3a14 14 0 0 1 0 18M12 3a14 14 0 0 0 0 18"/>',
  link: '<path d="M10 14a4 4 0 0 0 5.7 0l3-3a4 4 0 0 0-5.7-5.7l-1 1M14 10a4 4 0 0 0-5.7 0l-3 3a4 4 0 0 0 5.7 5.7l1-1"/>',
  box: '<path d="m21 8-9-5-9 5v8l9 5 9-5zM3 8l9 5 9-5M12 13v8"/>',
  hash: '<path d="M5 9h14M5 15h14M10 3 8 21M16 3l-2 18"/>',
  flag: '<path d="M5 21V4M5 4h11l-2 4 2 4H5"/>',
};
const ico = (n, cls = "") => `<svg class="i ${cls}" viewBox="0 0 24 24">${P[n] || ""}</svg>`;

// ---------- форматирование ----------

const UNITS = ["Б", "КБ", "МБ", "ГБ", "ТБ"];
function fmtSize(b) {
  if (b == null || b < 0) return "-";
  let i = 0;
  while (b >= 1024 && i < UNITS.length - 1) { b /= 1024; i++; }
  return (i === 0 ? b.toFixed(0) : b < 10 ? b.toFixed(2) : b < 100 ? b.toFixed(1) : b.toFixed(0)) + " " + UNITS[i];
}
const fmtSpeed = (b) => (b > 0 ? fmtSize(b) + "/с" : "-");
function fmtEta(s) {
  if (s < 0) return "∞";
  if (!s) return "-";
  const d = Math.floor(s / 86400), h = Math.floor(s % 86400 / 3600), m = Math.floor(s % 3600 / 60), sec = s % 60;
  if (d) return `${d} д ${h} ч`;
  if (h) return `${h} ч ${m} мин`;
  if (m) return `${m} мин ${sec} с`;
  return `${sec} с`;
}
function fmtDur(s) { return s > 0 ? fmtEta(s) : "-"; }
function fmtDate(ts) {
  if (!ts || ts <= 0) return "-";
  const d = new Date(ts * 1000);
  return d.toLocaleDateString("ru-RU") + " " + d.toLocaleTimeString("ru-RU", { hour: "2-digit", minute: "2-digit" });
}
const pct = (p) => (p >= 1 ? "100%" : (Math.floor(p * 1000) / 10).toFixed(1) + "%");
const esc = (s) => String(s ?? "").replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]);
const $ = (id) => document.getElementById(id);

const STATUS = {
  downloading: ["Загрузка", "down"],
  seeding: ["Раздача", "up"],
  completed: ["Готово", "check"],
  paused: ["Пауза", "pause"],
  queued: ["В очереди", "clock"],
  queued_seed: ["В очереди", "clock"],
  checking: ["Проверка", "refresh"],
  moving: ["Перемещение", "move"],
  metadata: ["Получаю данные", "magnet"],
  searching: ["Ищу пиров", "radar"],
  stalled: ["Нет данных", "radar"],
  error: ["Ошибка", "alert"],
};

// ---------- состояние ----------

const S = {
  torrents: [],
  byId: new Map(),
  session: {},
  settings: {},
  update: {},
  integration: {},
  pending: [],
  selected: new Set(),
  anchor: null,
  focusId: null,
  filter: store.get("filter", "all"),
  search: "",
  sort: store.get("sort", { key: "added", dir: -1 }),
  tab: store.get("tab", "general"),
  pendingShown: null,
  detailsCache: null,
};

const FILTERS = [
  ["all", "Все", "list", () => true],
  ["downloading", "Загружаются", "down", (t) => ["downloading", "searching", "stalled", "metadata", "queued"].includes(t.status)],
  ["seeding", "Раздаются", "up", (t) => t.status === "seeding" || t.status === "queued_seed"],
  ["completed", "Завершены", "check", (t) => t.progress >= 1],
  ["active", "Активные", "zap", (t) => t.dl > 0 || t.ul > 0],
  ["paused", "На паузе", "pause", (t) => t.status === "paused" || t.status === "completed"],
  ["error", "С ошибкой", "alert", (t) => t.status === "error"],
];

const ALL_COLS = [
  { key: "name", title: "Название", w: null, cls: "name" },
  { key: "size", title: "Размер", w: 84, cls: "num" },
  { key: "progress", title: "Прогресс", w: 130 },
  { key: "status", title: "Статус", w: 120 },
  { key: "seeds", title: "Сиды", w: 74, cls: "num", tip: "подключено (всего в рое)" },
  { key: "peers", title: "Пиры", w: 74, cls: "num", tip: "подключено (всего в рое)" },
  { key: "dl", title: "Загрузка", w: 92, cls: "num" },
  { key: "ul", title: "Отдача", w: 92, cls: "num" },
  { key: "eta", title: "Осталось", w: 92, cls: "num" },
  { key: "ratio", title: "Рейтинг", w: 66, cls: "num" },
  { key: "added", title: "Добавлен", w: 124, cls: "num" },
  { key: "uploaded", title: "Отдано", w: 84, cls: "num" },
];
let hiddenCols = new Set(store.get("hiddenCols", ["ratio", "added", "uploaded"]));
let COLS = ALL_COLS.filter((c) => !hiddenCols.has(c.key));

function toggleCol(key) {
  hiddenCols.has(key) ? hiddenCols.delete(key) : hiddenCols.add(key);
  store.set("hiddenCols", [...hiddenCols]);
  COLS = ALL_COLS.filter((c) => !hiddenCols.has(c.key));
  for (const r of rowCache.values()) r.tr.remove();
  rowCache.clear();
  buildHead();
  renderList();
}

// ---------- тема ----------

function applyTheme() {
  let t = S.settings.theme || store.get("theme", "dark");
  store.set("theme", t);
  if (t === "system") t = matchMedia("(prefers-color-scheme: light)").matches ? "light" : "dark";
  document.documentElement.dataset.theme = t;
  const cur = S.settings.theme || "dark";
  $("btnTheme").innerHTML = ico(cur === "light" ? "sun" : cur === "system" ? "monitor" : "moon");
}
matchMedia("(prefers-color-scheme: light)").addEventListener("change", applyTheme);
document.documentElement.dataset.theme = (() => {
  const t = store.get("theme", "dark");
  return t === "system" ? (matchMedia("(prefers-color-scheme: light)").matches ? "light" : "dark") : t;
})();

async function cycleTheme() {
  const order = ["dark", "light", "system"];
  const next = order[(order.indexOf(S.settings.theme || "dark") + 1) % order.length];
  S.settings = await api("settings", { theme: next });
  applyTheme();
  toast(next === "dark" ? "Темная тема" : next === "light" ? "Светлая тема" : "Тема как в Windows", "ok");
}

// ---------- уведомления ----------

function toast(text, kind = "ok", ms = 3500) {
  const el = document.createElement("div");
  el.className = "toast " + kind;
  el.innerHTML = ico(kind === "err" ? "alert" : kind === "ok" ? "check" : "info") + `<div>${esc(text)}</div>`;
  $("toasts").appendChild(el);
  setTimeout(() => el.remove(), ms);
}

async function run(fn, okText) {
  try {
    const r = await fn();
    if (okText) toast(okText, "ok");
    refresh();
    return r;
  } catch (e) {
    toast(e.message, "err", 6000);
  }
}

// ---------- таблица ----------

function buildHead() {
  $("cols").innerHTML = COLS.map((c) => `<col${c.w ? ` style="width:${c.w}px"` : ""}>`).join("");
  $("thead").innerHTML = COLS.map((c) => {
    const sorted = S.sort.key === c.key;
    return `<th data-key="${c.key}" class="${c.cls === "num" ? "num " : ""}${sorted ? "sorted" + (S.sort.dir > 0 ? " asc" : "") : ""}"${c.tip ? ` title="${c.tip}"` : ""}>${c.title}</th>`;
  }).join("");
}

$("thead").addEventListener("contextmenu", (e) => {
  e.preventDefault();
  showMenu(e.clientX, e.clientY, ALL_COLS.filter((c) => c.key !== "name").map((c) => ({
    title: c.title, icon: hiddenCols.has(c.key) ? "" : "check", fn: () => toggleCol(c.key),
  })));
});
$("thead").addEventListener("click", (e) => {
  const th = e.target.closest("th");
  if (!th) return;
  const key = th.dataset.key;
  S.sort = { key, dir: S.sort.key === key ? -S.sort.dir : (key === "name" ? 1 : -1) };
  store.set("sort", S.sort);
  buildHead();
  renderList();
});

function visibleTorrents() {
  const f = FILTERS.find((x) => x[0] === S.filter) || FILTERS[0];
  const q = S.search.trim().toLowerCase();
  const list = S.torrents.filter((t) => f[3](t) && (!q || t.name.toLowerCase().includes(q)));
  const { key, dir } = S.sort;
  list.sort((a, b) => {
    let x = a[key], y = b[key];
    if (key === "name" || key === "status") return dir * String(x).localeCompare(String(y), "ru");
    if (key === "eta") { x = x < 0 ? 1e12 : x; y = y < 0 ? 1e12 : y; }
    return dir * ((x || 0) - (y || 0)) || a.name.localeCompare(b.name, "ru");
  });
  return list;
}

function rowCells(t) {
  const [label, icon] = STATUS[t.status] || [t.status, "info"];
  const st = `s-${t.status}`;
  const pulse = t.status === "searching" || t.status === "metadata" ? " searching-pulse" : "";
  const statusText = t.status === "error" ? (t.error || label) : label;
  return {
    name: `<div class="cell-name ${st}"><div class="st-ico${pulse}">${ico(icon)}</div><span title="${esc(t.name)}">${esc(t.name)}</span></div>`,
    size: t.has_metadata ? fmtSize(t.size) : "-",
    progress: `<div class="progress ${st}"><i style="width:${(t.progress * 100).toFixed(2)}%"></i><span>${pct(t.progress)}</span></div>`,
    status: `<span class="status-text ${st}${pulse}" title="${esc(statusText)}">${esc(statusText)}</span>`,
    seeds: `${t.seeds}${t.seeds_total >= 0 ? ` <span class="muted">(${t.seeds_total})</span>` : ""}`,
    peers: `${t.peers}${t.peers_total >= 0 ? ` <span class="muted">(${t.peers_total})</span>` : ""}`,
    dl: fmtSpeed(t.dl),
    ul: fmtSpeed(t.ul),
    eta: t.progress >= 1 ? "-" : fmtEta(t.eta),
    ratio: t.ratio.toFixed(2),
    added: fmtDate(t.added),
    uploaded: fmtSize(t.uploaded),
  };
}

const rowCache = new Map();   // id -> {tr, html: {key: html}}

function renderList() {
  const list = visibleTorrents();
  const tbody = $("tbody");
  const seen = new Set();
  let prev = null;
  for (const t of list) {
    seen.add(t.id);
    let r = rowCache.get(t.id);
    if (!r) {
      const tr = document.createElement("tr");
      tr.dataset.id = t.id;
      tr.innerHTML = COLS.map((c) => `<td class="${c.cls || ""}"></td>`).join("");
      r = { tr, html: {} };
      rowCache.set(t.id, r);
    }
    const cells = rowCells(t);
    COLS.forEach((c, i) => {
      if (r.html[c.key] !== cells[c.key]) {
        const td = r.tr.children[i];
        // у полосы прогресса меняем только ширину, чтобы работала анимация
        if (c.key === "progress" && r.html.progress && td.firstChild) {
          const bar = td.firstChild;
          bar.className = `progress s-${t.status}`;
          bar.firstChild.style.width = (t.progress * 100).toFixed(2) + "%";
          bar.lastChild.textContent = pct(t.progress);
        } else {
          td.innerHTML = cells[c.key];
        }
        r.html[c.key] = cells[c.key];
      }
    });
    r.tr.classList.toggle("sel", S.selected.has(t.id));
    const want = prev ? prev.nextSibling : tbody.firstChild;
    if (want !== r.tr) tbody.insertBefore(r.tr, want);
    prev = r.tr;
  }
  for (const [id, r] of rowCache) {
    if (!seen.has(id)) { r.tr.remove(); if (!S.byId.has(id)) rowCache.delete(id); }
  }
  $("empty").classList.toggle("show", S.torrents.length === 0);
}

function renderFilters() {
  $("filters").innerHTML = FILTERS.map(([key, title, icon, fn]) =>
    `<div class="filter${S.filter === key ? " active" : ""}" data-f="${key}">${ico(icon)}<span>${title}</span><span class="n">${S.torrents.filter(fn).length}</span></div>`).join("");
}
$("filters").addEventListener("click", (e) => {
  const f = e.target.closest(".filter");
  if (!f) return;
  S.filter = f.dataset.f;
  store.set("filter", S.filter);
  renderFilters();
  renderList();
});

function renderNet() {
  const s = S.session;
  const dht = s.dht_nodes || 0;
  const dhtCls = !S.settings.dht ? "" : dht > 50 ? "ok" : dht > 0 ? "warn" : "err";
  const port = s.incoming ? ["ok", "открыт"] : s.upnp && s.upnp.startsWith("проброшен") ? ["ok", "проброшен"] : ["warn", "не проверен"];
  $("net").innerHTML = `
    <div class="row" title="Узлы распределенной сети: через них находятся пиры без трекеров"><span><i class="dot ${dhtCls}"></i>DHT</span><b>${S.settings.dht ? dht + " узл." : "выкл"}</b></div>
    <div class="row" title="Пиры, с которыми сейчас есть соединение"><span><i class="dot ${s.peers ? "ok" : ""}"></i>Пиры</span><b>${s.peers || 0}</b></div>
    <div class="row" title="${esc(s.upnp || "Входящих соединений пока не было")}"><span><i class="dot ${port[0]}"></i>Порт ${s.port || ""}</span><b>${port[1]}</b></div>
    <div class="row" title="Публичные трекеры, которые подмешиваются в открытые раздачи"><span><i class="dot ${S.settings.extra_trackers ? "ok" : ""}"></i>Доп. трекеры</span><b>${S.settings.extra_trackers ? s.trackers_pool : "выкл"}</b></div>
    <div class="row" title="Переобъявление раздач, у которых мало пиров"><span><i class="dot ${S.settings.peer_hunter ? "ok" : ""}"></i>Охотник</span><b>${S.settings.peer_hunter ? S.torrents.filter((t) => t.hunting).length + " ищут" : "выкл"}</b></div>`;
}

function renderFooter() {
  const s = S.session;
  $("fDl").innerHTML = ico("down") + fmtSpeed(s.dl || 0).replace("-", "0 Б/с");
  $("fUl").innerHTML = ico("up") + fmtSpeed(s.ul || 0).replace("-", "0 Б/с");
  $("fDht").textContent = S.settings.dht ? `DHT: ${s.dht_nodes || 0}` : "DHT выкл";
  $("fPeers").textContent = `Соединений: ${s.peers || 0}`;
  const u = S.update;
  $("fVer").innerHTML = `v${esc(S.version || "")}${u.available ? " · доступна " + esc(u.version) : ""}`;
  drawGraph($("fGraph"), s.history || [], false);
  const active = (s.dl || 0) + (s.ul || 0) > 0;
  const parts = [];
  if (s.dl > 0) parts.push("↓ " + fmtSpeed(s.dl));
  if (s.ul > 0) parts.push("↑ " + fmtSpeed(s.ul));
  document.title = active ? `${parts.join(" ")} - TarBitTorrent` : "TarBitTorrent";
  $("btnAlt").classList.toggle("on", !!S.settings.alt_enabled);
}

function cssVar(n) { return getComputedStyle(document.documentElement).getPropertyValue(n).trim(); }

function drawGraph(cv, hist, big) {
  const dpr = window.devicePixelRatio || 1;
  const w = cv.clientWidth, h = cv.clientHeight;
  if (!w || !h) return;
  if (cv.width !== w * dpr || cv.height !== h * dpr) { cv.width = w * dpr; cv.height = h * dpr; }
  const g = cv.getContext("2d");
  g.setTransform(dpr, 0, 0, dpr, 0, 0);
  g.clearRect(0, 0, w, h);
  const n = big ? 300 : 120;
  const data = hist.slice(-n);
  const max = Math.max(1024, ...data.map((x) => Math.max(x[0], x[1]))) * 1.15;
  const pad = big ? 22 : 1;
  if (big) {
    g.strokeStyle = cssVar("--line");
    g.fillStyle = cssVar("--text-3");
    g.font = "11px Segoe UI";
    g.lineWidth = 1;
    for (let i = 0; i <= 4; i++) {
      const y = pad + (h - 2 * pad) * i / 4;
      g.beginPath(); g.moveTo(60, y); g.lineTo(w - 6, y); g.stroke();
      g.fillText(fmtSpeed(max * (1 - i / 4)).replace("-", "0"), 4, y + 4);
    }
  }
  const x0 = big ? 60 : 0;
  const series = [[0, cssVar("--dl")], [1, cssVar("--ul")]];
  for (const [k, color] of series) {
    if (data.length < 2) continue;
    const pts = data.map((v, i) => [x0 + (w - x0 - (big ? 6 : 0)) * (i + n - data.length) / (n - 1), h - pad - (h - 2 * pad) * v[k] / max]);
    const grad = g.createLinearGradient(0, 0, 0, h);
    grad.addColorStop(0, color + "66");
    grad.addColorStop(1, color + "00");
    g.beginPath();
    g.moveTo(pts[0][0], h - pad);
    pts.forEach((p) => g.lineTo(p[0], p[1]));
    g.lineTo(pts[pts.length - 1][0], h - pad);
    g.fillStyle = grad;
    g.fill();
    g.beginPath();
    pts.forEach((p, i) => (i ? g.lineTo(p[0], p[1]) : g.moveTo(p[0], p[1])));
    g.strokeStyle = color;
    g.lineWidth = big ? 2 : 1.5;
    g.stroke();
  }
}

// ---------- выбор строк ----------

function setSelection(ids, focus) {
  S.selected = new Set(ids);
  if (focus !== undefined) S.focusId = focus;
  if (S.focusId && !S.selected.has(S.focusId)) S.focusId = S.selected.size ? [...S.selected][S.selected.size - 1] : null;
  for (const [id, r] of rowCache) r.tr.classList.toggle("sel", S.selected.has(id));
  S.detailsCache = null;
  renderDetails(true);
  updateToolbar();
}

$("tbody").addEventListener("mousedown", (e) => {
  const tr = e.target.closest("tr");
  if (!tr) return;
  const id = tr.dataset.id;
  if (e.button === 2 && S.selected.has(id)) return;
  if (e.shiftKey && S.anchor) {
    const ids = visibleTorrents().map((t) => t.id);
    const a = ids.indexOf(S.anchor), b = ids.indexOf(id);
    setSelection(ids.slice(Math.min(a, b), Math.max(a, b) + 1), id);
  } else if (e.ctrlKey) {
    const s = new Set(S.selected);
    s.has(id) ? s.delete(id) : s.add(id);
    S.anchor = id;
    setSelection(s, id);
  } else {
    S.anchor = id;
    setSelection([id], id);
  }
});
$("tbody").addEventListener("dblclick", (e) => {
  const tr = e.target.closest("tr");
  if (tr) act([tr.dataset.id], "open_folder");
});
$("list").addEventListener("mousedown", (e) => {
  if (e.target === $("list") || e.target.closest(".empty")) setSelection([]);
});

function updateToolbar() {
  const n = S.selected.size;
  ["btnResume", "btnPause", "btnDelete", "btnFolder"].forEach((id) => { $(id).disabled = !n; });
}

// ---------- действия ----------

async function act(ids, action, extra = {}) {
  ids = [...ids];
  if (!ids.length) return;
  if (action === "open_folder") return run(() => api(`torrent/${ids[0]}/action`, { action }));
  if (action === "copy_magnet") {
    const links = [];
    for (const id of ids) links.push((await api(`torrent/${id}`)).magnet);
    await navigator.clipboard.writeText(links.join("\n")).catch(() => showText("Magnet", links.join("\n")));
    return toast(ids.length > 1 ? "Ссылки скопированы" : "Ссылка скопирована", "ok");
  }
  if (action === "move") {
    const t = S.byId.get(ids[0]);
    const r = await run(() => api("pick_folder", { initial: t ? t.save_path : "" }));
    if (!r || !r.path) return;
    extra = { path: r.path };
  }
  const names = { pause: "Приостановлено", resume: "Возобновлено", force: "Запущено вне очереди", recheck: "Проверка запущена",
    reannounce: "Ищу пиров на трекерах и в DHT", move: "Перемещаю файлы" };
  return run(() => api("bulk", { ids, action, ...extra }), names[action]);
}

function askDelete(ids) {
  ids = [...ids];
  if (!ids.length) return;
  const names = ids.map((id) => (S.byId.get(id) || {}).name || id);
  $("delTitle").textContent = ids.length > 1 ? `Удалить раздачи (${ids.length})?` : "Удалить раздачу?";
  $("delNames").innerHTML = names.slice(0, 6).map(esc).join("<br>") + (names.length > 6 ? `<br>и еще ${names.length - 6}` : "");
  $("delData").checked = false;
  $("delGo").onclick = async () => {
    closeOverlay("delOverlay");
    await run(() => api("bulk", { ids, action: "remove", with_data: $("delData").checked }),
      $("delData").checked ? "Удалено вместе с файлами" : "Удалено из списка");
    setSelection([]);
  };
  openOverlay("delOverlay");
}

$("btnResume").onclick = () => act(S.selected, "resume");
$("btnPause").onclick = () => act(S.selected, "pause");
$("btnDelete").onclick = () => askDelete(S.selected);
$("btnFolder").onclick = () => act(S.selected, "open_folder");
$("btnAlt").onclick = () => run(() => api("alt_speed", { on: !S.settings.alt_enabled }),
  !S.settings.alt_enabled ? "Скорость ограничена" : "Ограничение снято");
$("btnTheme").onclick = cycleTheme;
$("btnSettings").onclick = () => openSettings();
$("search").addEventListener("input", (e) => { S.search = e.target.value; renderList(); });

// ---------- контекстное меню ----------

function showMenu(x, y, items) {
  const m = $("menu");
  m.innerHTML = items.map((it, i) => it === "-" ? "<hr>" : `<a data-i="${i}" class="${it.danger ? "danger" : ""}">${ico(it.icon)}${esc(it.title)}</a>`).join("");
  m.classList.add("show");
  const r = m.getBoundingClientRect();
  m.style.left = Math.min(x, innerWidth - r.width - 8) + "px";
  m.style.top = Math.min(y, innerHeight - r.height - 8) + "px";
  m.onclick = (e) => {
    const a = e.target.closest("a");
    if (!a) return;
    hideMenu();
    items[+a.dataset.i].fn();
  };
}
function hideMenu() { $("menu").classList.remove("show"); }
document.addEventListener("mousedown", (e) => { if (!e.target.closest(".menu")) hideMenu(); });
window.addEventListener("blur", hideMenu);

$("list").addEventListener("contextmenu", (e) => {
  e.preventDefault();
  const tr = e.target.closest("tr");
  if (!tr) return showMenu(e.clientX, e.clientY, [{ title: "Добавить раздачу", icon: "plus", fn: openAdd }]);
  const ids = [...S.selected];
  const one = ids.length === 1 ? S.byId.get(ids[0]) : null;
  showMenu(e.clientX, e.clientY, [
    { title: "Открыть папку", icon: "folder", fn: () => act(ids, "open_folder") },
    "-",
    { title: "Возобновить", icon: "play", fn: () => act(ids, "resume") },
    { title: "Приостановить", icon: "pause", fn: () => act(ids, "pause") },
    { title: "Запустить вне очереди", icon: "zap", fn: () => act(ids, "force") },
    "-",
    { title: "Найти пиров сейчас", icon: "radar", fn: () => act(ids, "reannounce") },
    { title: "Проверить файлы", icon: "refresh", fn: () => act(ids, "recheck") },
    { title: one && one.sequential ? "Обычный порядок загрузки" : "Качать по порядку", icon: "sort", fn: () => act(ids, "sequential", { value: !(one && one.sequential) }) },
    { title: "Переместить файлы...", icon: "move", fn: () => act(ids, "move") },
    "-",
    { title: "Вверх в очереди", icon: "up", fn: () => act(ids, "queue_up") },
    { title: "Вниз в очереди", icon: "down", fn: () => act(ids, "queue_down") },
    "-",
    { title: "Копировать magnet-ссылку", icon: "copy", fn: () => act(ids, "copy_magnet") },
    { title: "Удалить...", icon: "trash", danger: true, fn: () => askDelete(ids) },
  ]);
});

// ---------- подробности ----------

$("tabs").addEventListener("click", (e) => {
  const t = e.target.closest(".tab");
  if (!t) return;
  S.tab = t.dataset.tab;
  store.set("tab", S.tab);
  document.querySelectorAll(".tab").forEach((x) => x.classList.toggle("active", x === t));
  S.detailsCache = null;
  renderDetails(true);
});
document.querySelectorAll(".tab").forEach((x) => x.classList.toggle("active", x.dataset.tab === S.tab));

let detailsBusy = false;
async function renderDetails(force) {
  const pane = $("pane");
  const id = S.focusId;
  if (!id || !S.byId.has(id)) {
    pane.innerHTML = `<div class="muted" style="padding:20px;text-align:center">Выберите раздачу, чтобы увидеть подробности</div>`;
    return;
  }
  if (detailsBusy && !force) return;
  detailsBusy = true;
  try {
    const tab = S.tab;
    if (tab === "general") renderGeneral(pane, await api(`torrent/${id}`));
    else if (tab === "files") renderFiles(pane, id, await api(`torrent/${id}/files`), force);
    else if (tab === "peers") renderPeers(pane, await api(`torrent/${id}/peers`));
    else if (tab === "trackers") renderTrackers(pane, id, await api(`torrent/${id}/trackers`), force);
    else if (tab === "speed") renderSpeed(pane, await api(`torrent/${id}`));
  } catch (e) {
    if (!String(e.message).startsWith("не найдено")) pane.innerHTML = `<div class="muted">${esc(e.message)}</div>`;
  } finally {
    detailsBusy = false;
  }
}

function renderGeneral(pane, d) {
  const hunt = d.hunt || {};
  const huntHtml = d.hunting
    ? `<div class="hunt">${ico("radar", "searching-pulse")}<div><b>Ищу пиров.</b> Переобъявлений: ${hunt.announces || 0}. Опрашиваются все трекеры (${d.private ? "раздача приватная, DHT и PEX выключены" : "включая публичные"}), DHT, PEX и локальная сеть.</div></div>`
    : "";
  const kv = [
    ["Сохраняется в", `<span title="${esc(d.save_path)}">${esc(d.save_path)}</span>`],
    ["Размер", d.has_metadata ? fmtSize(d.size) : "неизвестен"],
    ["Скачано", `${fmtSize(d.done)} (${pct(d.progress)})`],
    ["Загрузка / отдача", `${fmtSpeed(d.dl)} / ${fmtSpeed(d.ul)}`],
    ["Осталось", d.progress >= 1 ? "-" : fmtEta(d.eta)],
    ["Сиды", `${d.seeds} из ${d.seeds_total >= 0 ? d.seeds_total : "?"}`],
    ["Пиры", `${d.peers} из ${d.peers_total >= 0 ? d.peers_total : "?"}`],
    ["Кандидатов для соединения", d.candidates],
    ["Доступность", d.availability >= 0 ? d.availability.toFixed(2) : "-"],
    ["Всего скачано", fmtSize(d.downloaded)],
    ["Всего отдано", fmtSize(d.uploaded)],
    ["Рейтинг", d.ratio.toFixed(3)],
    ["Отброшено", fmtSize(d.wasted)],
    ["Время в работе", fmtDur(d.active_time)],
    ["Время раздачи", fmtDur(d.seeding_time)],
    ["Куски", d.piece_count ? `${d.pieces} / ${d.piece_count} по ${fmtSize(d.piece_size)}` : "-"],
    ["Добавлен", fmtDate(d.added)],
    ["Завершен", fmtDate(d.completed)],
    ["Полный сид был", fmtDate(d.last_seen_complete)],
    ["Создан", fmtDate(d.created)],
    ["Создатель", esc(d.creator || "-")],
    ["Приватная", d.private ? "да" : "нет"],
    ["Хеш v1", `<span class="mono">${esc(d.hash_v1 || "-")}</span>`],
    ["Хеш v2", `<span class="mono" title="${esc(d.hash_v2)}">${esc(d.hash_v2 || "-")}</span>`],
    ["Комментарий", `<span title="${esc(d.comment)}">${esc(d.comment || "-")}</span>`],
  ];
  pane.innerHTML = `<canvas class="pieces" id="pieces"></canvas>${huntHtml}${d.error ? `<div class="hunt" style="color:var(--err)">${ico("alert")}${esc(d.error)}</div>` : ""}
    <div class="kv">${kv.map(([k, v]) => `<div><span>${k}</span><b>${v}</b></div>`).join("")}</div>`;
  drawPieces($("pieces"), d.piece_map || [], d.progress);
}

function drawPieces(cv, map, progress) {
  const dpr = window.devicePixelRatio || 1;
  const w = cv.clientWidth, h = cv.clientHeight;
  cv.width = w * dpr; cv.height = h * dpr;
  const g = cv.getContext("2d");
  g.scale(dpr, dpr);
  if (!map.length) {
    g.fillStyle = cssVar("--dl");
    g.fillRect(0, 0, w * progress, h);
    return;
  }
  const cw = w / map.length;
  map.forEach((v, i) => {
    if (!v) return;
    g.fillStyle = v >= 9 ? cssVar("--dl") : `rgba(61,139,255,${0.15 + v / 12})`;
    g.fillRect(Math.floor(i * cw), 0, Math.ceil(cw), h);
  });
}

const PRIO = [[0, "Не качать"], [1, "Низкий"], [4, "Обычный"], [7, "Высокий"]];
function renderFiles(pane, id, files, force) {
  if (!files.length) {
    pane.innerHTML = `<div class="muted">Список файлов появится, когда будут получены данные о раздаче.</div>`;
    return;
  }
  const key = id + ":" + files.length;
  if (force || pane.dataset.key !== key) {
    pane.dataset.key = key;
    pane.innerHTML = `<table><colgroup><col style="width:34px"><col><col style="width:100px"><col style="width:160px"><col style="width:130px"></colgroup>
      <thead><tr><th></th><th>Имя</th><th class="num">Размер</th><th>Прогресс</th><th>Приоритет</th></tr></thead>
      <tbody>${files.map((f) => `<tr data-i="${f.index}"><td><input type="checkbox" class="fchk"></td><td class="name" title="${esc(f.path)}">${esc(f.path)}</td>
        <td class="num">${fmtSize(f.size)}</td><td><div class="progress"><i></i><span></span></div></td>
        <td><select class="prio">${PRIO.map(([v, n]) => `<option value="${v}">${n}</option>`).join("")}</select></td></tr>`).join("")}</tbody></table>`;
    pane.querySelector("tbody").addEventListener("change", (e) => {
      const tr = e.target.closest("tr");
      const i = tr.dataset.i;
      let p;
      if (e.target.classList.contains("fchk")) p = e.target.checked ? 4 : 0;
      else p = +e.target.value;
      run(() => api(`torrent/${id}/files`, { priorities: { [i]: p } }));
    });
    pane.querySelector("tbody").addEventListener("dblclick", (e) => {
      const tr = e.target.closest("tr");
      if (tr && !e.target.closest("select,input")) run(() => api(`torrent/${id}/action`, { action: "open_file", index: +tr.dataset.i }));
    });
  }
  const rows = pane.querySelectorAll("tbody tr");
  files.forEach((f, n) => {
    const tr = rows[n];
    if (!tr) return;
    const bar = tr.querySelector(".progress");
    bar.className = "progress " + (f.priority === 0 ? "s-paused" : f.progress >= 1 ? "s-seeding" : "s-downloading");
    bar.firstChild.style.width = (f.progress * 100).toFixed(1) + "%";
    bar.lastChild.textContent = pct(f.progress);
    const sel = tr.querySelector("select");
    const near = PRIO.reduce((a, b) => (Math.abs(b[0] - f.priority) < Math.abs(a[0] - f.priority) ? b : a))[0];
    if (document.activeElement !== sel) sel.value = String(near);
    tr.querySelector(".fchk").checked = f.priority > 0;
  });
}

function renderPeers(pane, peers) {
  if (!peers.length) {
    pane.innerHTML = `<div class="muted">Пока нет соединений. Программа продолжает искать пиров через трекеры, DHT, PEX и локальную сеть.</div>`;
    return;
  }
  pane.innerHTML = `<table><colgroup><col style="width:190px"><col><col style="width:120px"><col style="width:95px"><col style="width:95px"><col style="width:95px"><col style="width:180px"></colgroup>
    <thead><tr><th>Адрес</th><th>Клиент</th><th>Прогресс</th><th class="num">Загрузка</th><th class="num">Отдача</th><th class="num">Скачано</th><th>Откуда</th></tr></thead>
    <tbody>${peers.map((p) => `<tr><td class="mono sel-text">${esc(p.ip.includes(":") ? `[${p.ip}]` : p.ip)}:${p.port}${p.encrypted ? " 🔒" : ""}</td>
      <td title="${esc(p.client)}">${p.seed ? '<span class="chip ok">сид</span>' : ""}${esc(p.client || "?")}</td>
      <td><div class="progress ${p.seed ? "s-seeding" : "s-downloading"}"><i style="width:${(p.progress * 100).toFixed(1)}%"></i><span>${pct(p.progress)}</span></div></td>
      <td class="num">${fmtSpeed(p.dl)}</td><td class="num">${fmtSpeed(p.ul)}</td><td class="num">${fmtSize(p.downloaded)}</td>
      <td>${p.sources.map((s) => `<span class="chip">${esc(s)}</span>`).join("")}</td></tr>`).join("")}</tbody></table>`;
}

const TR_STATUS = { working: ["работает", "ok"], updating: ["опрашиваю", "warn"], error: ["ошибка", "err"], idle: ["ждет", ""], disabled: ["выключено", ""] };
function trackerRows(rows) {
  return rows.map((r) => {
      const [label, cls] = TR_STATUS[r.status] || [r.status, ""];
      if (r.pseudo) return `<tr><td class="muted">${esc(r.url)}</td><td><span class="chip ${cls}">${label}</span></td><td class="num" colspan="2">${r.connected} пир.</td><td></td><td></td></tr>`;
      return `<tr><td class="mono sel-text" title="${esc(r.url)}">${esc(r.url)}</td><td><span class="chip ${cls}">${label}</span></td>
        <td class="num">${r.seeds >= 0 ? r.seeds : "-"}</td><td class="num">${r.peers >= 0 ? r.peers : "-"}</td>
        <td class="num">${r.next ? fmtEta(r.next) : "-"}</td><td title="${esc(r.message)}">${esc(r.message)}</td></tr>`;
  }).join("");
}

function renderTrackers(pane, id, rows, force) {
  const body = pane.querySelector("#trBody");
  if (body && !force && pane.dataset.key === "tr:" + id) {
    body.innerHTML = trackerRows(rows);
    return;
  }
  pane.dataset.key = "tr:" + id;
  pane.innerHTML = `<table><colgroup><col><col style="width:110px"><col style="width:80px"><col style="width:80px"><col style="width:90px"><col style="width:260px"></colgroup>
    <thead><tr><th>Адрес</th><th>Статус</th><th class="num">Сиды</th><th class="num">Пиры</th><th class="num">Через</th><th>Сообщение</th></tr></thead>
    <tbody id="trBody">${trackerRows(rows)}</tbody></table>
    <div class="field" style="margin-top:14px"><label>Добавить трекеры (по одному на строку)</label>
      <textarea class="input" id="trAdd" style="height:70px" spellcheck="false"></textarea>
      <div><button class="btn" id="trAddGo">${ico("plus")}Добавить и опросить</button></div></div>`;
  const ta = pane.querySelector("#trAdd");
  pane.querySelector("#trAddGo").onclick = () => {
    const urls = ta.value.split(/\s+/).filter(Boolean);
    if (!urls.length) return;
    ta.value = "";
    run(() => api(`torrent/${id}/trackers`, { urls }), "Трекеры добавлены");
  };
}

function renderSpeed(pane, d) {
  if (!pane.querySelector("#tGraph")) {
    pane.innerHTML = `<div class="legend"><span><i style="background:var(--dl)"></i>Загрузка <b id="lgDl"></b></span><span><i style="background:var(--ul)"></i>Отдача <b id="lgUl"></b></span><span class="muted">последние 2 минуты</span></div>
      <canvas class="graph" id="tGraph"></canvas>`;
  }
  $("lgDl").textContent = fmtSpeed(d.dl);
  $("lgUl").textContent = fmtSpeed(d.ul);
  const cv = $("tGraph");
  cv.style.height = Math.max(pane.clientHeight - 60, 120) + "px";
  drawGraph(cv, d.history || [], true);
}

// ---------- разделитель ----------

(() => {
  const det = $("details");
  let h = store.get("detailsH", 280);
  det.style.height = h + "px";
  let start = null;
  $("splitter").addEventListener("mousedown", (e) => { start = [e.clientY, det.offsetHeight]; e.preventDefault(); });
  window.addEventListener("mousemove", (e) => {
    if (!start) return;
    h = Math.max(120, Math.min(innerHeight - 220, start[1] - (e.clientY - start[0])));
    det.style.height = h + "px";
  });
  window.addEventListener("mouseup", () => { if (start) { start = null; store.set("detailsH", h); renderDetails(true); } });
})();

// ---------- добавление ----------

function openAdd() {
  $("addMagnet").value = "";
  openOverlay("addOverlay");
  setTimeout(() => $("addMagnet").focus(), 50);
}
$("btnAdd").innerHTML = ico("plus") + "Добавить";
$("btnAdd").onclick = openAdd;
$("emptyAdd").onclick = openAdd;
$("addDrop").onclick = () => $("addFile").click();
$("addFile").onchange = async (e) => {
  const files = [...e.target.files];
  e.target.value = "";
  closeOverlay("addOverlay");
  await addFiles(files);
};
$("addGo").onclick = async () => {
  const lines = $("addMagnet").value.split(/\s+/).filter(Boolean);
  if (!lines.length) return toast("Вставьте magnet-ссылку или выберите файл", "err");
  closeOverlay("addOverlay");
  for (const l of lines) await addMagnet(l);
};

function toMagnet(s) {
  s = s.trim();
  if (/^[0-9a-f]{40}$/i.test(s) || /^[a-z2-7]{32}$/i.test(s)) return "magnet:?xt=urn:btih:" + s;
  if (/^[0-9a-f]{64}$/i.test(s)) return "magnet:?xt=urn:btmh:1220" + s;
  return s.toLowerCase().startsWith("magnet:") ? s : null;
}

async function addMagnet(s) {
  const m = toMagnet(s);
  if (!m) return toast("Это не magnet-ссылка: " + s.slice(0, 60), "err");
  const r = await run(() => api("add", { magnet: m }));
  afterAdd(r);
}

function readB64(file) {
  return new Promise((res, rej) => {
    const fr = new FileReader();
    fr.onload = () => res(String(fr.result).split(",")[1]);
    fr.onerror = () => rej(fr.error);
    fr.readAsDataURL(file);
  });
}

async function addFiles(files) {
  for (const f of files) {
    if (!/\.torrent$/i.test(f.name)) { toast("Пропущен не .torrent: " + f.name, "err"); continue; }
    const r = await run(async () => api("add", { torrent: await readB64(f) }));
    afterAdd(r);
  }
}

function afterAdd(r) {
  if (!r) return;
  if (r.added) toast(r.added.duplicate ? `Уже есть: ${r.added.name}` : `Добавлено: ${r.added.name}`, "ok");
  if (r.pending) { S.pending.push(r.pending); showPending(); }
}

// перетаскивание файлов и ссылок в окно
let dragDepth = 0;
window.addEventListener("dragenter", (e) => { e.preventDefault(); dragDepth++; $("dragover").classList.add("show"); });
window.addEventListener("dragleave", () => { if (--dragDepth <= 0) { dragDepth = 0; $("dragover").classList.remove("show"); } });
window.addEventListener("dragover", (e) => e.preventDefault());
window.addEventListener("drop", async (e) => {
  e.preventDefault();
  dragDepth = 0;
  $("dragover").classList.remove("show");
  closeOverlay("addOverlay");
  const files = [...e.dataTransfer.files];
  if (files.length) return addFiles(files);
  const text = e.dataTransfer.getData("text/uri-list") || e.dataTransfer.getData("text/plain");
  for (const l of (text || "").split(/\s+/).filter(Boolean)) await addMagnet(l);
});

// Ctrl+V с magnet-ссылкой в любом месте окна
document.addEventListener("paste", async (e) => {
  if (e.target.closest("input, textarea")) return;
  const text = e.clipboardData.getData("text");
  const links = (text || "").split(/\s+/).map(toMagnet).filter(Boolean);
  if (!links.length) return;
  e.preventDefault();
  for (const l of links) await addMagnet(l);
});

// ---------- окно подтверждения ----------

function showPending() {
  if (S.pendingShown || !S.pending.length || $("pendOverlay").classList.contains("show")) return;
  const p = S.pending[0];
  S.pendingShown = p.pid;
  $("pendTitle").textContent = p.name || "Новая раздача";
  $("pendMeta").innerHTML = [
    p.has_metadata ? `${ico("box")} ${fmtSize(p.size)}` : `${ico("magnet")} magnet-ссылка: список файлов придет после подключения`,
    p.files.length ? `${ico("layers")} файлов: ${p.files.length}` : "",
    p.private ? `${ico("flag")} приватная раздача` : "",
    p.comment ? esc(p.comment).slice(0, 120) : "",
  ].filter(Boolean).map((x) => `<span>${x}</span>`).join("");
  $("pendPath").value = S.settings.save_path_effective || "";
  $("pendStart").checked = !S.settings.start_paused;
  $("pendSeq").checked = !!S.settings.sequential_default;
  $("pendFilesField").style.display = p.files.length > 1 ? "" : "none";
  $("pendFiles").innerHTML = p.files.map((f) => `<tr><td><input type="checkbox" data-i="${f.index}" checked></td>
    <td class="name" title="${esc(f.path)}">${esc(f.path)}</td><td class="num">${fmtSize(f.size)}</td></tr>`).join("");
  updatePendSel();
  openOverlay("pendOverlay");
}

function updatePendSel() {
  const p = S.pending.find((x) => x.pid === S.pendingShown);
  if (!p || p.files.length < 2) { $("pendSel").textContent = ""; return; }
  let n = 0, size = 0;
  $("pendFiles").querySelectorAll("input").forEach((c) => {
    if (c.checked) { n++; size += p.files.find((f) => f.index === +c.dataset.i).size; }
  });
  $("pendSel").textContent = `Выбрано ${n} из ${p.files.length}, ${fmtSize(size)}`;
}
$("pendFiles").addEventListener("change", updatePendSel);
$("pendAll").onclick = () => { $("pendFiles").querySelectorAll("input").forEach((c) => { c.checked = true; }); updatePendSel(); };
$("pendNone").onclick = () => { $("pendFiles").querySelectorAll("input").forEach((c) => { c.checked = false; }); updatePendSel(); };
$("pendBrowse").onclick = async () => {
  const r = await run(() => api("pick_folder", { initial: $("pendPath").value }));
  if (r && r.path) $("pendPath").value = r.path;
};

function finishPending() {
  S.pending = S.pending.filter((x) => x.pid !== S.pendingShown);
  S.pendingShown = null;
  closeOverlay("pendOverlay");
  setTimeout(showPending, 150);
}
$("pendOk").onclick = async () => {
  const pid = S.pendingShown;
  const priorities = {};
  $("pendFiles").querySelectorAll("input").forEach((c) => { priorities[c.dataset.i] = c.checked ? 4 : 0; });
  if (Object.keys(priorities).length && !Object.values(priorities).some(Boolean)) return toast("Не выбрано ни одного файла", "err");
  const r = await run(() => api(`pending/${pid}/confirm`, {
    save_path: $("pendPath").value.trim(), paused: !$("pendStart").checked, sequential: $("pendSeq").checked, priorities,
  }));
  finishPending();
  if (r) {
    toast(`Добавлено: ${r.name}`, "ok");
    setSelection([r.id], r.id);
  }
};
const cancelPending = async () => {
  const pid = S.pendingShown;
  finishPending();
  await api(`pending/${pid}/cancel`, {}).catch(() => {});
};
$("pendCancel").onclick = cancelPending;
$("pendX").innerHTML = ico("x");
$("pendX").onclick = cancelPending;

// ---------- модальные окна ----------

function openOverlay(id) { $(id).classList.add("show"); }
function closeOverlay(id) { $(id).classList.remove("show"); }
document.querySelectorAll("[data-close]").forEach((b) => {
  if (b.classList.contains("icon")) b.innerHTML = ico("x");
  b.addEventListener("click", () => closeOverlay(b.closest(".overlay").id));
});
document.querySelectorAll(".overlay").forEach((o) => o.addEventListener("mousedown", (e) => {
  if (e.target === o && o.id !== "pendOverlay") closeOverlay(o.id);
}));

function showText(title, text) {
  $("textTitle").textContent = title;
  $("textBody").textContent = text;
  openOverlay("textOverlay");
}

// ---------- настройки ----------

const SET_SECTIONS = [
  ["main", "Основное", "gear"],
  ["speed", "Скорость", "zap"],
  ["net", "Сеть и поиск пиров", "radar"],
  ["windows", "Windows", "monitor"],
  ["about", "О программе", "info"],
];

function sw(key, title, hint, checked) {
  return `<div class="toggle"><div class="t"><b>${title}</b>${hint ? `<span>${hint}</span>` : ""}</div>
    <label class="switch"><input type="checkbox" data-key="${key}"${checked ? " checked" : ""}><i></i></label></div>`;
}
function num(key, title, hint, value, unit) {
  return `<div class="toggle"><div class="t"><b>${title}</b>${hint ? `<span>${hint}</span>` : ""}</div>
    <div class="row-inline"><input class="input num-input" type="number" min="0" data-key="${key}" value="${value}">${unit ? `<span class="muted">${unit}</span>` : ""}</div></div>`;
}
function sel(key, title, hint, value, options) {
  return `<div class="toggle"><div class="t"><b>${title}</b>${hint ? `<span>${hint}</span>` : ""}</div>
    <select class="select" data-key="${key}">${options.map(([v, n]) => `<option value="${v}"${v === value ? " selected" : ""}>${n}</option>`).join("")}</select></div>`;
}

function renderSettings(active) {
  const s = S.settings, ig = S.integration, u = S.update;
  $("setNav").innerHTML = SET_SECTIONS.map(([k, t, i]) => `<a data-s="${k}" class="${k === active ? "active" : ""}">${ico(i)}${t}</a>`).join("");
  const theme = s.theme || "dark";
  $("setBody").innerHTML = `
  <section data-s="main">
    <h4>Загрузки</h4>
    <div class="field"><label>Папка по умолчанию</label>
      <div class="row-inline"><input class="input" data-key="save_path" value="${esc(s.save_path)}" placeholder="${esc(s.save_path_effective)}" spellcheck="false">
      <button class="btn" id="setBrowse">Обзор</button><button class="btn icon" id="setOpenDir" title="Открыть папку">${ico("folder")}</button></div></div>
    ${sw("ask_on_add", "Спрашивать при добавлении", "Окно с выбором папки и файлов перед началом загрузки", s.ask_on_add)}
    ${sw("start_paused", "Добавлять на паузе", "", s.start_paused)}
    ${sw("sequential_default", "Качать по порядку", "Удобно для видео: можно смотреть, не дожидаясь конца", s.sequential_default)}
    ${sw("notify_finished", "Уведомлять о завершении", "Всплывающее уведомление Windows", s.notify_finished)}
    <h4>Оформление</h4>
    <div class="toggle"><div class="t"><b>Тема</b></div>
      <div class="seg" id="setTheme">${[["dark", "Темная", "moon"], ["light", "Светлая", "sun"], ["system", "Как в Windows", "monitor"]]
        .map(([v, n, i]) => `<button data-v="${v}" class="${theme === v ? "active" : ""}">${ico(i)}${n}</button>`).join("")}</div></div>
  </section>
  <section data-s="speed">
    <h4>Ограничения</h4>
    ${num("dl_limit", "Загрузка", "0 - без ограничений", s.dl_limit, "КБ/с")}
    ${num("ul_limit", "Отдача", "0 - без ограничений", s.ul_limit, "КБ/с")}
    <h4>Режим «черепаха»</h4>
    ${sw("alt_enabled", "Включен", "Включается кнопкой на панели или в трее", s.alt_enabled)}
    ${num("alt_dl_limit", "Загрузка", "", s.alt_dl_limit, "КБ/с")}
    ${num("alt_ul_limit", "Отдача", "", s.alt_ul_limit, "КБ/с")}
    <h4>Раздача</h4>
    ${num("seed_ratio_limit", "Остановить при рейтинге", "0 - раздавать без ограничений", s.seed_ratio_limit, "")}
    ${num("max_active_downloads", "Одновременных загрузок", "Медленные раздачи не занимают место в очереди", s.max_active_downloads, "")}
    ${num("max_active_seeds", "Одновременных раздач", "", s.max_active_seeds, "")}
  </section>
  <section data-s="net">
    <h4>Поиск сидов и пиров</h4>
    ${sw("peer_hunter", "Охотник за пирами", "Пока у раздачи мало пиров или нет скорости, она переобъявляется на всех трекерах и в DHT каждые 15 с - 5 мин", s.peer_hunter)}
    ${sw("extra_trackers", "Публичные трекеры", `Подмешивать живые открытые трекеры и ретрекер провайдера в открытые раздачи (сейчас ${S.session.trackers_pool || 0}). В приватные не добавляются`, s.extra_trackers)}
    ${sw("dht", "DHT", "Поиск пиров без трекеров через распределенную сеть", s.dht)}
    ${sw("pex", "PEX", "Пиры рассказывают друг другу о других пирах", s.pex)}
    ${sw("lsd", "Поиск в локальной сети (LSD)", "Самая быстрая отдача - с соседних компьютеров", s.lsd)}
    ${sw("upnp", "UPnP и NAT-PMP", "Автоматически открыть порт на роутере для входящих соединений", s.upnp)}
    <div class="field" style="margin-top:12px"><label>Свои трекеры (по одному на строку, добавляются во все открытые раздачи)</label>
      <textarea class="input" data-key="custom_trackers" spellcheck="false">${esc(s.custom_trackers)}</textarea></div>
    <h4>Подключение</h4>
    <div class="toggle"><div class="t"><b>Порт для входящих</b><span>TCP и UDP. Сейчас слушается ${S.session.port || "-"}</span></div>
      <div class="row-inline"><input class="input num-input" type="number" min="1024" max="65535" data-key="listen_port" value="${s.listen_port}">
      <button class="btn" id="setRandPort" title="Случайный порт">${ico("refresh")}</button></div></div>
    ${sel("encryption", "Шифрование", "Помогает, если провайдер режет торренты", s.encryption, [["prefer", "Разрешено"], ["require", "Обязательно"], ["off", "Выключено"]])}
    ${num("max_connections", "Максимум соединений", "", s.max_connections, "")}
    ${sel("identity", "Представляться трекерам как", "Некоторые трекеры пускают только известные клиенты", s.identity, [["native", "TarBitTorrent (libtorrent)"], ["qbittorrent", "qBittorrent 5"]])}
  </section>
  <section data-s="windows">
    <h4>Запуск</h4>
    ${ig.frozen ? sw("autostart", "Запускать вместе с Windows", "Программа стартует свернутой в трей", ig.autostart) : `<div class="muted">Автозапуск и ассоциации доступны в собранной программе (exe).</div>`}
    <h4>Ссылки и файлы</h4>
    <div class="toggle"><div class="t"><b>Magnet-ссылки</b><span>${ig.magnet ? "Открываются в TarBitTorrent" : "Открываются другой программой или ничем"}</span></div><span class="chip ${ig.magnet ? "ok" : ""}">${ig.magnet ? "да" : "нет"}</span></div>
    <div class="toggle"><div class="t"><b>Файлы .torrent</b><span>${ig.torrent ? "Открываются в TarBitTorrent" : "Открываются другой программой или ничем"}</span></div><span class="chip ${ig.torrent ? "ok" : ""}">${ig.torrent ? "да" : "нет"}</span></div>
    <div style="margin-top:12px"><button class="btn primary" id="setAssoc"${ig.frozen ? "" : " disabled"}>${ico("link")}Открывать magnet и .torrent в TarBitTorrent</button>
    <div class="muted" style="margin-top:8px">Если Windows уже запомнила другую программу, откроются «Приложения по умолчанию»: там выберите TarBitTorrent.</div></div>
  </section>
  <section data-s="about">
    <div style="display:flex;gap:16px;align-items:center;margin:14px 0">
      <img src="icon-128.png" alt="" style="width:72px">
      <div><div style="font-size:20px;font-weight:700">TarBit<span style="background:var(--grad);-webkit-background-clip:text;background-clip:text;color:transparent">Torrent</span></div>
      <div class="muted">Версия ${esc(S.version)} · движок libtorrent</div></div></div>
    <h4>Обновления</h4>
    ${sw("check_updates", "Проверять обновления автоматически", "При запуске и каждые 6 часов, с GitHub", s.check_updates)}
    <div class="toggle"><div class="t"><b>${u.available ? "Доступна версия " + esc(u.version) : esc(u.message || u.error || "Еще не проверялось")}</b>
      ${u.error ? `<span style="color:var(--err)">${esc(u.error)}</span>` : ""}</div>
      <div class="row-inline">${u.available ? `<button class="btn primary" id="setInstall"${u.can_install ? "" : " disabled"}>Обновить</button>` : ""}
      <button class="btn" id="setCheck">${ico("refresh")}Проверить</button></div></div>
  </section>`;
  document.querySelectorAll("#setBody section").forEach((x) => x.classList.toggle("active", x.dataset.s === active));
  bindSettings(active);
}

let setActive = "main";
function openSettings(section) {
  setActive = section || setActive;
  $("setSaved").textContent = "";
  renderSettings(setActive);
  openOverlay("setOverlay");
}

let saveTimer = null;
async function saveSetting(key, value) {
  try {
    if (key === "autostart") {
      await api("settings", { autostart: value });
      S.integration.autostart = value;
    } else {
      S.settings = await api("settings", { [key]: value });
    }
    $("setSaved").textContent = "Сохранено";
    clearTimeout(saveTimer);
    saveTimer = setTimeout(() => { $("setSaved").textContent = ""; }, 1500);
    if (key === "theme") applyTheme();
    renderNet();
    renderFooter();
  } catch (e) {
    toast(e.message, "err");
  }
}

function bindSettings() {
  $("setNav").onclick = (e) => {
    const a = e.target.closest("a");
    if (!a) return;
    setActive = a.dataset.s;
    document.querySelectorAll("#setNav a").forEach((x) => x.classList.toggle("active", x === a));
    document.querySelectorAll("#setBody section").forEach((x) => x.classList.toggle("active", x.dataset.s === setActive));
  };
  const body = $("setBody");
  body.querySelectorAll("[data-key]").forEach((el) => {
    const key = el.dataset.key;
    const ev = el.tagName === "TEXTAREA" || (el.tagName === "INPUT" && el.type !== "checkbox") ? "change" : "change";
    el.addEventListener(ev, () => {
      let v = el.type === "checkbox" ? el.checked : el.value;
      if (el.type === "number") v = key === "seed_ratio_limit" ? parseFloat(v || "0") : parseInt(v || "0", 10);
      saveSetting(key, v);
    });
  });
  const theme = $("setTheme");
  if (theme) theme.onclick = (e) => {
    const b = e.target.closest("button");
    if (!b) return;
    theme.querySelectorAll("button").forEach((x) => x.classList.toggle("active", x === b));
    saveSetting("theme", b.dataset.v);
  };
  const browse = $("setBrowse");
  if (browse) browse.onclick = async () => {
    const r = await run(() => api("pick_folder", { initial: S.settings.save_path_effective }));
    if (r && r.path) { body.querySelector('[data-key="save_path"]').value = r.path; saveSetting("save_path", r.path); }
  };
  const openDir = $("setOpenDir");
  if (openDir) openDir.onclick = () => run(() => api("open_path", { path: S.settings.save_path_effective }));
  const rp = $("setRandPort");
  if (rp) rp.onclick = () => {
    const p = 20000 + Math.floor(Math.random() * 40000);
    body.querySelector('[data-key="listen_port"]').value = p;
    saveSetting("listen_port", p);
  };
  const assoc = $("setAssoc");
  if (assoc) assoc.onclick = async () => {
    const r = await run(() => api("associate", {}));
    if (r) { S.integration = r; renderSettings("windows"); toast(r.magnet && r.torrent ? "Готово: magnet и .torrent открываются здесь" : "Выберите TarBitTorrent в открывшемся окне Windows", "ok"); }
  };
  const chk = $("setCheck");
  if (chk) chk.onclick = async () => {
    chk.disabled = true;
    const r = await run(() => api("update/check", {}));
    if (r) { S.update = r; renderSettings("about"); }
  };
  const inst = $("setInstall");
  if (inst) inst.onclick = installUpdate;
}

// ---------- обновление ----------

async function installUpdate() {
  closeOverlay("setOverlay");
  await run(() => api("update/install", {}));
}
$("bannerInstall").onclick = installUpdate;
$("bannerNotes").onclick = () => showText("Что нового в " + (S.update.version || ""), S.update.notes || "Описания нет");
$("fVer").onclick = () => openSettings("about");

function renderBanner() {
  const u = S.update;
  const show = u.available || u.busy;
  $("banner").classList.toggle("show", !!show);
  if (!show) return;
  $("bannerIco").innerHTML = ico(u.busy ? "refresh" : "zap");
  $("bannerText").innerHTML = u.busy ? esc(u.message || "Обновляю")
    : u.error ? `Обновление до ${esc(u.version)} не удалось: ${esc(u.error)}`
    : `Доступна новая версия <b>${esc(u.version)}</b>`;
  $("bannerProg").hidden = !u.busy;
  $("bannerProg").firstChild.style.width = Math.max(u.progress, 0) * 100 + "%";
  $("bannerInstall").disabled = !!u.busy || !u.can_install;
  $("bannerInstall").title = u.can_install ? "" : "Обновление работает в собранной программе";
}

// ---------- клавиатура ----------

document.addEventListener("keydown", (e) => {
  const inField = e.target.closest("input, textarea, select");
  if (e.key === "Escape") {
    hideMenu();
    const open = [...document.querySelectorAll(".overlay.show")].pop();
    if (open) (open.id === "pendOverlay" ? cancelPending() : closeOverlay(open.id));
    return;
  }
  if (document.querySelector(".overlay.show") || inField) {
    if (e.key === "Enter" && e.ctrlKey && $("addOverlay").classList.contains("show")) $("addGo").click();
    return;
  }
  if (e.ctrlKey && e.key.toLowerCase() === "a") {
    e.preventDefault();
    setSelection(visibleTorrents().map((t) => t.id));
  } else if (e.ctrlKey && e.key.toLowerCase() === "o") {
    e.preventDefault(); openAdd();
  } else if (e.ctrlKey && e.key.toLowerCase() === "f") {
    e.preventDefault(); $("search").focus();
  } else if (e.key === "Delete") {
    askDelete(S.selected);
  } else if (e.key === "Enter") {
    act(S.selected, "open_folder");
  } else if (e.key === " ") {
    e.preventDefault();
    const ts = [...S.selected].map((id) => S.byId.get(id)).filter(Boolean);
    if (ts.length) act(S.selected, ts.every((t) => t.status === "paused" || t.status === "completed") ? "resume" : "pause");
  } else if (e.key === "ArrowDown" || e.key === "ArrowUp") {
    e.preventDefault();
    const ids = visibleTorrents().map((t) => t.id);
    if (!ids.length) return;
    let i = ids.indexOf(S.focusId);
    i = e.key === "ArrowDown" ? Math.min(i + 1, ids.length - 1) : Math.max(i - 1, 0);
    S.anchor = ids[i];
    setSelection([ids[i]], ids[i]);
    rowCache.get(ids[i]).tr.scrollIntoView({ block: "nearest" });
  }
});

// ---------- опрос ----------

let failures = 0;
async function refresh() {
  try {
    const st = await api("state");
    failures = 0;
    S.torrents = st.torrents;
    S.byId = new Map(st.torrents.map((t) => [t.id, t]));
    S.session = st.session;
    const themeChanged = S.settings.theme !== st.settings.theme;
    S.settings = st.settings;
    S.update = st.update;
    S.integration = st.integration;
    S.version = st.version;
    for (const p of st.pending) if (!S.pending.find((x) => x.pid === p.pid)) S.pending.push(p);
    S.pending = S.pending.filter((x) => st.pending.find((p) => p.pid === x.pid));
    if (themeChanged) applyTheme();
    for (const id of [...S.selected]) if (!S.byId.has(id)) S.selected.delete(id);
    if (S.focusId && !S.byId.has(S.focusId)) S.focusId = null;
    renderList();
    renderFilters();
    renderNet();
    renderFooter();
    renderBanner();
    updateToolbar();
    showPending();
    renderDetails(false);
  } catch (e) {
    if (++failures === 3) toast("Нет связи с программой. Она закрыта?", "err", 8000);
  }
}

$("btnResume").innerHTML = ico("play");
$("btnPause").innerHTML = ico("pause");
$("btnDelete").innerHTML = ico("trash");
$("btnFolder").innerHTML = ico("folder");
$("btnSettings").innerHTML = ico("gear");
$("btnAlt").innerHTML = ico("turtle") + "<span>Черепаха</span>";
$("searchBox").insertAdjacentHTML("afterbegin", ico("search"));
buildHead();
applyTheme();
updateToolbar();
refresh();
setInterval(refresh, 1000);
window.addEventListener("resize", () => renderDetails(true));
