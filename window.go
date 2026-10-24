package main

// Окно программы: свое окно Windows с WebView2 внутри (как у Discord или VS Code).
// Без браузера: ни вкладок, ни адресной строки, своя кнопка на панели задач.
// Крестик прячет окно в трей (если в настройках не выбрано иное), повторное
// открытие показывает то же окно мгновенно.

import (
	"encoding/json"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/jchv/go-webview2"
	"golang.org/x/sys/windows"
)

var (
	modUser32  = windows.NewLazySystemDLL("user32.dll")
	modDwmapi  = windows.NewLazySystemDLL("dwmapi.dll")
	modShell32 = windows.NewLazySystemDLL("shell32.dll")

	procShowWindow            = modUser32.NewProc("ShowWindow")
	procIsIconic              = modUser32.NewProc("IsIconic")
	procIsWindowVisible       = modUser32.NewProc("IsWindowVisible")
	procSetForegroundWindow   = modUser32.NewProc("SetForegroundWindow")
	procKeybdEvent            = modUser32.NewProc("keybd_event")
	procPostMessageW          = modUser32.NewProc("PostMessageW")
	procSetWindowLongPtrW     = modUser32.NewProc("SetWindowLongPtrW")
	procCallWindowProcW       = modUser32.NewProc("CallWindowProcW")
	procSetWindowTextW        = modUser32.NewProc("SetWindowTextW")
	procGetWindowPlacement    = modUser32.NewProc("GetWindowPlacement")
	procSetWindowPlacement    = modUser32.NewProc("SetWindowPlacement")
	procMessageBoxW           = modUser32.NewProc("MessageBoxW")
	procAllowSetForeground    = modUser32.NewProc("AllowSetForegroundWindow")
	procDwmSetWindowAttribute = modDwmapi.NewProc("DwmSetWindowAttribute")
)

const (
	wmClose         = 0x0010
	swHide          = 0
	swShow          = 5
	swRestore       = 9
	gwlpWndProc     = ^uintptr(3) // -4
	dwmaDarkMode    = 20
	dwmaDarkModeOld = 19
)

type point struct{ X, Y int32 }
type rect struct{ Left, Top, Right, Bottom int32 }

type windowPlacement struct {
	Length           uint32
	Flags            uint32
	ShowCmd          uint32
	MinPosition      point
	MaxPosition      point
	NormalPosition   rect
}

type Window struct {
	app      *App
	mu       sync.Mutex
	w        webview2.WebView
	h        uintptr
	opening  bool
	closed   chan struct{}
	quitting atomic.Bool
	origProc uintptr
	hinted   bool
}

var (
	theWindow  *Window
	wndProcCb  = windows.NewCallback(subclassProc)
)

func newWindow(app *App) *Window {
	theWindow = &Window{app: app}
	return theWindow
}

func (win *Window) hwnd() uintptr {
	win.mu.Lock()
	defer win.mu.Unlock()
	return win.h
}

// Show открывает окно или выводит уже открытое вперед.
func (win *Window) Show() {
	win.mu.Lock()
	if win.h != 0 {
		h := win.h
		win.mu.Unlock()
		focusWindow(h)
		return
	}
	if win.opening || win.quitting.Load() {
		win.mu.Unlock()
		return
	}
	win.opening = true
	win.closed = make(chan struct{})
	win.mu.Unlock()
	go win.run()
}

func (win *Window) run() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(win.closed)
	w := webview2.NewWithOptions(webview2.WebViewOptions{
		DataPath:  win.app.paths.Webview,
		AutoFocus: true,
		WindowOptions: webview2.WindowOptions{
			Title: appName, Width: 1360, Height: 860, IconId: 1, Center: true,
		},
	})
	if w == nil {
		win.mu.Lock()
		win.opening = false
		win.mu.Unlock()
		logf("WebView2 недоступен")
		messageBox(appName, "Для окна программы нужен компонент Microsoft Edge WebView2 Runtime.\n"+
			"Он есть в Windows 11 и обычно в Windows 10. Скачать: https://go.microsoft.com/fwlink/p/?LinkId=2124703\n\n"+
			"Пока интерфейс откроется в браузере.")
		openURL(win.app.server.URL())
		return
	}
	h := uintptr(w.Window())
	win.restorePlacement(h)
	w.SetSize(960, 600, webview2.HintMin)
	setDarkTitle(h, win.app.cfg.Str("theme") != "light")
	_ = w.Bind("tbtTheme", func(theme string) { setDarkTitle(h, theme == "dark") })
	_ = w.Bind("tbtTitle", func(title string) { setWindowText(h, title) })
	win.origProc, _, _ = procSetWindowLongPtrW.Call(h, gwlpWndProc, wndProcCb)
	w.Navigate(win.app.server.URL())

	win.mu.Lock()
	win.w, win.h, win.opening = w, h, false
	win.mu.Unlock()
	focusWindow(h)
	w.Run()
	w.Destroy()
	win.mu.Lock()
	win.w, win.h = nil, 0
	win.mu.Unlock()
}

