package main

// Данные для интерфейса. Поля те же, что ждет ui/app.js.

import (
	"math"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
)

type M = map[string]any

func (e *Engine) statusLocked(it *Item, st *torrent.TorrentStats) string {
	switch {
	case it.err != "" && it.t == nil:
		return "error"
	case it.moving:
		return "moving"
	case it.checking:
		return "checking"
	case it.Paused && it.Finished:
		return "completed"
	case it.Paused:
		return "paused"
	case it.t == nil && it.Finished:
		return "queued_seed"
	case it.t == nil:
		return "queued"
	case !it.HasMeta:
		return "metadata"
	case it.Finished:
		return "seeding"
	case st != nil && st.ActivePeers == 0:
		return "searching"
	case it.dl < 512 && it.hunt.Active:
		return "searching"
	case it.dl < 512:
		return "stalled"
	}
	return "downloading"
}

func (e *Engine) swarmLocked(it *Item) (seeds, peers int) {
	seeds, peers = -1, -1
	for _, tr := range it.trackers {
		seeds = max(seeds, tr.seeds)
		peers = max(peers, tr.peers)
	}
	return
}

func (e *Engine) summaryLocked(it *Item) M {
	var st *torrent.TorrentStats
	read, written := int64(0), int64(0)
	if it.t != nil {
		s := it.t.Stats()
		st = &s
		read, written = s.BytesReadUsefulData.Int64(), s.BytesWrittenData.Int64()
	}
	uploaded, downloaded := it.Uploaded+written, it.Downloaded+read
	progress := 0.0
	if it.Size > 0 {
		progress = float64(it.Done) / float64(it.Size)
	} else if it.Finished {
		progress = 1
	}
	if it.Finished {
		progress = 1
	}
	eta := int64(0)
	if left := it.Size - it.Done; left > 0 {
		eta = -1
		if it.dl > 0 {
			eta = int64(float64(left) / it.dl)
		}
	}
	ratio := 0.0
	if d := max(downloaded, it.Done); d > 0 {
		ratio = float64(uploaded) / float64(d)
	}
	seedsTotal, peersTotal := e.swarmLocked(it)
	m := M{
		"id": it.ID, "name": it.Name, "size": it.Size, "done": it.Done, "progress": progress,
		"status": e.statusLocked(it, st), "dl": it.dl, "ul": it.ul,
		"seeds": 0, "peers": 0, "seeds_total": seedsTotal, "peers_total": peersTotal, "candidates": 0,
		"eta": eta, "ratio": ratio, "uploaded": uploaded, "downloaded": downloaded,
		"added": it.Added, "completed": it.Completed, "save_path": it.SavePath,
		"sequential": it.Sequential, "error": it.err, "has_metadata": it.HasMeta,
		"availability": -1, "hunting": it.hunt.Active, "auto_managed": !it.Force,
	}
	if st != nil {
		m["seeds"] = st.ConnectedSeeders
		m["peers"] = max(st.ActivePeers-st.ConnectedSeeders, 0)
		m["candidates"] = st.PendingPeers
	}
	return m
}

func (e *Engine) List() []M {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]M, 0, len(e.items))
	for _, it := range e.items {
		out = append(out, e.summaryLocked(it))
	}
	return out
}

func (e *Engine) item(id string) (*Item, error) {
	it, ok := e.items[id]
	if !ok {
		return nil, errNotFound
	}
	return it, nil
}

