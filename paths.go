package main

// Где лежат данные программы. По умолчанию %APPDATA%\TarBitTorrent,
// для отладки можно переопределить TARBIT_HOME.

import (
	"os"
	"path/filepath"
	"strings"
)

type Paths struct {
	Data     string // корень
	Torrents string // копии .torrent (метаданные раздач)
	Webview  string // профиль WebView2
	Logs     string
}

func newPaths() (Paths, error) {
	root := os.Getenv("TARBIT_HOME")
	if root == "" {
		base := os.Getenv("APPDATA")
		if base == "" {
			base, _ = os.UserHomeDir()
		}
		root = filepath.Join(base, appName)
	}
	p := Paths{
		Data:     root,
		Torrents: filepath.Join(root, "torrents"),
		Webview:  filepath.Join(root, "webview"),
		Logs:     filepath.Join(root, "logs"),
	}
	for _, d := range []string{p.Data, p.Torrents, p.Webview, p.Logs} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return p, err
		}
	}
	return p, nil
}

func (p Paths) file(name string) string { return filepath.Join(p.Data, name) }

func defaultDownloads() string {
	home, _ := os.UserHomeDir()
	d := filepath.Join(home, "Downloads")
	if fileExists(d) {
		return d
	}
	return home
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// within - p лежит внутри base и не совпадает с ней.
func within(base, p string) bool {
	rel, err := filepath.Rel(base, p)
	if err != nil || rel == "." || filepath.IsAbs(rel) {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func exePath() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		return r
	}
	return exe
}

// writeAtomic пишет через временный файл, чтобы при сбое не остался обрезанный файл.
func writeAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
