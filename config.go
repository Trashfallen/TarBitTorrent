package main

// Настройки программы: settings.json в папке данных. Ключи те же, что видит
// интерфейс, поэтому хранятся как словарь с типами из значений по умолчанию.

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

var configDefaults = map[string]any{
	"save_path":            "",       // пусто - папка «Загрузки»
	"listen_port":          0.0,      // 0 - выбрать случайный при первом запуске
	"dl_limit":             0.0,      // КБ/с, 0 - без ограничений
	"ul_limit":             0.0,      //
	"alt_dl_limit":         1024.0,   // «черепаха»
	"alt_ul_limit":         256.0,    //
	"alt_enabled":          false,    //
	"max_connections":      100.0,    // на одну раздачу
	"max_active_downloads": 8.0,      //
	"max_active_seeds":     15.0,     //
	"extra_trackers":       true,     // подмешивать публичные трекеры в открытые раздачи
	"custom_trackers":      "",       // свои трекеры, по одному на строку
	"peer_hunter":          true,     // переобъявляться, пока мало пиров
	"encryption":           "prefer", // prefer | require | off
	"upnp":                 true,     //
	"lsd":                  true,     //
	"dht":                  true,     //
	"pex":                  true,     //
	"identity":             "native", // native | qbittorrent
	"ask_on_add":           true,     //
	"start_paused":         false,    //
	"sequential_default":   false,    //
	"seed_ratio_limit":     0.0,      // 0 - раздавать без ограничений
	"notify_finished":      true,     //
	"close_to_tray":        true,     // крестик прячет окно в трей
	"theme":                "dark",   // dark | light | system
	"check_updates":        true,     //
}

var configChoices = map[string][]string{
	"encryption": {"prefer", "require", "off"},
	"identity":   {"native", "qbittorrent"},
	"theme":      {"dark", "light", "system"},
}

// Ключи, для которых движок нужно пересоздать (все раздачи переподключатся).
var engineRestartKeys = map[string]bool{
	"listen_port": true, "encryption": true, "upnp": true, "dht": true, "pex": true, "identity": true,
}

type Config struct {
	path string
	mu   sync.Mutex
	data map[string]any
}

func loadConfig(path string) *Config {
	c := &Config{path: path, data: map[string]any{}}
	for k, v := range configDefaults {
		c.data[k] = v
	}
	if b, err := os.ReadFile(path); err == nil {
		var saved map[string]any
		if json.Unmarshal(b, &saved) == nil {
			for k, v := range saved {
				if cv, ok := coerce(k, v); ok {
					c.data[k] = cv
				}
			}
		}
	}
	if c.Int("listen_port") == 0 {
		c.data["listen_port"] = float64(20000 + rand.IntN(40000))
	}
	_ = c.save()
	return c
}

func (c *Config) get(k string) any {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.data[k]
}

func (c *Config) Bool(k string) bool      { v, _ := c.get(k).(bool); return v }
func (c *Config) Float(k string) float64  { v, _ := c.get(k).(float64); return v }
func (c *Config) Int(k string) int        { return int(c.Float(k)) }
func (c *Config) Str(k string) string     { v, _ := c.get(k).(string); return v }

func (c *Config) SavePath() string {
	if p := c.Str("save_path"); p != "" {
		return p
	}
	return defaultDownloads()
}

func (c *Config) Snapshot() map[string]any {
	c.mu.Lock()
	out := make(map[string]any, len(c.data)+1)
	for k, v := range c.data {
		out[k] = v
	}
	c.mu.Unlock()
	out["save_path_effective"] = c.SavePath()
	return out
}

// Update применяет известные ключи с проверкой типов и возвращает то, что поменялось.
func (c *Config) Update(changes map[string]any) map[string]any {
	changed := map[string]any{}
	c.mu.Lock()
	for k, v := range changes {
		cv, ok := coerce(k, v)
		if !ok || c.data[k] == cv {
			continue
		}
		c.data[k] = cv
		changed[k] = cv
	}
	c.mu.Unlock()
	if len(changed) > 0 {
		_ = c.save()
	}
	return changed
}

func (c *Config) save() error {
	c.mu.Lock()
	b, err := json.MarshalIndent(c.data, "", "  ")
	c.mu.Unlock()
	if err != nil {
		return err
	}
	return writeAtomic(c.path, b)
}

func coerce(key string, v any) (any, bool) {
	def, known := configDefaults[key]
	if !known {
		return nil, false
	}
	switch def.(type) {
	case bool:
		switch x := v.(type) {
		case bool:
			return x, true
		case float64:
			return x != 0, true
		}
		return nil, false
	case float64:
		var f float64
		switch x := v.(type) {
		case float64:
			f = x
		case string:
			p, err := strconv.ParseFloat(x, 64)
			if err != nil {
				return nil, false
			}
			f = p
		default:
			return nil, false
		}
		if f < 0 {
			f = 0
		}
		if key == "listen_port" && (f < 1024 || f > 65535) {
			return nil, false
		}
		if key != "seed_ratio_limit" {
			f = float64(int64(f))
		}
		return f, true
	case string:
		s := fmt.Sprint(v)
		if key == "save_path" && strings.TrimSpace(s) != "" {
			s = filepath.Clean(strings.TrimSpace(s))
		}
		if choices, ok := configChoices[key]; ok {
			for _, ch := range choices {
				if ch == s {
					return s, true
				}
			}
			return nil, false
		}
		return s, true
	}
	return nil, false
}