func (e *Engine) Details(id string) (M, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	it, err := e.item(id)
	if err != nil {
		return nil, err
	}
	m := e.summaryLocked(it)
	m["hash_v1"] = it.ID
	m["hash_v2"] = ""
	m["private"] = it.Private
	m["active_time"] = it.ActiveSecs
	m["seeding_time"] = it.SeedSecs
	m["last_seen_complete"] = 0
	m["hunt"] = it.hunt
	m["history"] = it.history
	m["magnet"] = it.Magnet
	m["pieces"], m["piece_count"], m["piece_size"] = 0, 0, 0
	m["connections"], m["wasted"] = 0, 0
	m["comment"], m["creator"], m["created"] = "", "", 0
	if mi, err := metainfo.LoadFromFile(e.metainfoPath(id)); err == nil {
		m["comment"], m["creator"], m["created"] = mi.Comment, mi.CreatedBy, mi.CreationDate
		if info, err := mi.UnmarshalInfo(); err == nil {
			m["piece_count"], m["piece_size"] = info.NumPieces(), info.PieceLength
			mag := mi.Magnet(nil, &info)
			mag.Trackers = nil
			for _, tr := range it.trackers {
				if !tr.Extra {
					mag.Trackers = append(mag.Trackers, tr.URL)
				}
			}
			m["magnet"] = mag.String()
		}
	}
	if t := it.t; t != nil && t.Info() != nil {
		st := t.Stats()
		m["pieces"] = st.PiecesComplete
		m["connections"] = st.ActivePeers
		m["wasted"] = st.PiecesDirtiedBad.Int64() * t.Info().PieceLength
		m["piece_map"] = pieceMap(t, 300)
	} else {
		m["piece_map"] = []int{}
	}
	if m["magnet"] == "" {
		m["magnet"] = "magnet:?xt=urn:btih:" + id
	}
	return m, nil
}

// pieceMap - полоса кусков: доля скачанного в каждой ячейке, 0..9.
func pieceMap(t *torrent.Torrent, cells int) []int {
	n := t.NumPieces()
	if n == 0 {
		return []int{}
	}
	have := make([]bool, n)
	i := 0
	for _, run := range t.PieceStateRuns() {
		for k := 0; k < run.Length && i < n; k++ {
			have[i] = run.Complete
			i++
		}
	}
	return cellsFromPieces(have, cells)
}

func cellsFromPieces(have []bool, cells int) []int {
	n := len(have)
	cells = min(cells, n)
	out := make([]int, cells)
	for c := range cells {
		a, b := c*n/cells, (c+1)*n/cells
		if b <= a {
			b = a + 1
		}
		cnt := 0
		for i := a; i < b; i++ {
			if have[i] {
				cnt++
			}
		}
		out[c] = (9*cnt + (b-a)/2) / (b - a)
	}
	return out
}

func (e *Engine) Files(id string) ([]M, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	it, err := e.item(id)
	if err != nil {
		return nil, err
	}
	out := []M{}
	var info *metainfo.Info
	if it.t != nil && it.t.Info() != nil {
		info = it.t.Info()
	} else if mi, err := metainfo.LoadFromFile(e.metainfoPath(id)); err == nil {
		if inf, err := mi.UnmarshalInfo(); err == nil {
			info = &inf
		}
	}
	if info == nil {
		return out, nil
	}
	for i, f := range infoFiles(info) {
		if f.pad {
			continue
		}
		var done int64
		if i < len(it.filesDone) {
			done = it.filesDone[i]
		} else if it.Finished {
			done = f.size
		}
		p, ok := it.Prio[i]
		if !ok {
			p = prioNormal
		}
		progress := 1.0
		if f.size > 0 {
			progress = float64(done) / float64(f.size)
		}
		out = append(out, M{"index": i, "path": f.path, "size": f.size, "done": done, "progress": progress, "priority": p})
	}
	return out, nil
}

var sourceNames = map[torrent.PeerSource]string{
	torrent.PeerSourceTracker:         "трекер",
	torrent.PeerSourceDhtGetPeers:     "DHT",
	torrent.PeerSourceDhtAnnouncePeer: "DHT",
	torrent.PeerSourcePex:             "PEX",
	torrent.PeerSourceIncoming:        "входящий",
	torrent.PeerSourceDirect:          "сохраненный",
	torrent.PeerSourceUtHolepunch:     "holepunch",
	lsdSourceLocal:                    "LSD",
}

