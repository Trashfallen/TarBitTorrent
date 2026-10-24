package main

// Поиск пиров в локальной сети (Local Service Discovery, BEP 14): программа
// раз в 5 минут и по запросу охотника рассылает по мультикасту, какие раздачи
// у нее есть, и слушает такие же сообщения соседей. Соседи по сети отдают
// быстрее всех.

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/anacrolix/torrent"
)

const (
	lsdGroup             = "239.192.152.143:6771"
	lsdSourceLocal       = torrent.PeerSource("L")
	lsdMinAnnounceGap    = 30 * time.Second
	lsdMaxHashesInPacket = 20
)

type LSD struct {
	conn   *net.UDPConn
	group  *net.UDPAddr
	cookie string
	onPeer func(infohash string, ip net.IP, port int)

	mu   sync.Mutex
	last map[string]time.Time
}

func newLSD(onPeer func(string, net.IP, int)) (*LSD, error) {
	group, err := net.ResolveUDPAddr("udp4", lsdGroup)
	if err != nil {
		return nil, err
	}
	conn, err := net.ListenMulticastUDP("udp4", nil, group)
	if err != nil {
		return nil, err
	}
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	l := &LSD{conn: conn, group: group, cookie: hex.EncodeToString(b), onPeer: onPeer, last: map[string]time.Time{}}
	go l.read()
	return l, nil
}

func (l *LSD) Close() { l.conn.Close() }

func (l *LSD) read() {
	buf := make([]byte, 2048)
	for {
		n, from, err := l.conn.ReadFromUDP(buf)
		if err != nil {
			return
		}
		port, hashes, cookie, ok := parseLSD(buf[:n])
		if !ok || cookie == l.cookie {
			continue
		}
		for _, ih := range hashes {
			l.onPeer(ih, from.IP, port)
		}
	}
}

func parseLSD(b []byte) (port int, hashes []string, cookie string, ok bool) {
	req, err := http.ReadRequest(bufio.NewReader(bytes.NewReader(b)))
	if err != nil || req.Method != "BT-SEARCH" {
		return 0, nil, "", false
	}
	port, err = strconv.Atoi(req.Header.Get("Port"))
	if err != nil || port <= 0 || port > 65535 {
		return 0, nil, "", false
	}
	for _, h := range req.Header.Values("Infohash") {
		h = strings.ToLower(strings.TrimSpace(h))
		if len(h) == 40 {
			hashes = append(hashes, h)
		}
	}
	return port, hashes, req.Header.Get("Cookie"), len(hashes) > 0
}

// Announce рассылает infohash'и (не чаще раза в 30 с для каждого).
func (l *LSD) Announce(hashes []string, port int) {
	l.mu.Lock()
	var fresh []string
	now := time.Now()
	for _, h := range hashes {
		if now.Sub(l.last[h]) >= lsdMinAnnounceGap {
			l.last[h] = now
			fresh = append(fresh, h)
		}
	}
	l.mu.Unlock()
	for len(fresh) > 0 {
		n := min(len(fresh), lsdMaxHashesInPacket)
		var sb strings.Builder
		fmt.Fprintf(&sb, "BT-SEARCH * HTTP/1.1\r\nHost: %s\r\nPort: %d\r\n", lsdGroup, port)
		for _, h := range fresh[:n] {
			fmt.Fprintf(&sb, "Infohash: %s\r\n", h)
		}
		fmt.Fprintf(&sb, "cookie: %s\r\n\r\n\r\n", l.cookie)
		_, _ = l.conn.WriteToUDP([]byte(sb.String()), l.group)
		fresh = fresh[n:]
	}
}

// ---------- связь с движком ----------

func (e *Engine) startLSD() {
	e.mu.Lock()
	old := e.lsd
	e.lsd = nil
	e.mu.Unlock()
	if old != nil {
		old.Close()
	}
	if !e.cfg.Bool("lsd") {
		return
	}
	l, err := newLSD(e.onLSDPeer)
	if err != nil {
		logf("Поиск в локальной сети недоступен: %v", err)
		return
	}
	e.mu.Lock()
	e.lsd = l
	e.mu.Unlock()
}

func (e *Engine) onLSDPeer(ih string, ip net.IP, port int) {
	e.mu.Lock()
	it, ok := e.items[ih]
	var t *torrent.Torrent
	if ok && !it.Private {
		t = it.t
	}
	e.mu.Unlock()
	if t != nil {
		t.AddPeers([]torrent.PeerInfo{{Addr: &net.TCPAddr{IP: ip, Port: port}, Source: lsdSourceLocal}})
	}
}

func (e *Engine) lsdAnnounce(it *Item) {
	if e.lsd != nil && it.t != nil && !it.Private && e.cl != nil {
		go e.lsd.Announce([]string{it.ID}, e.cl.LocalPort())
	}
}

func (e *Engine) lsdAnnounceAll() {
	e.mu.Lock()
	l := e.lsd
	var hashes []string
	for _, it := range e.items {
		if it.t != nil && !it.Private {
			hashes = append(hashes, it.ID)
		}
	}
	port := 0
	if e.cl != nil {
		port = e.cl.LocalPort()
	}
	e.mu.Unlock()
	if l != nil && len(hashes) > 0 {
		l.Announce(hashes, port)
	}
}
