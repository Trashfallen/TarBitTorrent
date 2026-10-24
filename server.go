package main

// Локальный HTTP-сервер для окна программы: отдает интерфейс (ui/, вшит в exe)
// и JSON API. Слушает только 127.0.0.1, все /api/* требуют токен (X-Token),
// чтобы сайты в браузере не могли управлять клиентом.

import (
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed ui
var uiFiles embed.FS

const maxBody = 64 << 20

type pendingItem struct {
	Preview
	PID     string `json:"pid"`
	Created int64  `json:"created"`
	Magnet  bool   `json:"magnet"`
	src     *source
}

type Server struct {
	app     *App
	token   string
	ln      net.Listener
	srv     *http.Server
	mu      sync.Mutex
	pending map[string]*pendingItem
}

func newServer(app *App) (*Server, error) {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	s := &Server{app: app, token: hex.EncodeToString(b), ln: ln, pending: map[string]*pendingItem{}}
	s.srv = &http.Server{Handler: s, ReadHeaderTimeout: 10 * time.Second}
	return s, nil
}

func (s *Server) Port() int     { return s.ln.Addr().(*net.TCPAddr).Port }
func (s *Server) URL() string   { return "http://127.0.0.1:" + strconv.Itoa(s.Port()) + "/#t=" + s.token }
func (s *Server) Start()        { go s.srv.Serve(s.ln) }
func (s *Server) Stop()         { _ = s.srv.Close() }

// Prepare разбирает раздачу: в окно подтверждения или сразу в загрузки.
func (s *Server) Prepare(magnet string, data []byte, forceDialog bool) (M, error) {
	src, err := parseSource(magnet, data)
	if err != nil {
		return nil, err
	}
	eng := s.app.engine
	if eng.Has(src.ih.HexString()) || !(s.app.cfg.Bool("ask_on_add") || forceDialog) {
		res, err := eng.Add(src, AddOptions{})
		if err != nil {
			return nil, err
		}
		return M{"added": res}, nil
	}
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	p := &pendingItem{Preview: src.preview(), PID: hex.EncodeToString(b), Created: time.Now().Unix(),
		Magnet: src.magnet != "", src: src}
	s.mu.Lock()
	s.pending[p.PID] = p
	s.mu.Unlock()
	return M{"pending": p}, nil
}

func (s *Server) pendingList() []*pendingItem {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []*pendingItem{}
	for _, p := range s.pending {
		out = append(out, p)
	}
	return out
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		s.api(w, r)
		return
	}
	s.static(w, r)
}

func (s *Server) static(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/")
	if name == "" {
		name = "index.html"
	}
	b, err := fs.ReadFile(uiFiles, "ui/"+name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	ct := map[string]string{
		".html": "text/html; charset=utf-8", ".js": "application/javascript; charset=utf-8",
		".css": "text/css; charset=utf-8", ".png": "image/png", ".svg": "image/svg+xml",
	}[name[strings.LastIndex(name, "."):]]
	if ct == "" {
		ct = "application/octet-stream"
	}
	h := w.Header()
	h.Set("Content-Type", ct)
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self'; connect-src 'self'")
	_, _ = w.Write(b)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		logf("JSON: %v", err)
		code, b = 500, []byte(`{"error":"внутренняя ошибка"}`)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_, _ = w.Write(b)
}

func (s *Server) api(w http.ResponseWriter, r *http.Request) {
	if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Token")), []byte(s.token)) != 1 {
		writeJSON(w, 403, M{"error": "нет доступа"})
		return
	}
	body := M{}
	if r.Method == http.MethodPost {
		b, err := io.ReadAll(io.LimitReader(r.Body, maxBody))
		if err == nil && len(b) > 0 {
			err = json.Unmarshal(b, &body)
		}
		if err != nil {
			writeJSON(w, 400, M{"error": "кривой запрос: " + err.Error()})
			return
		}
	}
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/"), "/"), "/")
	res, err := s.route(r.Method, parts, body)
	switch {
	case errors.Is(err, errNotFound):
		writeJSON(w, 404, M{"error": "не найдено: " + err.Error()})
	case err != nil:
		writeJSON(w, 400, M{"error": err.Error()})
	case res == nil:
		writeJSON(w, 200, M{"ok": true})
	default:
		writeJSON(w, 200, res)
	}
}

func str(m M, k string) string     { v, _ := m[k].(string); return v }
func boolPtr(m M, k string) *bool { v, ok := m[k].(bool); if !ok { return nil }; return &v }

func intMap(v any) map[int]int {
	out := map[int]int{}
	if m, ok := v.(map[string]any); ok {
		for k, x := range m {
			i, err := strconv.Atoi(k)
			f, ok := x.(float64)
			if err == nil && ok {
				out[i] = int(f)
			}
		}
	}
	return out
}