func subclassProc(hwnd, msg, wp, lp uintptr) uintptr {
	win := theWindow
	if msg == wmClose && win != nil && !win.quitting.Load() {
		win.savePlacement(hwnd)
		if win.app.cfg.Bool("close_to_tray") {
			procShowWindow.Call(hwnd, swHide)
			if !win.hinted {
				win.hinted = true
				go win.app.notify(appName+" работает в трее", "Раздачи продолжают качаться. Выход - через значок в трее.")
			}
			return 0
		}
		go win.app.Quit()
		return 0
	}
	r, _, _ := procCallWindowProcW.Call(win.origProc, hwnd, msg, wp, lp)
	return r
}

// Close закрывает окно при выходе из программы.
func (win *Window) Close() {
	win.quitting.Store(true)
	win.mu.Lock()
	h, closed := win.h, win.closed
	win.mu.Unlock()
	if h == 0 {
		return
	}
	win.savePlacement(h)
	procPostMessageW.Call(h, wmClose, 0, 0)
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
	}
}

func focusWindow(h uintptr) {
	if v, _, _ := procIsWindowVisible.Call(h); v == 0 {
		procShowWindow.Call(h, swShow)
	}
	if ic, _, _ := procIsIconic.Call(h); ic != 0 {
		procShowWindow.Call(h, swRestore)
	}
	// нажатие Alt снимает запрет Windows выводить окно вперед из фона
	procKeybdEvent.Call(0x12, 0, 0, 0)
	procSetForegroundWindow.Call(h)
	procKeybdEvent.Call(0x12, 0, 2, 0)
}

func setDarkTitle(h uintptr, dark bool) {
	var v int32
	if dark {
		v = 1
	}
	r, _, _ := procDwmSetWindowAttribute.Call(h, dwmaDarkMode, uintptr(unsafe.Pointer(&v)), 4)
	if r != 0 {
		procDwmSetWindowAttribute.Call(h, dwmaDarkModeOld, uintptr(unsafe.Pointer(&v)), 4)
	}
}

func setWindowText(h uintptr, s string) {
	p, _ := windows.UTF16PtrFromString(s)
	procSetWindowTextW.Call(h, uintptr(unsafe.Pointer(p)))
}

func messageBox(title, text string) {
	t, _ := windows.UTF16PtrFromString(title)
	m, _ := windows.UTF16PtrFromString(text)
	procMessageBoxW.Call(0, uintptr(unsafe.Pointer(m)), uintptr(unsafe.Pointer(t)), 0x40)
}

// ---------- положение окна между запусками ----------

func (win *Window) placementFile() string { return win.app.paths.file("window.json") }

func (win *Window) savePlacement(h uintptr) {
	var wp windowPlacement
	wp.Length = uint32(unsafe.Sizeof(wp))
	if r, _, _ := procGetWindowPlacement.Call(h, uintptr(unsafe.Pointer(&wp))); r == 0 {
		return
	}
	if wp.ShowCmd == 2 { // свернуто - открывать обычным
		wp.ShowCmd = 1
	}
	if b, err := json.Marshal(wp); err == nil {
		_ = writeAtomic(win.placementFile(), b)
	}
}

func (win *Window) restorePlacement(h uintptr) {
	b, err := os.ReadFile(win.placementFile())
	if err != nil {
		return
	}
	var wp windowPlacement
	if json.Unmarshal(b, &wp) != nil || wp.NormalPosition.Right-wp.NormalPosition.Left < 400 {
		return
	}
	wp.Length = uint32(unsafe.Sizeof(wp))
	if wp.ShowCmd != 3 {
		wp.ShowCmd = 1
	}
	procSetWindowPlacement.Call(h, uintptr(unsafe.Pointer(&wp)))
}
