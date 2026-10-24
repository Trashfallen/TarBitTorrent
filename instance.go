package main

// Одна копия программы: вторая передает свои аргументы первой и выходит.
// Так работают двойной клик по .torrent и magnet-ссылки из браузера, когда
// программа уже запущена. Именованный мьютекс ловит одновременный запуск
// нескольких копий (выделили пять файлов и нажали Enter), instance.json
// хранит порт и токен интерфейса.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"time"

	"golang.org/x/sys/windows"
)

type instanceInfo struct {
	Port  int    `json:"port"`
	Token string `json:"token"`
	PID   int    `json:"pid"`
}

var instanceMutex windows.Handle

// acquireInstance: true - мы первая копия.
func acquireInstance() bool {
	name, _ := windows.UTF16PtrFromString(`Local\` + appName)
	h, err := windows.CreateMutex(nil, false, name)
	instanceMutex = h
	return err != windows.ERROR_ALREADY_EXISTS
}

func writeInstance(p Paths, port int, token string) {
	b, _ := json.Marshal(instanceInfo{Port: port, Token: token, PID: os.Getpid()})
	_ = writeAtomic(p.file("instance.json"), b)
}

func removeInstance(p Paths) { _ = os.Remove(p.file("instance.json")) }

// forwardToRunning отдает аргументы запущенной копии; ждет до wait, пока она поднимется.
func forwardToRunning(p Paths, args []string, show bool, wait time.Duration) bool {
	deadline := time.Now().Add(wait)
	client := &http.Client{Timeout: 5 * time.Second}
	post := func(info instanceInfo, path string, body any) error {
		b, _ := json.Marshal(body)
		req, _ := http.NewRequest("POST", "http://127.0.0.1:"+strconv.Itoa(info.Port)+"/api/"+path, bytes.NewReader(b))
		req.Header.Set("X-Token", info.Token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		resp.Body.Close()
		if resp.StatusCode != 200 {
			return os.ErrInvalid
		}
		return nil
	}
	for {
		var info instanceInfo
		b, err := os.ReadFile(p.file("instance.json"))
		if err == nil {
			err = json.Unmarshal(b, &info)
		}
		if err == nil {
			allowForeground(info.PID) // чтобы окно первой копии могло выйти вперед
			if len(args) > 0 {
				err = post(info, "open", map[string]any{"args": args})
			}
			if err == nil && (show || len(args) == 0) {
				err = post(info, "show", map[string]any{})
			}
			if err == nil {
				return true
			}
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(300 * time.Millisecond)
	}
}