func (e *Engine) Peers(id string) ([]M, error) {
	e.mu.Lock()
	it, err := e.item(id)
	var t *torrent.Torrent
	if err == nil {
		t = it.t
	}
	e.mu.Unlock()
	if err != nil {
		return nil, err
	}
	out := []M{}
	if t == nil {
		return out, nil
	}
	n := t.NumPieces()
	for _, pc := range t.PeerConns() {
		host, port, _ := net.SplitHostPort(pc.RemoteAddr.String())
		host = strings.TrimPrefix(host, "::ffff:")
		p, _ := strconv.Atoi(port)
		client, _ := pc.PeerClientName.Load().(string)
		st := pc.Stats()
		progress := 0.0
		if n > 0 {
			progress = float64(st.RemotePieceCount) / float64(n)
		}
		src := sourceNames[pc.Discovery]
		if src == "" {
			src = string(pc.Discovery)
		}
		out = append(out, M{
			"ip": host, "port": p, "client": client, "progress": progress,
			"dl": finite(st.DownloadRate), "ul": finite(st.LastWriteUploadRate),
			"downloaded": st.BytesReadUsefulData.Int64(), "uploaded": st.BytesWrittenData.Int64(),
			"seed": n > 0 && st.RemotePieceCount >= n, "encrypted": false,
			"incoming": pc.Discovery == torrent.PeerSourceIncoming, "sources": []string{src},
		})
	}
	sort.Slice(out, func(i, j int) bool {
		a := out[i]["dl"].(float64) + out[i]["ul"].(float64)
		b := out[j]["dl"].(float64) + out[j]["ul"].(float64)
		if a != b {
			return a > b
		}
		return out[i]["progress"].(float64) > out[j]["progress"].(float64)
	})
	return out, nil
}

// finite: у движка скорость пира бывает NaN (нулевой интервал), а JSON его не примет.
func finite(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
		return 0
	}
	return v
}

func (e *Engine) Trackers(id string) ([]M, error) {
	e.mu.Lock()
	it, err := e.item(id)
	if err != nil {
		e.mu.Unlock()
		return nil, err
	}
	rows := []M{}
	for _, tr := range it.trackers {
		next := 0
		if !tr.next.IsZero() && it.t != nil {
			next = max(int(time.Until(tr.next).Seconds()), 0)
		}
		status := tr.status
		if it.t == nil {
			status = "idle"
		}
		rows = append(rows, M{"url": tr.URL, "tier": tr.Tier, "status": status, "message": tr.message,
			"seeds": tr.seeds, "peers": tr.peers, "downloaded": tr.downloaded, "next": next, "fails": tr.fails})
	}
	t, private := it.t, it.Private
	e.mu.Unlock()

	counts := map[string]int{"DHT": 0, "PEX": 0, "LSD": 0}
	if t != nil {
		for _, pc := range t.PeerConns() {
			switch pc.Discovery {
			case torrent.PeerSourceDhtGetPeers, torrent.PeerSourceDhtAnnouncePeer:
				counts["DHT"]++
			case torrent.PeerSourcePex:
				counts["PEX"]++
			case lsdSourceLocal:
				counts["LSD"]++
			}
		}
	}
	enabled := map[string]bool{"DHT": e.cfg.Bool("dht") && !private, "PEX": e.cfg.Bool("pex"), "LSD": e.cfg.Bool("lsd") && !private}
	var pseudo []M
	for _, k := range []string{"DHT", "PEX", "LSD"} {
		st := "working"
		if !enabled[k] {
			st = "disabled"
		}
		pseudo = append(pseudo, M{"url": "** [" + k + "] **", "pseudo": true, "connected": counts[k], "status": st})
	}
	return append(pseudo, rows...), nil
}

func (e *Engine) SessionInfo() M {
	e.mu.Lock()
	defer e.mu.Unlock()
	var dl, ul float64
	peers := 0
	for _, it := range e.items {
		dl += it.dl
		ul += it.ul
		if it.t != nil {
			peers += it.t.Stats().ActivePeers
		}
	}
	port := 0
	if e.cl != nil {
		port = e.cl.LocalPort()
	}
	h := e.history
	if len(h) > 120 {
		h = h[len(h)-120:]
	}
	upnp := ""
	if e.incoming {
		upnp = "проброшен порт " + strconv.Itoa(port)
	}
	return M{
		"dl": dl, "ul": ul, "dht_nodes": e.dhtNodes, "peers": peers, "port": port,
		"incoming": e.incoming, "upnp": upnp, "ext_ip": "", "alt": e.cfg.Bool("alt_enabled"),
		"trackers_pool": len(e.extraTrackers()), "history": h,
	}
}
