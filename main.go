package main

// TarBitTorrent - торрент-клиент для Windows: движок anacrolix/torrent, свое
// окно на WebView2, значок в трее, самообновление из релизов GitHub.

import (
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"fyne.io/systray"
)

const appName = "TarBitTorrent"

// appVersion задается при сборке: -ldflags "-X main.appVersion=0.1.0"
var appVersion = "dev"

//go:embed assets/icon.ico
var trayIcon []byte

var logger *log.Logger

func logf(format string, args ...any) {
	if logger != nil {
		logger.Printf(format, args...)
	}
}

func setupLogging(p Paths) {
	name := filepath.Join(p.Logs, "tarbit.log")
	if fi, err := os.Stat(name); err == nil && fi.Size() > 2<<20 {
		_ = os.Rename(name, name+".1")
	}
	f, err := os.OpenFile(name, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	logger = log.New(f, "", log.LstdFlags)
}

type App struct {
	paths     Paths
	cfg       *Config
	engine    *Engine
	server    *Server
	window    *Window
	updater   *Updater
	speed     *SpeedTest
	quitCh    chan struct{}
	quitOnce  sync.Once
	trayReady atomic.Bool
}

func main() {
	minimized := flag.Bool("minimized", false, "запуститься в трее без окна")
	afterUpdate := flag.Int("after-update", 0, "PID старой копии после обновления")
	flag.Parse()
	items := flag.Args()

	paths, err := newPaths()
	if err != nil {
		messageBox(appName, "Не удалось создать папку данных: "+err.Error())
		os.Exit(1)
	}
	if *afterUpdate > 0 {
		waitForExit(*afterUpdate, 30*time.Second)
		cleanupOldExe()
	}
	if !acquireInstance(paths) {
		// программа уже запущена: отдаем ей аргументы; вторую копию не запускаем никогда
		if !forwardToRunning(paths, items, len(items) == 0 && !*minimized, 30*time.Second) {
			messageBox(appName, appName+" уже запущен, но не отвечает. Закройте его через значок в трее или в диспетчере задач и запустите снова.")
		}
		return
	}
	setupLogging(paths)
	logf("%s %s запускается", appName, appVersion)

	app := &App{paths: paths, quitCh: make(chan struct{})}
	app.cfg = loadConfig(paths.file("settings.json"))
	app.updater = newUpdater()
	app.window = newWindow(app)
	app.speed = newSpeedTest(app)
	app.engine, err = newEngine(app.cfg, paths, app.notify)
	if err != nil {
		logf("Запуск не удался: %v", err)
		messageBox(appName, "Запуск не удался: "+err.Error())
		os.Exit(1)
	}
	app.server, err = newServer(app)
	if err != nil {
		messageBox(appName, "Не удалось открыть локальный порт для окна: "+err.Error())
		os.Exit(1)
	}
	app.server.Start()
	writeInstance(paths, app.server.Port(), app.server.token)
	// тестовые копии (TARBIT_HOME) в Windows не регистрируются: путь к ним временный
	if exe := exePath(); appVersion != "dev" && exe != "" && os.Getenv("TARBIT_HOME") == "" {
		if err := registerApp(exe); err != nil {
			logf("Регистрация в Windows: %v", err)
		}
		go ensureStartMenu(exe)
	}
	app.updater.startBackground(func() bool { return app.cfg.Bool("check_updates") })

	needWindow := app.handleArgs(items)
	if needWindow || !*minimized {
		app.window.Show()
	}
	app.runTray() // до выхода
	app.shutdown()
}

func (a *App) notify(title, text string) {
	if a.trayReady.Load() {
		notifyBalloon(title, text)
	}
}

// handleArgs добавляет раздачи из аргументов. true - есть ждущие подтверждения, нужно окно.
func (a *App) handleArgs(args []string) bool {
	show := false
	for _, arg := range args {
		var res M
		var err error
		switch {
		case strings.HasPrefix(strings.ToLower(arg), "magnet:"):
			res, err = a.server.Prepare(arg, nil, false)
		case strings.HasSuffix(strings.ToLower(arg), ".torrent"):
			var b []byte
			if b, err = os.ReadFile(arg); err == nil {
				res, err = a.server.Prepare("", b, false)
			}
		default:
			continue
		}
		if err != nil {
			logf("Не удалось добавить %.80s: %v", arg, err)
			a.notify("Не удалось добавить", err.Error())
			continue
		}
		if _, ok := res["pending"]; ok {
			show = true
		}
		if added, ok := res["added"].(AddResult); ok {
			verb := "Добавлено"
			if added.Duplicate {
				verb = "Уже есть"
			}
			a.notify(verb, added.Name)
		}
	}
	return show
}

func (a *App) openTorrentFolder(id string, fileIndex int) error {
	e := a.engine
	e.mu.Lock()
	it, ok := e.items[id]
	var root, save string
	var files []string
	if ok {
		root, files, save = e.rootPath(it), e.filePaths(it), it.SavePath
	}
	e.mu.Unlock()
	if !ok {
		return errNotFound
	}
	existing := func(p string) string {
		if !fileExists(p) && fileExists(p+".part") {
			return p + ".part"
		}
		return p
	}
	switch {
	case fileIndex >= 0 && fileIndex < len(files):
		return openInExplorer(existing(files[fileIndex]), true)
	case fileIndex >= 0:
		return errNotFound
	case len(files) == 1:
		return openInExplorer(existing(files[0]), true)
	case root != "" && fileExists(root):
		return openInExplorer(root, false)
	}
	return openInExplorer(save, false)
}

func (a *App) setAutostart(on bool) error {
	if appVersion == "dev" {
		return errors.New("автозапуск доступен в собранной программе")
	}
	return setAutostart(exePath(), on)
}

func (a *App) integrationState() M {
	m, t := associations()
	return M{"frozen": appVersion != "dev", "autostart": autostartEnabled(), "magnet": m, "torrent": t,
		"shortcut": fileExists(desktopShortcut()), "exe": exePath()}
}

// ensureStartMenu: ярлык в меню «Пуск» ставится сам, если его еще нет.
func ensureStartMenu(exe string) {
	if lnk := startMenuShortcut(); lnk != "" && !fileExists(lnk) {
		if err := createShortcut(lnk, exe); err != nil {
			logf("Ярлык в меню Пуск: %v", err)
		}
	}
}

func (a *App) desktopShortcut() (M, error) {
	if appVersion == "dev" {
		return nil, errors.New("доступно в собранной программе")
	}
	if err := createShortcut(desktopShortcut(), exePath()); err != nil {
		return nil, err
	}
	return a.integrationState(), nil
}

func (a *App) associate() (M, error) {
	if appVersion == "dev" {
		return nil, errors.New("доступно в собранной программе")
	}
	if err := claimDefaults(exePath()); err != nil {
		return nil, fmt.Errorf("реестр: %w", err)
	}
	if m, t := associations(); !m || !t {
		openDefaultApps()
	}
	return a.integrationState(), nil
}

func (a *App) onSettingsChanged(changed map[string]any) {
	if len(changed) > 0 {
		a.engine.applySettings(changed)
	}
}

// Quit закрывает окно и выходит из цикла трея; остальное доделывает shutdown.
func (a *App) Quit() {
	a.quitOnce.Do(func() {
		logf("Выход")
		close(a.quitCh)
		a.speed.Stop()
		a.window.Close()
		if a.trayReady.Load() {
			systray.Quit()
		}
	})
}

func (a *App) shutdown() {
	removeInstance(a.paths)
	a.server.Stop()
	a.engine.Shutdown()
}
