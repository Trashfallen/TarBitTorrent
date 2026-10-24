package main

// Значок в трее: открыть окно, пауза/продолжение, «черепаха», выход.
// Цикл значка крутится в главном потоке до выхода из программы.

import (
	"fmt"
	"time"
	"unsafe"

	"fyne.io/systray"
	"golang.org/x/sys/windows"
)

var (
	procFindWindowW               = modUser32.NewProc("FindWindowW")
	procFindWindowExW             = modUser32.NewProc("FindWindowExW")
	procRegisterWindowMessageW    = modUser32.NewProc("RegisterWindowMessageW")
	procChangeWindowMessageFilter = modUser32.NewProc("ChangeWindowMessageFilter")
)

// taskbarReady - есть ли панель задач с областью уведомлений. При запуске из
// автозагрузки проводник нередко стартует позже программы, а systray после
// неудачного Shell_NotifyIcon молча остается без значка.
func taskbarReady() bool {
	cls, _ := windows.UTF16PtrFromString("Shell_TrayWnd")
	tray, _, _ := procFindWindowW.Call(uintptr(unsafe.Pointer(cls)), 0)
	if tray == 0 {
		return false
	}
	notify, _ := windows.UTF16PtrFromString("TrayNotifyWnd")
	n, _, _ := procFindWindowExW.Call(tray, 0, uintptr(unsafe.Pointer(notify)), 0)
	return n != 0
}

// allowTaskbarCreated: чтобы значок вернулся после перезапуска проводника.
func allowTaskbarCreated() {
	name, _ := windows.UTF16PtrFromString("TaskbarCreated")
	msg, _, _ := procRegisterWindowMessageW.Call(uintptr(unsafe.Pointer(name)))
	if msg != 0 {
		procChangeWindowMessageFilter.Call(msg, 1)
	}
}

func human(v float64) string {
	units := []string{"Б/с", "КБ/с", "МБ/с", "ГБ/с"}
	i := 0
	for v >= 1024 && i < len(units)-1 {
		v /= 1024
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%.0f %s", v, units[i])
	}
	return fmt.Sprintf("%.1f %s", v, units[i])
}

func (a *App) runTray() {
	allowTaskbarCreated()
	deadline := time.Now().Add(3 * time.Minute)
	for !taskbarReady() && time.Now().Before(deadline) {
		select {
		case <-a.quitCh:
			return
		case <-time.After(500 * time.Millisecond):
		}
	}
	systray.Run(func() {
		systray.SetIcon(trayIcon)
		systray.SetTooltip(appName)
		mOpen := systray.AddMenuItem("Открыть "+appName, "")
		systray.AddSeparator()
		mResume := systray.AddMenuItem("Возобновить все", "")
		mPause := systray.AddMenuItem("Приостановить все", "")
		mAlt := systray.AddMenuItemCheckbox("Ограничить скорость", "Режим «черепаха»", a.cfg.Bool("alt_enabled"))
		systray.AddSeparator()
		mQuit := systray.AddMenuItem("Выход", "Остановить раздачи и закрыть программу")
		systray.SetOnTapped(func() { go a.window.Show() })
		a.trayReady.Store(true)

		go func() {
			tick := time.NewTicker(2 * time.Second)
			defer tick.Stop()
			for {
				select {
				case <-mOpen.ClickedCh:
					go a.window.Show()
				case <-mResume.ClickedCh:
					a.engine.ResumeAll()
				case <-mPause.ClickedCh:
					a.engine.PauseAll()
				case <-mAlt.ClickedCh:
					a.engine.SetAltSpeed(!a.cfg.Bool("alt_enabled"))
				case <-mQuit.ClickedCh:
					go a.Quit()
				case <-tick.C:
					s := a.engine.SessionInfo()
					systray.SetTooltip(fmt.Sprintf("%s\n↓ %s   ↑ %s", appName, human(s["dl"].(float64)), human(s["ul"].(float64))))
					if a.cfg.Bool("alt_enabled") {
						mAlt.Check()
					} else {
						mAlt.Uncheck()
					}
				case <-a.quitCh:
					return
				}
			}
		}()
	}, func() {})
}
