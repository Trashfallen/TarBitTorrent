package main

// Окно для внешних сайтов замера скорости (Яндекс Интернетометр, speedtest.net):
// тоже свое окно с WebView2, без браузера. Открываются только адреса из списка.

import (
	"runtime"
	"sync"

	"github.com/jchv/go-webview2"
)

var webSites = map[string]struct{ title, url string }{
	"yandex":    {"Яндекс Интернетометр - " + appName, "https://yandex.ru/internet"},
	"speedtest": {"Speedtest - " + appName, "https://www.speedtest.net/"},
}

var (
	webMu   sync.Mutex
	webOpen = map[string]uintptr{}
)

func openWebWindow(app *App, site string) bool {
	s, ok := webSites[site]
	if !ok {
		return false
	}
	webMu.Lock()
	if h, ok := webOpen[site]; ok {
		webMu.Unlock()
		if h != 0 {
			focusWindow(h)
		}
		return true
	}
	webOpen[site] = 0 // открывается
	webMu.Unlock()
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		defer func() {
			webMu.Lock()
			delete(webOpen, site)
			webMu.Unlock()
		}()
		w := webview2.NewWithOptions(webview2.WebViewOptions{
			DataPath:  app.paths.Webview,
			AutoFocus: true,
			WindowOptions: webview2.WindowOptions{
				Title: s.title, Width: 1100, Height: 820, IconId: 1, Center: true,
			},
		})
		if w == nil {
			openURL(s.url)
			return
		}
		h := uintptr(w.Window())
		setDarkTitle(h, app.cfg.Str("theme") != "light")
		webMu.Lock()
		webOpen[site] = h
		webMu.Unlock()
		w.Navigate(s.url)
		focusWindow(h)
		w.Run()
		w.Destroy()
	}()
	return true
}

func closeWebWindows() {
	webMu.Lock()
	defer webMu.Unlock()
	for _, h := range webOpen {
		if h != 0 {
			procPostMessageW.Call(h, wmClose, 0, 0)
		}
	}
}
