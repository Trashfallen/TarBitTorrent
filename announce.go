package main

// Опрос трекеров. Движок anacrolix сам трекеры не трогает (DisableTrackers):
// программа объявляется на каждом трекере раздачи, видит его ответ (сиды и пиры
// в рое, ошибки, интервал) и может переобъявиться раньше интервала, когда
// раздаче не хватает пиров (охотник в engine.go).

import (
	"context"
	"errors"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/tracker"
)

type trState struct {
	URL        string
	Tier       int
	Extra      bool // из общего списка публичных трекеров
	status     string
	message    string
	seeds      int
	peers      int
	downloaded int
	next       time.Time
	last       time.Time
	interval   time.Duration
	fails      int
	updating   bool
	started    bool // started уже отправлен в этой сессии
}

func (e *Engine) extraTrackers() []string {
	if !e.cfg.Bool("extra_trackers") {
		return nil
	}
	return e.pool.Get(e.cfg.Str("custom_trackers"))
}

func newTrState(u string, tier int, extra bool) *trState {
	return &trState{URL: u, Tier: tier, Extra: extra, status: "idle", seeds: -1, peers: -1, downloaded: -1, next: time.Now()}
}

// buildTrackersLocked: трекеры из ссылки или файла, свои трекеры пользователя и публичные.
func (e *Engine) buildTrackersLocked(it *Item, tiers [][]string) {
	it.trackers = nil
	for i, tier := range tiers {
		for _, u := range tier {
			e.addTrackerLocked(it, u, i, false)
		}
	}
	for _, u := range it.UserTrackers {
		e.addTrackerLocked(it, u, 0, false)
	}
	if !it.Private {
		e.addTrackersLocked(it, e.extraTrackers(), true)
	}
}

func validTracker(u string) bool {
	p, err := url.Parse(strings.TrimSpace(u))
	return err == nil && p.Host != "" && (p.Scheme == "udp" || p.Scheme == "http" || p.Scheme == "https")
}

func (e *Engine) addTrackerLocked(it *Item, u string, tier int, extra bool) bool {
	u = strings.TrimSpace(u)
	if !validTracker(u) {
		return false
	}
	k := trackerKey(u)
	for _, tr := range it.trackers {
		if trackerKey(tr.URL) == k {
			return false
		}
	}
	it.trackers = append(it.trackers, newTrState(u, tier, extra))
	return true
}

func (e *Engine) addTrackersLocked(it *Item, urls []string, extra bool) {
	top := 0
	for _, tr := range it.trackers {
		top = max(top, tr.Tier+1)
	}
	for _, u := range urls {
		e.addTrackerLocked(it, u, top, extra)
	}
}

func (e *Engine) dropExtraTrackersLocked(it *Item) {
	kept := it.trackers[:0]
	for _, tr := range it.trackers {
		if !tr.Extra {
			kept = append(kept, tr)
		}
	}
	it.trackers = kept
}

func (e *Engine) topUpAllTrackers() {
	extra := e.extraTrackers()
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, it := range e.items {
		if !it.Private {
			e.addTrackersLocked(it, extra, true)
		}
	}
}

// AddTrackers - свои трекеры пользователя для раздачи; опрашиваются сразу.
func (e *Engine) AddTrackers(id string, urls []string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	it, ok := e.items[id]
	if !ok {
		return errNotFound
	}
	added := 0
	for _, u := range urls {
		if e.addTrackerLocked(it, u, 0, false) {
			it.UserTrackers = append(it.UserTrackers, strings.TrimSpace(u))
			added++
		}
	}
	if added == 0 {
		return errors.New("нет новых трекеров (адрес должен начинаться с udp://, http:// или https://)")
	}
	e.dirty = true
	return nil
}

// trackersLoop раз в секунду запускает объявления, у которых подошло время.
func (e *Engine) trackersLoop() {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	refreshCheck := time.Time{}
	for {
		select {
		case <-e.stop:
			return
		case <-tick.C:
		}
		if time.Since(refreshCheck) > 10*time.Minute {
			refreshCheck = time.Now()
			if e.cfg.Bool("extra_trackers") && e.pool.Stale() {
				go func() {
					if e.pool.Refresh() {
						e.topUpAllTrackers()
					}
				}()
			}
		}
		now := time.Now()
		e.mu.Lock()
		for _, it := range e.items {
			if it.t == nil {
				continue
			}
			for _, tr := range it.trackers {
				if tr.updating || now.Before(tr.next) {
					continue
				}
				ev := tracker.None
				if !tr.started {
					ev = tracker.Started
				}
				tr.updating = true
				tr.status = "updating"
				go e.announce(it, it.t, tr, ev)
			}
		}
		e.mu.Unlock()
	}
}