func (s *Server) route(method string, parts []string, body M) (any, error) {
	app, eng := s.app, s.app.engine
	head := parts[0]
	arg := func(i int) string {
		if i < len(parts) {
			return parts[i]
		}
		return ""
	}

	if method == http.MethodGet {
		switch head {
		case "ping":
			return M{"app": appName, "version": appVersion}, nil
		case "state":
			return M{"torrents": eng.List(), "session": eng.SessionInfo(), "pending": s.pendingList(),
				"update": app.updater.State(), "version": appVersion, "settings": app.cfg.Snapshot(),
				"integration": app.integrationState()}, nil
		case "settings":
			return app.cfg.Snapshot(), nil
		case "speedtest":
			return app.speed.State(), nil
		case "torrent":
			switch arg(2) {
			case "files":
				return eng.Files(arg(1))
			case "peers":
				return eng.Peers(arg(1))
			case "trackers":
				return eng.Trackers(arg(1))
			}
			return eng.Details(arg(1))
		}
		return nil, errNotFound
	}

	switch head {
	case "add":
		magnet := strings.TrimSpace(str(body, "magnet"))
		var data []byte
		if t := str(body, "torrent"); t != "" {
			b, err := base64.StdEncoding.DecodeString(t)
			if err != nil {
				return nil, errors.New("файл .torrent не прочитан")
			}
			data = b
		}
		if magnet == "" && len(data) == 0 {
			return nil, errors.New("нет magnet-ссылки или файла")
		}
		dialog, _ := body["dialog"].(bool)
		return s.Prepare(magnet, data, dialog)
	case "pending":
		s.mu.Lock()
		p, ok := s.pending[arg(1)]
		delete(s.pending, arg(1))
		s.mu.Unlock()
		if !ok {
			return nil, errNotFound
		}
		if arg(2) == "cancel" {
			return nil, nil
		}
		return eng.Add(p.src, AddOptions{SavePath: str(body, "save_path"), Paused: boolPtr(body, "paused"),
			Sequential: boolPtr(body, "sequential"), Prio: intMap(body["priorities"])})
	case "torrent":
		id := arg(1)
		switch arg(2) {
		case "action":
			act := str(body, "action")
			switch act {
			case "open_folder":
				return nil, app.openTorrentFolder(id, -1)
			case "open_file":
				idx, _ := body["index"].(float64)
				return nil, app.openTorrentFolder(id, int(idx))
			}
			return nil, eng.Action(id, act, body)
		case "files":
			return nil, eng.SetFilePriorities(id, intMap(body["priorities"]))
		case "trackers":
			var urls []string
			if l, ok := body["urls"].([]any); ok {
				for _, u := range l {
					if s, ok := u.(string); ok {
						urls = append(urls, s)
					}
				}
			}
			return nil, eng.AddTrackers(id, urls)
		}
	case "bulk":
		act := str(body, "action")
		var errs []string
		ids, _ := body["ids"].([]any)
		for _, x := range ids {
			id, _ := x.(string)
			var err error
			if act == "open_folder" {
				err = app.openTorrentFolder(id, -1)
			} else {
				err = eng.Action(id, act, body)
			}
			if err != nil {
				errs = append(errs, id+": "+err.Error())
			}
		}
		if len(errs) > 0 {
			return nil, errors.New(strings.Join(errs, "; "))
		}
		return nil, nil
	case "pause_all":
		eng.PauseAll()
		return nil, nil
	case "resume_all":
		eng.ResumeAll()
		return nil, nil
	case "alt_speed":
		on, _ := body["on"].(bool)
		eng.SetAltSpeed(on)
		return nil, nil
	case "settings":
		if v, ok := body["autostart"].(bool); ok {
			if err := app.setAutostart(v); err != nil {
				return nil, err
			}
			delete(body, "autostart")
		}
		changed := app.cfg.Update(body)
		app.onSettingsChanged(changed)
		return app.cfg.Snapshot(), nil
	case "pick_folder":
		initial := str(body, "initial")
		if initial == "" {
			initial = app.cfg.SavePath()
		}
		path, err := pickFolder(app.window.hwnd(), initial)
		return M{"path": path}, err
	case "open_path":
		p := str(body, "path")
		if p == "" {
			p = app.cfg.SavePath()
		}
		return nil, openInExplorer(p, false)
	case "update":
		switch arg(1) {
		case "check":
			app.updater.Check()
			return app.updater.State(), nil
		case "install":
			go app.installUpdate()
			return app.updater.State(), nil
		}
	case "associate":
		return app.associate()
	case "speedtest":
		switch arg(1) {
		case "start":
			throttle, ok := body["throttle"].(bool)
			return nil, app.speed.Start(throttle || !ok)
		case "stop":
			app.speed.Stop()
			return nil, nil
		}
	case "webwindow":
		if !openWebWindow(app, str(body, "site")) {
			return nil, errors.New("неизвестный сайт")
		}
		return nil, nil
	case "open":
		var args []string
		if l, ok := body["args"].([]any); ok {
			for _, a := range l {
				if s, ok := a.(string); ok {
					args = append(args, s)
				}
			}
		}
		if app.handleArgs(args) {
			go app.window.Show()
		}
		return nil, nil
	case "show":
		go app.window.Show()
		return nil, nil
	case "quit":
		go app.Quit()
		return nil, nil
	}
	return nil, errNotFound
}
