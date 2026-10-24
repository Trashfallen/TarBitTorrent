package main

// Связь с Windows: автозапуск, magnet-ссылки и .torrent, проводник, выбор папки,
// уведомления. Все пишется в HKCU, права администратора не нужны.

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const (
	runKey      = `Software\Microsoft\Windows\CurrentVersion\Run`
	classesKey  = `Software\Classes`
	progTorrent = appName + ".torrent"
	progMagnet  = appName + ".magnet"
	capsKey     = `Software\` + appName + `\Capabilities`
)

func regSet(path, name, value string) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, path, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.SetStringValue(name, value)
}

func regGet(path, name string) string {
	k, err := registry.OpenKey(registry.CURRENT_USER, path, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer k.Close()
	v, _, _ := k.GetStringValue(name)
	return v
}

// ---------- автозапуск ----------

func autostartEnabled() bool { return regGet(runKey, appName) != "" }

func setAutostart(exe string, on bool) error {
	if on {
		return regSet(runKey, appName, `"`+exe+`" --minimized`)
	}
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return nil
	}
	defer k.Close()
	if err := k.DeleteValue(appName); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return err
	}
	return nil
}

// ---------- ассоциации ----------

// registerApp: ProgID и Capabilities - программа появляется в «Приложениях по
// умолчанию». Делается при каждом запуске (путь к exe остается актуальным),
// чужие ассоциации не трогаются.
func registerApp(exe string) error {
	cmd := `"` + exe + `" "%1"`
	icon := `"` + exe + `",0`
	steps := [][3]string{
		{classesKey + `\` + progTorrent, "", "Торрент-файл"},
		{classesKey + `\` + progTorrent + `\DefaultIcon`, "", icon},
		{classesKey + `\` + progTorrent + `\shell\open\command`, "", cmd},
		{classesKey + `\` + progMagnet, "", "Magnet-ссылка"},
		{classesKey + `\` + progMagnet, "URL Protocol", ""},
		{classesKey + `\` + progMagnet + `\DefaultIcon`, "", icon},
		{classesKey + `\` + progMagnet + `\shell\open\command`, "", cmd},
		{classesKey + `\.torrent\OpenWithProgids`, progTorrent, ""},
		{classesKey + `\Applications\` + filepath.Base(exe) + `\shell\open\command`, "", cmd},
		{capsKey, "ApplicationName", appName},
		{capsKey, "ApplicationDescription", "Торрент-клиент"},
		{capsKey + `\FileAssociations`, ".torrent", progTorrent},
		{capsKey + `\URLAssociations`, "magnet", progMagnet},
		{`Software\RegisteredApplications`, appName, capsKey},
	}
	for _, s := range steps {
		if err := regSet(s[0], s[1], s[2]); err != nil {
			return err
		}
	}
	return nil
}

// claimDefaults: обработчик по умолчанию там, где пользователь не выбирал явно.
func claimDefaults(exe string) error {
	if err := registerApp(exe); err != nil {
		return err
	}
	cmd := `"` + exe + `" "%1"`
	for _, s := range [][3]string{
		{classesKey + `\.torrent`, "", progTorrent},
		{classesKey + `\magnet`, "", "URL:Magnet"},
		{classesKey + `\magnet`, "URL Protocol", ""},
		{classesKey + `\magnet\DefaultIcon`, "", `"` + exe + `",0`},
		{classesKey + `\magnet\shell\open\command`, "", cmd},
	} {
		if err := regSet(s[0], s[1], s[2]); err != nil {
			return err
		}
	}
	const shcneAssocChanged = 0x08000000
	modShell32.NewProc("SHChangeNotify").Call(shcneAssocChanged, 0, 0, 0)
	return nil
}

func associations() (magnet, torrent bool) {
	m := regGet(`Software\Microsoft\Windows\Shell\Associations\UrlAssociations\magnet\UserChoice`, "ProgId")
	if m == "" {
		if strings.Contains(strings.ToLower(regGet(classesKey+`\magnet\shell\open\command`, "")), strings.ToLower(appName)) {
			m = progMagnet
		}
	}
	t := regGet(`Software\Microsoft\Windows\CurrentVersion\Explorer\FileExts\.torrent\UserChoice`, "ProgId")
	if t == "" {
		t = regGet(classesKey+`\.torrent`, "")
	}
	return m == progMagnet, t == progTorrent
}

func openDefaultApps() {
	openURL("ms-settings:defaultapps?registeredAppUser=" + appName)
}

// ---------- ярлыки ----------

func knownFolder(id *windows.KNOWNFOLDERID) string {
	p, err := windows.KnownFolderPath(id, 0)
	if err != nil {
		return ""
	}
	return p
}

func desktopShortcut() string {
	if d := knownFolder(windows.FOLDERID_Desktop); d != "" {
		return filepath.Join(d, appName+".lnk")
	}
	return ""
}

func startMenuShortcut() string {
	if d := knownFolder(windows.FOLDERID_Programs); d != "" {
		return filepath.Join(d, appName+".lnk")
	}
	return ""
}

// createShortcut делает .lnk через WScript.Shell (есть в любой Windows).
func createShortcut(lnk, target string) error {
	if lnk == "" {
		return errors.New("папка для ярлыка не найдена")
	}
	q := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
	script := "$s=(New-Object -ComObject WScript.Shell).CreateShortcut(" + q(lnk) + ");" +
		"$s.TargetPath=" + q(target) + ";$s.WorkingDirectory=" + q(filepath.Dir(target)) + ";" +
		"$s.IconLocation=" + q(target+",0") + ";$s.Description=" + q("Торрент-клиент") + ";$s.Save()"
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ярлык не создан: %v %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// ---------- проводник, ссылки ----------

func openURL(u string) {
	cmd := exec.Command("rundll32", "url.dll,FileProtocolHandler", u)
	_ = cmd.Start()
	go cmd.Wait()
}

func openInExplorer(path string, sel bool) error {
	path = filepath.Clean(path)
	if sel && fileExists(path) {
		cmd := exec.Command("explorer.exe")
		cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: `explorer.exe /select,"` + path + `"`}
		_ = cmd.Start()
		go cmd.Wait()
		return nil
	}
	for p := path; p != "" && p != filepath.Dir(p); p = filepath.Dir(p) {
		if fi, err := os.Stat(p); err == nil && fi.IsDir() {
			cmd := exec.Command("explorer.exe", p)
			_ = cmd.Start()
			go cmd.Wait()
			return nil
		}
	}
	return errors.New("папка не найдена: " + path)
}

// ---------- уведомления ----------

// notifyBalloon показывает уведомление от значка программы в трее
// (Windows 10/11 превращает его в обычное уведомление).
func notifyBalloon(title, text string) {
	h := findOwnWindow("SystrayClass")
	if h == 0 {
		return
	}
	type notifyIconData struct {
		Size                       uint32
		Wnd                        windows.Handle
		ID, Flags, CallbackMessage uint32
		Icon                       windows.Handle
		Tip                        [128]uint16
		State, StateMask           uint32
		Info                       [256]uint16
		Timeout, Version           uint32
		InfoTitle                  [64]uint16
		InfoFlags                  uint32
		GuidItem                   windows.GUID
		BalloonIcon                windows.Handle
	}
	const nimModify, nifInfo, niifUser, niifLarge = 1, 0x10, 4, 0x20
	nid := notifyIconData{Wnd: windows.Handle(h), ID: 100, Flags: nifInfo, InfoFlags: niifUser | niifLarge}
	nid.Size = uint32(unsafe.Sizeof(nid))
	copy(nid.Info[:len(nid.Info)-1], windows.StringToUTF16(truncRunes(text, 250)))
	copy(nid.InfoTitle[:len(nid.InfoTitle)-1], windows.StringToUTF16(truncRunes(title, 60)))
	modShell32.NewProc("Shell_NotifyIconW").Call(nimModify, uintptr(unsafe.Pointer(&nid)))
}

func truncRunes(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

// findOwnWindow ищет окно нашего процесса с данным классом (у Mejgorod такой же класс трея).
var (
	findMu    sync.Mutex
	findClass string
	findFound uintptr
	findCb    = windows.NewCallback(func(h, _ uintptr) uintptr {
		var pid uint32
		procGetWindowThreadProcessId.Call(h, uintptr(unsafe.Pointer(&pid)))
		if pid != uint32(os.Getpid()) {
			return 1
		}
		buf := make([]uint16, 64)
		procGetClassNameW.Call(h, uintptr(unsafe.Pointer(&buf[0])), 64)
		if windows.UTF16ToString(buf) == findClass {
			findFound = h
			return 0
		}
		return 1
	})
	procGetWindowThreadProcessId = modUser32.NewProc("GetWindowThreadProcessId")
	procGetClassNameW            = modUser32.NewProc("GetClassNameW")
	procEnumWindows              = modUser32.NewProc("EnumWindows")
)

func findOwnWindow(class string) uintptr {
	findMu.Lock()
	defer findMu.Unlock()
	findClass, findFound = class, 0
	procEnumWindows.Call(findCb, 0)
	return findFound
}

func allowForeground(pid int) {
	procAllowSetForeground.Call(uintptr(pid))
}
