package main

// Публичные трекеры, которые подмешиваются в открытые раздачи.
//
// Чем больше живых трекеров у раздачи, тем быстрее находятся сиды и пиры, особенно
// для старых magnet-ссылок без трекеров. Список берется из поддерживаемых
// сообществом источников и обновляется раз в 12 часов; без сети работает
// встроенный список. В приватные раздачи ничего не добавляется.

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

var trackerSources = []string{
	"https://raw.githubusercontent.com/ngosang/trackerslist/master/trackers_best.txt",
	"https://cdn.jsdelivr.net/gh/ngosang/trackerslist@master/trackers_best.txt",
	"https://newtrackon.com/api/stable",
}

var builtinTrackers = []string{
	"udp://tracker.opentrackr.org:1337/announce",
	"udp://open.demonii.com:1337/announce",
	"udp://open.stealth.si:80/announce",
	"udp://tracker.torrent.eu.org:451/announce",
	"udp://exodus.desync.com:6969/announce",
	"udp://explodie.org:6969/announce",
	"udp://tracker.theoks.net:6969/announce",
	"udp://tracker.qu.ax:6969/announce",
	"udp://tracker.dump.cl:6969/announce",
	"udp://tracker.0x7c0.com:6969/announce",
	"udp://tracker-udp.gbitt.info:80/announce",
	"udp://tracker.tiny-vps.com:6969/announce",
	"udp://opentracker.io:6969/announce",
	"udp://p4p.arenabg.com:1337/announce",
	"udp://tracker.moeking.me:6969/announce",
	"udp://tracker.cyberia.is:6969/announce",
	"udp://retracker01-msk-virt.corbina.net:80/announce",
	"http://tracker.opentrackr.org:1337/announce",
	"https://tracker.gbitt.info:443/announce",
	"http://open.acgnxtracker.com:80/announce",
	// локальные ретрекеры провайдеров: соседи по сети провайдера, самая быстрая отдача
	"http://retracker.local/announce",
}

const (
	trackersRefreshEvery = 12 * time.Hour
	maxExtraTrackers     = 60
)

func parseTrackerList(text string) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		u := strings.TrimSpace(line)
		if u == "" || strings.HasPrefix(u, "#") {
			continue
		}
		scheme, _, ok := strings.Cut(u, "://")
		if ok && (scheme == "udp" || scheme == "http" || scheme == "https") {
			out = append(out, u)
		}
	}
	return out
}

func trackerKey(u string) string { return strings.ToLower(strings.TrimRight(u, "/")) }

func mergeTrackers(lists ...[]string) []string {
	seen := map[string]bool{}
	var out []string
	for _, l := range lists {
		for _, u := range l {
			if k := trackerKey(u); !seen[k] {
				seen[k] = true
				out = append(out, u)
			}
		}
	}
	return out
}

type TrackerPool struct {
	cache   string
	mu      sync.Mutex
	fetched []string
	updated time.Time
}

type trackerCache struct {
	Updated  int64    `json:"updated"`
	Trackers []string `json:"trackers"`
}

func newTrackerPool(cache string) *TrackerPool {
	p := &TrackerPool{cache: cache}
	if b, err := os.ReadFile(cache); err == nil {
		var c trackerCache
		if json.Unmarshal(b, &c) == nil {
			p.fetched = parseTrackerList(strings.Join(c.Trackers, "\n"))
			p.updated = time.Unix(c.Updated, 0)
		}
	}
	return p
}

// Get: сначала свои трекеры пользователя, потом свежие из сети, потом встроенные.
func (p *TrackerPool) Get(custom string) []string {
	p.mu.Lock()
	fetched := append([]string(nil), p.fetched...)
	p.mu.Unlock()
	own := parseTrackerList(custom)
	all := mergeTrackers(own, fetched, builtinTrackers)
	if limit := maxExtraTrackers + len(own); len(all) > limit {
		all = all[:limit]
	}
	return all
}

func (p *TrackerPool) Stale() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return time.Since(p.updated) > trackersRefreshEvery
}

// Refresh скачивает свежие списки. true - что-то получили.
func (p *TrackerPool) Refresh() bool {
	client := &http.Client{Timeout: 15 * time.Second}
	var got [][]string
	for _, src := range trackerSources {
		req, _ := http.NewRequest("GET", src, nil)
		req.Header.Set("User-Agent", appName+"/"+appVersion)
		resp, err := client.Do(req)
		if err != nil {
			logf("Список трекеров %s недоступен: %v", src, err)
			continue
		}
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512<<10))
		resp.Body.Close()
		if resp.StatusCode == 200 {
			got = append(got, parseTrackerList(string(b)))
		}
	}
	fresh := mergeTrackers(got...)
	if len(fresh) == 0 {
		return false
	}
	p.mu.Lock()
	p.fetched, p.updated = fresh, time.Now()
	p.mu.Unlock()
	b, _ := json.MarshalIndent(trackerCache{Updated: time.Now().Unix(), Trackers: fresh}, "", " ")
	_ = writeAtomic(p.cache, b)
	logf("Список трекеров обновлен: %d шт.", len(fresh))
	return true
}