func (e *Engine) announceRequestLocked(it *Item, t *torrent.Torrent, ev tracker.AnnounceEvent) tracker.AnnounceRequest {
	st := t.Stats()
	left := int64(-1)
	if it.HasMeta {
		left = max(it.Size-it.Done, 0)
	}
	return tracker.AnnounceRequest{
		InfoHash:   t.InfoHash(),
		PeerId:     e.cl.PeerID(),
		Downloaded: it.Downloaded + st.BytesReadUsefulData.Int64(),
		Uploaded:   it.Uploaded + st.BytesWrittenData.Int64(),
		Left:       left,
		Event:      ev,
		Key:        e.key,
		NumWant:    200,
		Port:       uint16(e.cl.LocalPort()),
	}
}

func (e *Engine) announce(it *Item, t *torrent.Torrent, tr *trState, ev tracker.AnnounceEvent) {
	select {
	case e.sem <- struct{}{}:
	case <-e.stop:
		return
	}
	defer func() { <-e.sem }()
	_, ua, _ := e.identity()
	e.mu.Lock()
	req := e.announceRequestLocked(it, t, ev)
	e.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	res, err := tracker.Announce{TrackerUrl: tr.URL, Request: req, UserAgent: ua, Context: ctx}.Do()

	e.mu.Lock()
	defer e.mu.Unlock()
	tr.updating = false
	tr.last = time.Now()
	if err != nil {
		tr.fails++
		tr.status = "error"
		tr.message = trackerError(err)
		backoff := time.Minute << min(tr.fails-1, 5) // 1, 2, 4 ... 32 минуты
		if isDNSError(err) {
			backoff = 30 * time.Minute
		}
		tr.next = time.Now().Add(backoff)
		return
	}
	tr.fails = 0
	tr.status = "working"
	tr.message = ""
	tr.started = true
	tr.seeds, tr.peers = int(res.Seeders), int(res.Leechers)
	tr.interval = time.Duration(res.Interval) * time.Second
	if tr.interval < time.Minute {
		tr.interval = time.Minute
	}
	if tr.interval > time.Hour {
		tr.interval = time.Hour
	}
	tr.next = time.Now().Add(tr.interval)
	if it.t != t || len(res.Peers) == 0 {
		return
	}
	peers := make([]torrent.PeerInfo, 0, len(res.Peers))
	for _, p := range res.Peers {
		if p.Port <= 0 || p.IP == nil {
			continue
		}
		peers = append(peers, torrent.PeerInfo{Addr: &net.TCPAddr{IP: p.IP, Port: p.Port}, Source: torrent.PeerSourceTracker})
	}
	go t.AddPeers(peers)
}

// announceStoppedLocked сообщает рабочим трекерам, что раздача остановлена.
func (e *Engine) announceStoppedLocked(it *Item) {
	if it.t == nil {
		return
	}
	_, ua, _ := e.identity()
	req := e.announceRequestLocked(it, it.t, tracker.Stopped)
	for _, tr := range it.trackers {
		if !tr.started {
			continue
		}
		tr.started = false
		go func(u string) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_, _ = tracker.Announce{TrackerUrl: u, Request: req, UserAgent: ua, Context: ctx}.Do()
		}(tr.URL)
	}
}

func (e *Engine) announceCompletedLocked(it *Item) {
	for _, tr := range it.trackers {
		if tr.started && !tr.updating {
			tr.updating = true
			go e.announce(it, it.t, tr, tracker.Completed)
		}
	}
}

func isDNSError(err error) bool {
	var de *net.DNSError
	return errors.As(err, &de)
}

func trackerError(err error) string {
	var de *net.DNSError
	switch {
	case errors.As(err, &de):
		return "сервер не найден"
	case errors.Is(err, context.DeadlineExceeded):
		return "нет ответа (таймаут)"
	}
	s := err.Error()
	switch {
	case strings.Contains(s, "connection refused"), strings.Contains(s, "actively refused"):
		return "соединение отклонено"
	case strings.Contains(s, "timeout"), strings.Contains(s, "deadline"):
		return "нет ответа (таймаут)"
	case strings.Contains(s, "connection reset"), strings.Contains(s, "forcibly closed"):
		return "соединение сброшено"
	}
	if len(s) > 160 {
		s = s[:160]
	}
	return s
}
