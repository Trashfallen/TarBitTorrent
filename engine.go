package main

// Торрент-движок: клиент anacrolix/torrent, список раздач, очередь, сохранение.
//
// Как программа ищет сиды и пиры:
//   - все каналы сразу: трекеры, DHT, PEX, поиск в локальной сети (LSD),
//     входящие через UPnP/NAT-PMP, TCP и uTP;
//   - трекеры опрашивает сама программа (announce.go), а не движок: так видно
//     состояние каждого трекера и можно переобъявляться, не дожидаясь интервала;
//   - в открытые раздачи подмешиваются живые публичные трекеры (trackers.go);
//   - таблица узлов DHT и известные пиры каждой раздачи сохраняются между
//     запусками, поэтому после перезапуска поиск идет сразу;
//   - «охотник»: пока у раздачи мало пиров или нет данных, она переобъявляется
//     на всех трекерах и в локальной сети с нарастающим интервалом 15 с - 5 мин.
//
// Раздача на паузе или в очереди из движка убирается целиком (Drop) и не держит
// соединений; прогресс хранится в базе проверенных кусков и не теряется.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	alog "github.com/anacrolix/log"
	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
	"github.com/anacrolix/torrent/types"
	"golang.org/x/time/rate"
)

const (
	huntMinPeers   = 8                // меньше - ищем активнее
	huntNoData     = 60 * time.Second // столько без данных - тоже ищем
	savedPeersMax  = 300
	stateSaveEvery = time.Minute
)

var huntSteps = []time.Duration{15 * time.Second, 30 * time.Second, time.Minute, 2 * time.Minute, 5 * time.Minute}

// Приоритеты файлов в понятиях интерфейса.
const (
	prioSkip   = 0
	prioNormal = 4
	prioHigh   = 7
)

// Item - одна раздача. Экспортируемые поля сохраняются в state.json.
type Item struct {
	ID           string      `json:"id"`
	Magnet       string      `json:"magnet,omitempty"`
	Name         string      `json:"name"`
	SavePath     string      `json:"save_path"`
	Paused       bool        `json:"paused"`
	Force        bool        `json:"force,omitempty"`
	Sequential   bool        `json:"sequential,omitempty"`
	Prio         map[int]int `json:"prio,omitempty"`
	Added        int64       `json:"added"`
	Completed    int64       `json:"completed,omitempty"`
	Finished     bool        `json:"finished,omitempty"`
	Uploaded     int64       `json:"uploaded"`   // за прошлые сессии
	Downloaded   int64       `json:"downloaded"` //
	Size         int64       `json:"size"`       // нужные байты, последнее известное
	Done         int64       `json:"done"`       //
	Private      bool        `json:"private,omitempty"`
	HasMeta      bool        `json:"has_meta,omitempty"`
	Peers        []string    `json:"peers,omitempty"`
	UserTrackers []string    `json:"user_trackers,omitempty"`
	ActiveSecs   int64       `json:"active_secs,omitempty"`
	NeedCheck    bool        `json:"need_check,omitempty"` // проверить файлы на диске при первом запуске
	// Folder - папка раздачи внутри SavePath; "" - файлы прямо в SavePath.
	// nil - раздача добавлена до появления этой настройки: как раньше раскладывал
	// движок (папка с названием для многофайловых, без папки для одиночного файла).
	Folder   *string `json:"folder,omitempty"`
	SeedSecs int64   `json:"seed_secs,omitempty"`

	t            *torrent.Torrent
	trackers     []*trState
	lastRead     int64
	lastWritten  int64
	dl, ul       float64
	history      []([2]float64)
	hunt         huntState
	wasDownload  bool
	checking     bool
	moving       bool
	err          string
	everIncoming bool
	filesDone    []int64
	verifyCancel context.CancelFunc // идет проверка файлов: отменить перед Drop
	verifyDone   chan struct{}
}

type huntState struct {
	Active    bool `json:"active"`
	Step      int  `json:"step"`
	Announces int  `json:"announces"`
	last      time.Time
	sinceData time.Time
}

type Engine struct {
	cfg    *Config
	paths  Paths
	notify func(title, text string)
	pool   *TrackerPool

	mu      sync.Mutex
	items   map[string]*Item
	cl      *torrent.Client
	pc      storage.PieceCompletion
	dhts    []*dhtNode
	lsd     *LSD
	dlLim   *rate.Limiter
	ulLim   *rate.Limiter
	history [][2]float64
	dirty   bool
	key     int32
	stop    chan struct{}
	done    chan struct{}

	dhtNodes  int
	incoming  bool
	throttled atomic.Bool   // на время замера скорости
	sem       chan struct{} // одновременные запросы к трекерам
}

func newEngine(cfg *Config, paths Paths, notify func(string, string)) (*Engine, error) {
	pc, err := storage.NewBoltPieceCompletion(paths.Data)
	if err != nil {
		return nil, fmt.Errorf("база проверенных кусков: %w", err)
	}
	e := &Engine{
		cfg: cfg, paths: paths, notify: notify, pc: pc,
		pool:  newTrackerPool(paths.file("trackers.json")),
		items: map[string]*Item{},
		dlLim: rate.NewLimiter(rate.Inf, 4<<20),
		ulLim: rate.NewLimiter(rate.Inf, 4<<20),
		key:   int32(time.Now().UnixNano()),
		stop:  make(chan struct{}),
		done:  make(chan struct{}),
		sem:   make(chan struct{}, 48),
	}
	e.applyLimits()
	e.loadState()
	if err := e.startClient(); err != nil {
		return nil, err
	}
	e.startLSD()
	e.manageQueue() // запустить восстановленные раздачи
	go e.loop()
	go e.trackersLoop()
	return e, nil
}

// ---------- клиент ----------

func (e *Engine) identity() (bep20, ua, ext string) {
	if e.cfg.Str("identity") == "qbittorrent" {
		return "-qB5050-", "qBittorrent/5.0.5", "qBittorrent v5.0.5"
	}
	v := strings.Split(appVersion, ".")
	digit := func(i int) string {
		if i < len(v) && len(v[i]) > 0 {
			return v[i][len(v[i])-1:]
		}
		return "0"
	}
	return "-TB" + digit(0) + digit(1) + digit(2) + "0-", appName + "/" + appVersion, appName + " " + appVersion
}

func (e *Engine) startClient() error {
	c := torrent.NewDefaultClientConfig()
	c.DataDir = e.paths.Data
	c.DefaultStorage = e.storageFor(e.cfg.SavePath())
	c.Seed = true
	c.ListenPort = e.cfg.Int("listen_port")
	c.NoDHT = true // свои DHT-серверы ниже: не объявлять приватные раздачи
	c.DisablePEX = !e.cfg.Bool("pex")
	c.DisableTrackers = true // трекеры опрашиваем сами
	c.NoDefaultPortForwarding = !e.cfg.Bool("upnp")
	switch e.cfg.Str("encryption") {
	case "require":
		c.HeaderObfuscationPolicy = torrent.HeaderObfuscationPolicy{Preferred: true, RequirePreferred: true}
	case "off":
		c.HeaderObfuscationPolicy = torrent.HeaderObfuscationPolicy{}
	default:
		c.HeaderObfuscationPolicy = torrent.HeaderObfuscationPolicy{Preferred: true}
	}
	c.EstablishedConnsPerTorrent = max(e.cfg.Int("max_connections"), 10)
	c.HalfOpenConnsPerTorrent = 40
	c.TotalHalfOpenConns = 250
	c.TorrentPeersHighWater = 3000
	c.DialRateLimiter = rate.NewLimiter(40, 40)
	c.DownloadRateLimiter = e.dlLim
	c.UploadRateLimiter = e.ulLim
	c.Bep20, c.HTTPUserAgent, c.ExtendedHandshakeClientVersion = e.identity()
	c.DhtStartingNodes = e.dhtStartingNodes
	c.Logger = alog.Default.WithFilterLevel(alog.Critical)

	cl, err := torrent.NewClient(c)
	if err != nil {
		logf("Порт %d занят (%v), беру случайный", c.ListenPort, err)
		c.ListenPort = 0
		if cl, err = torrent.NewClient(c); err != nil {
			return fmt.Errorf("движок не запустился: %w", err)
		}
	}
	e.mu.Lock()
	e.cl = cl
	e.mu.Unlock()
	if e.cfg.Bool("dht") {
		e.startDHT(cl)
	}
	logf("Движок запущен, порт %d", cl.LocalPort())
	return nil
}

func (e *Engine) storageFor(dir string) storage.ClientImpl {
	return storage.NewFileOpts(storage.NewFileClientOpts{ClientBaseDir: dir, PieceCompletion: e.pc})
}

// itemStorage раскладывает файлы раздачи по ее папке (Folder).
func (e *Engine) itemStorage(it *Item) storage.ClientImpl {
	folder := it.Folder
	return storage.NewFileOpts(storage.NewFileClientOpts{
		ClientBaseDir:   it.SavePath,
		PieceCompletion: e.pc,
		FilePathMaker: func(o storage.FilePathMakerOpts) string {
			return filepath.Join(folderFor(folder, o.Info), relFilePath(o.Info, o.File))
		},
	})
}

// relFilePath - путь файла внутри раздачи, без ее корневой папки.
func relFilePath(info *metainfo.Info, f *metainfo.FileInfo) string {
	if p := f.BestPath(); len(p) > 0 {
		return filepath.Join(p...)
	}
	return info.BestName() // раздача из одного файла
}

func folderFor(folder *string, info *metainfo.Info) string {
	if folder != nil {
		return *folder
	}
	if info != nil && info.IsDir() {
		return info.BestName()
	}
	return ""
}

// sanitizeFolder убирает из имени папки то, что Windows не разрешает.
func sanitizeFolder(name string) string {
	name = strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`<>:"/\|?*`, r) {
			return '_'
		}
		return r
	}, name)
	name = strings.TrimRight(strings.TrimSpace(name), ". ")
	if name == "." || name == ".." {
		return ""
	}
	return name
}

// restart пересоздает движок после смены порта, шифрования и т.п.
func (e *Engine) restart() {
	e.mu.Lock()
	for _, it := range e.items {
		if it.t != nil {
			e.deactivateLocked(it, false)
		}
	}
	cl, dhts := e.cl, e.dhts
	e.dhts = nil
	e.mu.Unlock()
	e.saveDHTNodes(dhts)
	for _, d := range dhts {
		d.s.Close()
	}
	cl.Close()
	if err := e.startClient(); err != nil {
		logf("Перезапуск движка: %v", err)
	}
	e.manageQueue()
}

// throttle притормаживает раздачи до 16 КБ/с, пока идет замер скорости интернета.
func (e *Engine) throttle(on bool) {
	e.throttled.Store(on)
	e.applyLimits()
}

func (e *Engine) applyLimits() {
	set := func(l *rate.Limiter, kb int) {
		l.SetBurst(4 << 20)
		if kb <= 0 {
			l.SetLimit(rate.Inf)
			return
		}
		l.SetLimit(rate.Limit(kb * 1024))
	}
	if e.throttled.Load() {
		for _, l := range []*rate.Limiter{e.dlLim, e.ulLim} {
			l.SetBurst(32 << 10)
			l.SetLimit(16 << 10)
		}
		return
	}
	if e.cfg.Bool("alt_enabled") {
		set(e.dlLim, e.cfg.Int("alt_dl_limit"))
		set(e.ulLim, e.cfg.Int("alt_ul_limit"))
	} else {
		set(e.dlLim, e.cfg.Int("dl_limit"))
		set(e.ulLim, e.cfg.Int("ul_limit"))
	}
}

func (e *Engine) applySettings(changed map[string]any) {
	e.applyLimits()
	restart := false
	for k := range changed {
		if engineRestartKeys[k] {
			restart = true
		}
	}
	if _, ok := changed["max_connections"]; ok {
		e.mu.Lock()
		for _, it := range e.items {
			if it.t != nil {
				it.t.SetMaxEstablishedConns(max(e.cfg.Int("max_connections"), 10))
			}
		}
		e.mu.Unlock()
	}
	if _, ok := changed["lsd"]; ok {
		e.startLSD()
	}
	if changed["extra_trackers"] == true || changed["custom_trackers"] != nil {
		go func() {
			e.pool.Refresh()
			e.topUpAllTrackers()
		}()
	}
	if restart {
		go e.restart()
	}
	e.manageQueue()
}

// ---------- добавление ----------

// Preview - что внутри раздачи, для окна добавления.
type Preview struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	Size        int64         `json:"size"`
	Files       []previewFile `json:"files"`
	Private     bool          `json:"private"`
	HasMetadata bool          `json:"has_metadata"`
	Comment     string        `json:"comment,omitempty"`
}

type previewFile struct {
	Index int    `json:"index"`
	Path  string `json:"path"`
	Size  int64  `json:"size"`
}

type source struct {
	magnet string
	mi     *metainfo.MetaInfo
	info   *metainfo.Info
	ih     metainfo.Hash
	name   string
	trs    [][]string
}

func parseSource(magnet string, data []byte) (*source, error) {
	if magnet = strings.TrimSpace(magnet); magnet != "" {
		m, err := metainfo.ParseMagnetUri(magnet)
		if err != nil {
			return nil, fmt.Errorf("не magnet-ссылка: %w", err)
		}
		return &source{magnet: magnet, ih: m.InfoHash, name: m.DisplayName, trs: [][]string{m.Trackers}}, nil
	}
	mi, err := metainfo.Load(strings.NewReader(string(data)))
	if err != nil {
		return nil, fmt.Errorf("файл .torrent не читается: %w", err)
	}
	info, err := mi.UnmarshalInfo()
	if err != nil {
		return nil, fmt.Errorf("файл .torrent поврежден: %w", err)
	}
	return &source{mi: mi, info: &info, ih: mi.HashInfoBytes(), name: info.BestName(), trs: mi.UpvertedAnnounceList()}, nil
}

func (s *source) preview() Preview {
	p := Preview{ID: s.ih.HexString(), Name: s.name, Files: []previewFile{}}
	if s.info != nil {
		p.HasMetadata = true
		p.Private = s.info.Private != nil && *s.info.Private
		p.Comment = s.mi.Comment
		for i, f := range infoFiles(s.info) {
			if f.pad {
				continue
			}
			p.Files = append(p.Files, previewFile{Index: i, Path: f.path, Size: f.size})
			p.Size += f.size
		}
	}
	return p
}

type infoFile struct {
	path string
	size int64
	pad  bool
}

// infoFiles - файлы раздачи; path - внутри раздачи, без ее корневой папки.
func infoFiles(info *metainfo.Info) []infoFile {
	var out []infoFile
	for _, f := range info.UpvertedFiles() {
		out = append(out, infoFile{path: relFilePath(info, &f), size: f.Length, pad: strings.Contains(f.Attr, "p")})
	}
	return out
}

type AddResult struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Duplicate bool   `json:"duplicate"`
}

type AddOptions struct {
	SavePath   string
	Paused     *bool
	Sequential *bool
	Prio       map[int]int
	Folder     *string // nil - по настройке create_folder
}

func (e *Engine) Has(id string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	_, ok := e.items[id]
	return ok
}

func (e *Engine) Add(src *source, o AddOptions) (AddResult, error) {
	id := src.ih.HexString()
	e.mu.Lock()
	if it, ok := e.items[id]; ok {
		// уже есть: допишем новые трекеры из ссылки
		for _, tier := range src.trs {
			e.addTrackersLocked(it, tier, false)
		}
		e.mu.Unlock()
		return AddResult{ID: id, Name: it.Name, Duplicate: true}, nil
	}
	e.mu.Unlock()

	if src.mi != nil {
		f, err := os.Create(filepath.Join(e.paths.Torrents, id+".torrent"))
		if err != nil {
			return AddResult{}, err
		}
		err = src.mi.Write(f)
		f.Close()
		if err != nil {
			return AddResult{}, err
		}
	}
	save := o.SavePath
	if save == "" {
		save = e.cfg.SavePath()
	}
	if err := os.MkdirAll(save, 0o755); err != nil {
		return AddResult{}, fmt.Errorf("папка %s: %w", save, err)
	}
	paused := e.cfg.Bool("start_paused")
	if o.Paused != nil {
		paused = *o.Paused
	}
	seq := e.cfg.Bool("sequential_default")
	if o.Sequential != nil {
		seq = *o.Sequential
	}
	it := &Item{
		ID: id, Magnet: src.magnet, Name: src.name, SavePath: save, Paused: paused, Sequential: seq,
		Prio: o.Prio, Added: time.Now().Unix(),
		// файлы могли остаться на диске (скачаны раньше или другим клиентом), а база
		// проверенных кусков могла запомнить эту раздачу по старой папке: не верим ей
		NeedCheck: true,
	}
	if it.Name == "" {
		it.Name = id
	}
	switch {
	case o.Folder != nil:
		f := sanitizeFolder(*o.Folder)
		it.Folder = &f
	case !e.cfg.Bool("create_folder"):
		f := ""
		it.Folder = &f
	case src.name != "":
		f := sanitizeFolder(src.name)
		it.Folder = &f
	}
	// magnet без названия при create_folder: решится по метаданным (как раньше)
	if src.info != nil {
		it.HasMeta = true
		it.Private = src.info.Private != nil && *src.info.Private
		it.Size = wantedSize(src.info, it.Prio)
	}
	e.mu.Lock()
	e.items[id] = it
	e.buildTrackersLocked(it, src.trs)
	e.dirty = true
	e.mu.Unlock()
	e.manageQueue()
	e.saveState()
	logf("Добавлена раздача %s", id)
	return AddResult{ID: id, Name: it.Name}, nil
}

func wantedSize(info *metainfo.Info, prio map[int]int) int64 {
	var n int64
	for i, f := range infoFiles(info) {
		if f.pad {
			continue
		}
		if p, ok := prio[i]; ok && p == prioSkip {
			continue
		}
		n += f.size
	}
	return n
}

// ---------- активация ----------

func (e *Engine) metainfoPath(id string) string {
	return filepath.Join(e.paths.Torrents, id+".torrent")
}

func (e *Engine) activateLocked(it *Item) error {
	var spec *torrent.TorrentSpec
	if mi, err := metainfo.LoadFromFile(e.metainfoPath(it.ID)); err == nil {
		spec, err = torrent.TorrentSpecFromMetaInfoErr(mi)
		if err != nil {
			return err
		}
	} else if it.Magnet != "" {
		spec, err = torrent.TorrentSpecFromMagnetUri(it.Magnet)
		if err != nil {
			return err
		}
	} else {
		return errors.New("нет ни .torrent, ни magnet-ссылки")
	}
	spec.Storage = e.itemStorage(it)
	t, _, err := e.cl.AddTorrentSpec(spec)
	if err != nil {
		return err
	}
	t.SetMaxEstablishedConns(max(e.cfg.Int("max_connections"), 10))
	it.t = t
	it.lastRead, it.lastWritten = 0, 0
	it.hunt = huntState{sinceData: time.Now()}
	it.err = ""
	for _, tr := range it.trackers {
		tr.started, tr.next = false, time.Now()
	}
	if len(it.Peers) > 0 {
		t.AddPeers(peerInfos(it.Peers, torrent.PeerSourceDirect))
	}
	go e.afterInfo(it, t)
	return nil
}

func (e *Engine) afterInfo(it *Item, t *torrent.Torrent) {
	select {
	case <-t.GotInfo():
	case <-t.Closed():
		return
	}
	info := t.Info()
	if !fileExists(e.metainfoPath(it.ID)) {
		mi := t.Metainfo()
		if f, err := os.Create(e.metainfoPath(it.ID)); err == nil {
			_ = mi.Write(f)
			f.Close()
		}
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if it.t != t {
		return
	}
	it.HasMeta = true
	it.Name = info.BestName()
	it.Private = info.Private != nil && *info.Private
	if it.Private {
		e.dropExtraTrackersLocked(it)
	}
	e.applyPrioLocked(it)
	e.dirty = true
	if it.NeedCheck && !it.checking {
		e.verifyLocked(it, t)
	}
}

// verifyLocked пересчитывает хеши файлов на диске. Пока идет проверка, раздача
// ничего не отдает: база могла ошибочно считать куски скачанными.
func (e *Engine) verifyLocked(it *Item, t *torrent.Torrent) {
	it.checking = true
	t.DisallowDataUpload()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	it.verifyCancel, it.verifyDone = cancel, done
	go func() {
		defer close(done)
		err := t.VerifyDataContext(ctx)
		e.mu.Lock()
		defer e.mu.Unlock()
		if it.verifyDone == done {
			it.verifyCancel, it.verifyDone = nil, nil
		}
		if it.t != t {
			return
		}
		it.checking = false
		t.AllowDataUpload()
		if err != nil {
			if ctx.Err() == nil {
				it.err = "проверка: " + err.Error()
			}
			return
		}
		it.NeedCheck = false
		e.dirty = true
	}()
}

func (e *Engine) applyPrioLocked(it *Item) {
	if it.t == nil || it.t.Info() == nil {
		return
	}
	for i, f := range it.t.Files() {
		p, ok := it.Prio[i]
		if !ok {
			p = prioNormal
		}
		f.SetPriority(uiToPiecePrio(p))
	}
}

func uiToPiecePrio(p int) types.PiecePriority {
	switch {
	case p <= prioSkip:
		return types.PiecePriorityNone
	case p >= prioHigh:
		return types.PiecePriorityHigh
	}
	return types.PiecePriorityNormal
}

func (e *Engine) deactivateLocked(it *Item, announceStop bool) {
	t := it.t
	if t == nil {
		return
	}
	st := t.Stats()
	it.Uploaded += st.BytesWrittenData.Int64()
	it.Downloaded += st.BytesReadUsefulData.Int64()
	if peers := knownPeers(t); len(peers) > 0 {
		it.Peers = peers
	}
	if announceStop {
		e.announceStoppedLocked(it)
	}
	it.t = nil
	it.dl, it.ul = 0, 0
	it.hunt.Active = false
	e.dirty = true
	go dropTorrent(t, e.takeVerifyLocked(it))
}

// takeVerifyLocked забирает у раздачи идущую проверку файлов (чтобы отменить).
func (e *Engine) takeVerifyLocked(it *Item) (stop func()) {
	cancel, done := it.verifyCancel, it.verifyDone
	it.verifyCancel, it.verifyDone, it.checking = nil, nil, false
	return func() {
		if cancel == nil {
			return
		}
		cancel()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			logf("Проверка файлов не остановилась за 10 с")
		}
	}
}

// dropTorrent убирает раздачу из движка. Сначала останавливает ее проверку файлов:
// Drop посреди проверки может намертво заблокировать движок.
func dropTorrent(t *torrent.Torrent, stopVerify func()) {
	stopVerify()
	t.Drop()
	select {
	case <-t.Closed():
	case <-time.After(15 * time.Second):
		logf("Раздача %s не закрылась за 15 с", t.InfoHash().HexString())
	}
}

func knownPeers(t *torrent.Torrent) []string {
	var out []string
	seen := map[string]bool{}
	for _, pc := range t.PeerConns() {
		a := pc.RemoteAddr.String()
		if !seen[a] {
			seen[a] = true
			out = append(out, a)
		}
	}
	for _, p := range t.KnownSwarm() {
		if len(out) >= savedPeersMax {
			break
		}
		a := p.Addr.String()
		if !seen[a] {
			seen[a] = true
			out = append(out, a)
		}
	}
	return out
}

func peerInfos(addrs []string, src torrent.PeerSource) []torrent.PeerInfo {
	var out []torrent.PeerInfo
	for _, a := range addrs {
		host, port, err := net.SplitHostPort(a)
		if err != nil {
			continue
		}
		ip := net.ParseIP(host)
		var p int
		fmt.Sscan(port, &p)
		if ip == nil || p <= 0 || p > 65535 {
			continue
		}
		out = append(out, torrent.PeerInfo{Addr: &net.TCPAddr{IP: ip, Port: p}, Source: src})
	}
	return out
}

// manageQueue: какие раздачи должны работать прямо сейчас.
func (e *Engine) manageQueue() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.cl == nil {
		return
	}
	var downs, seeds []*Item
	for _, it := range e.items {
		if it.Paused || it.moving {
			if it.t != nil {
				e.deactivateLocked(it, true)
			}
			continue
		}
		if it.Finished {
			seeds = append(seeds, it)
		} else {
			downs = append(downs, it)
		}
	}
	byAdded := func(l []*Item) {
		sort.Slice(l, func(i, j int) bool {
			if l[i].Force != l[j].Force {
				return l[i].Force
			}
			return l[i].Added < l[j].Added
		})
	}
	byAdded(downs)
	byAdded(seeds)
	apply := func(l []*Item, limit int) {
		n := 0
		for _, it := range l {
			want := it.Force || limit <= 0 || n < limit
			if want && !it.Force {
				n++
			}
			switch {
			case want && it.t == nil:
				if err := e.activateLocked(it); err != nil {
					it.err = err.Error()
					logf("Раздача %s не запустилась: %v", it.ID, err)
				}
			case !want && it.t != nil:
				e.deactivateLocked(it, true)
			}
		}
	}
	apply(downs, e.cfg.Int("max_active_downloads"))
	apply(seeds, e.cfg.Int("max_active_seeds"))
}

// ---------- действия ----------

var errNotFound = errors.New("раздача не найдена")

func (e *Engine) Action(id, act string, body map[string]any) error {
	e.mu.Lock()
	it, ok := e.items[id]
	if !ok {
		e.mu.Unlock()
		return errNotFound
	}
	requeue := true
	switch act {
	case "pause":
		it.Paused, it.Force = true, false
	case "resume":
		it.Paused, it.Force = false, false
	case "force":
		it.Paused, it.Force = false, true
	case "recheck":
		requeue = false
		switch {
		case it.checking:
		case it.t != nil && it.t.Info() != nil:
			e.verifyLocked(it, it.t)
		default:
			// на паузе или без метаданных: проверится при следующем запуске
			it.NeedCheck = true
			it.Paused = false
			requeue = true
		}
	case "reannounce":
		requeue = false
		e.huntNowLocked(it)
	case "sequential":
		v, ok := body["value"].(bool)
		if !ok {
			v = !it.Sequential
		}
		it.Sequential = v
		if !v {
			e.applyPrioLocked(it)
		}
	case "queue_up", "queue_top":
		it.Added -= 1 << 20 // раньше добавленные идут первыми
	case "queue_down", "queue_bottom":
		it.Added += 1 << 20
	case "move":
		path, _ := body["path"].(string)
		if path == "" {
			e.mu.Unlock()
			return errors.New("не выбрана папка")
		}
		e.mu.Unlock()
		return e.move(it, path)
	case "remove":
		withData, _ := body["with_data"].(bool)
		e.mu.Unlock()
		return e.remove(id, withData)
	default:
		e.mu.Unlock()
		return fmt.Errorf("неизвестное действие %q", act)
	}
	e.dirty = true
	e.mu.Unlock()
	if requeue {
		e.manageQueue()
	}
	return nil
}

func (e *Engine) itemInfo(it *Item) *metainfo.Info {
	if it.t != nil && it.t.Info() != nil {
		return it.t.Info()
	}
	if mi, err := metainfo.LoadFromFile(e.metainfoPath(it.ID)); err == nil {
		if info, err := mi.UnmarshalInfo(); err == nil {
			return &info
		}
	}
	return nil
}

// rootPath - то, что занимает раздача в SavePath: ее папка или единственный файл.
// "" - файлы лежат прямо в SavePath (без папки), удалять и переносить их по одному.
func (e *Engine) rootPath(it *Item) string {
	info := e.itemInfo(it)
	if info == nil {
		return ""
	}
	if f := folderFor(it.Folder, info); f != "" {
		return filepath.Join(it.SavePath, f)
	}
	if !info.IsDir() {
		return filepath.Join(it.SavePath, info.BestName())
	}
	return ""
}

// filePaths - полные пути всех файлов раздачи (по индексам файлов, включая pad).
func (e *Engine) filePaths(it *Item) []string {
	info := e.itemInfo(it)
	if info == nil {
		return nil
	}
	folder := folderFor(it.Folder, info)
	var out []string
	for _, f := range info.UpvertedFiles() {
		out = append(out, filepath.Join(it.SavePath, folder, relFilePath(info, &f)))
	}
	return out
}

// removeEmptyDirs убирает опустевшие папки от dir вверх до stop (не включая).
func removeEmptyDirs(dir, stop string) {
	for dir != stop && len(dir) > len(stop) {
		if os.Remove(dir) != nil {
			return
		}
		dir = filepath.Dir(dir)
	}
}

func (e *Engine) remove(id string, withData bool) error {
	e.mu.Lock()
	it, ok := e.items[id]
	if !ok {
		e.mu.Unlock()
		return errNotFound
	}
	t := it.t
	if t != nil {
		e.announceStoppedLocked(it)
	}
	stopVerify := e.takeVerifyLocked(it)
	it.t = nil
	delete(e.items, id)
	e.dirty = true
	root := e.rootPath(it)
	files := e.filePaths(it)
	e.mu.Unlock()
	if t != nil {
		dropTorrent(t, stopVerify)
	}
	if withData && root == "" {
		// раздача без своей папки: удаляем только ее файлы
		for _, f := range files {
			_ = os.Remove(f)
			_ = os.Remove(f + ".part")
			removeEmptyDirs(filepath.Dir(f), it.SavePath)
		}
	}
	if withData && root != "" {
		// файлы могут быть еще открыты движком: даем ему секунду
		var err error
		for range 10 {
			if err = os.RemoveAll(root); err == nil {
				break
			}
			time.Sleep(200 * time.Millisecond)
		}
		_ = os.RemoveAll(root + ".part")
		if err != nil {
			logf("Не удалось удалить %s: %v", root, err)
		}
	}
	_ = os.Remove(e.metainfoPath(id))
	e.saveState()
	return nil
}

func (e *Engine) move(it *Item, dest string) error {
	e.mu.Lock()
	if it.moving {
		e.mu.Unlock()
		return errors.New("файлы уже перемещаются")
	}
	it.moving = true
	if it.t != nil {
		e.deactivateLocked(it, true)
	}
	e.mu.Unlock()
	e.mu.Lock()
	root, files, src := e.rootPath(it), e.filePaths(it), it.SavePath
	e.mu.Unlock()
	go func() {
		err := os.MkdirAll(dest, 0o755)
		time.Sleep(time.Second) // движок закрывает файлы
		switch {
		case err != nil:
		case root != "" && fileExists(root):
			err = moveTree(root, filepath.Join(dest, filepath.Base(root)))
		case root != "":
			if fileExists(root + ".part") {
				err = moveTree(root+".part", filepath.Join(dest, filepath.Base(root)+".part"))
			}
		default:
			for _, f := range files {
				rel, rerr := filepath.Rel(src, f)
				if rerr != nil {
					continue
				}
				for _, p := range []string{f, f + ".part"} {
					if fileExists(p) {
						if err = moveTree(p, filepath.Join(dest, rel)+strings.TrimPrefix(p, f)); err != nil {
							break
						}
					}
				}
				removeEmptyDirs(filepath.Dir(f), src)
			}
		}
		e.mu.Lock()
		it.moving = false
		if err != nil {
			it.err = "перемещение: " + err.Error()
		} else {
			it.SavePath = dest
		}
		e.dirty = true
		e.mu.Unlock()
		e.manageQueue()
		e.saveState()
	}()
	return nil
}

func (e *Engine) SetFilePriorities(id string, prios map[int]int) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	it, ok := e.items[id]
	if !ok {
		return errNotFound
	}
	if it.Prio == nil {
		it.Prio = map[int]int{}
	}
	for i, p := range prios {
		if p == prioNormal {
			delete(it.Prio, i)
		} else {
			it.Prio[i] = p
		}
	}
	e.applyPrioLocked(it)
	if wanted, done, ok := e.wantedLocked(it); ok {
		it.Size, it.Done = wanted, done
		if done < wanted {
			it.Finished = false
		}
	}
	e.dirty = true
	return nil
}

func (e *Engine) PauseAll() {
	e.mu.Lock()
	for _, it := range e.items {
		it.Paused, it.Force = true, false
	}
	e.dirty = true
	e.mu.Unlock()
	e.manageQueue()
}

func (e *Engine) ResumeAll() {
	e.mu.Lock()
	for _, it := range e.items {
		it.Paused = false
	}
	e.dirty = true
	e.mu.Unlock()
	e.manageQueue()
}

func (e *Engine) SetAltSpeed(on bool) {
	e.cfg.Update(map[string]any{"alt_enabled": on})
	e.applyLimits()
}

// ---------- фоновый цикл ----------

func (e *Engine) loop() {
	defer close(e.done)
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	lastSave, lastDHT, lastLSD := time.Now(), time.Now(), time.Time{}
	for {
		select {
		case <-e.stop:
			return
		case <-tick.C:
		}
		e.tick()
		if time.Since(lastSave) > stateSaveEvery {
			lastSave = time.Now()
			e.saveState()
		}
		if time.Since(lastDHT) > 10*time.Minute {
			lastDHT = time.Now()
			e.mu.Lock()
			d := append([]*dhtNode(nil), e.dhts...)
			e.mu.Unlock()
			e.saveDHTNodes(d)
		}
		if time.Since(lastLSD) > 5*time.Minute {
			lastLSD = time.Now()
			e.lsdAnnounceAll()
		}
	}
}

func (e *Engine) wantedLocked(it *Item) (wanted, done int64, ok bool) {
	t := it.t
	if t == nil || t.Info() == nil {
		return 0, 0, false
	}
	files := t.Files()
	if len(it.filesDone) != len(files) {
		it.filesDone = make([]int64, len(files))
	}
	for i, f := range files {
		c := f.BytesCompleted()
		it.filesDone[i] = c
		if strings.Contains(f.FileInfo().Attr, "p") || f.Priority() == types.PiecePriorityNone {
			continue
		}
		wanted += f.Length()
		done += c
	}
	return wanted, done, true
}

func (e *Engine) tick() {
	now := time.Now()
	ratioLimit := e.cfg.Float("seed_ratio_limit")
	hunter := e.cfg.Bool("peer_hunter")
	var totalDl, totalUl float64
	requeue := false

	e.mu.Lock()
	for _, it := range e.items {
		t := it.t
		if t == nil {
			continue
		}
		st := t.Stats()
		read, written := st.BytesReadUsefulData.Int64(), st.BytesWrittenData.Int64()
		dl := float64(read - it.lastRead)
		ul := float64(written - it.lastWritten)
		if it.lastRead == 0 && it.lastWritten == 0 && (read > 0 || written > 0) {
			dl, ul = 0, 0
		}
		it.lastRead, it.lastWritten = read, written
		it.dl = 0.5*it.dl + 0.5*math.Max(dl, 0)
		it.ul = 0.5*it.ul + 0.5*math.Max(ul, 0)
		totalDl += it.dl
		totalUl += it.ul
		it.history = appendRing(it.history, [2]float64{it.dl, it.ul}, 120)
		it.ActiveSecs++
		if it.Finished {
			it.SeedSecs++
		}
		if !it.everIncoming {
			for _, pc := range t.PeerConns() {
				if pc.Discovery == torrent.PeerSourceIncoming {
					it.everIncoming, e.incoming = true, true
					break
				}
			}
		}
		if wanted, done, ok := e.wantedLocked(it); ok && !it.checking {
			it.Size, it.Done = wanted, done
			if it.Finished && done < wanted {
				// проверка нашла битые куски или выбраны новые файлы: докачиваем
				it.Finished = false
				requeue = true
			}
			if !it.Finished && wanted > 0 && done >= wanted {
				it.Finished = true
				it.Completed = now.Unix()
				it.Force = false
				e.dirty = true
				requeue = true
				e.announceCompletedLocked(it)
				if it.wasDownload && e.cfg.Bool("notify_finished") {
					go e.notify("Загрузка завершена", it.Name)
				}
			}
			if !it.Finished {
				it.wasDownload = true
			}
			if it.Sequential && !it.Finished {
				sequentialWindow(t)
			}
		}
		if ratioLimit > 0 && it.Finished {
			down := max(it.Downloaded+read, it.Done)
			if down > 0 && float64(it.Uploaded+written)/float64(down) >= ratioLimit {
				it.Paused = true
				requeue = true
			}
		}
		if hunter {
			e.huntStepLocked(it, st, now)
		} else {
			it.hunt.Active = false
		}
	}
	e.history = appendRing(e.history, [2]float64{totalDl, totalUl}, 300)
	nodes := 0
	for _, d := range e.dhts {
		nodes += d.s.Stats().GoodNodes
	}
	e.dhtNodes = nodes
	e.mu.Unlock()
	if requeue {
		e.manageQueue()
	}
	if e.cfg.Bool("dht") && nodes < 10 && now.Unix()%60 == 0 {
		e.bootstrapDHT()
	}
}

func appendRing[T any](s []T, v T, n int) []T {
	s = append(s, v)
	if len(s) > n {
		s = s[len(s)-n:]
	}
	return s
}

// sequentialWindow поднимает приоритет ближайших недокачанных кусков.
func sequentialWindow(t *torrent.Torrent) {
	n := t.NumPieces()
	set := 0
	for i := 0; i < n && set < 24; i++ {
		p := t.Piece(i)
		if p.State().Complete || p.State().Priority == types.PiecePriorityNone {
			continue
		}
		switch {
		case set == 0:
			p.SetPriority(types.PiecePriorityNow)
		case set < 4:
			p.SetPriority(types.PiecePriorityNext)
		default:
			p.SetPriority(types.PiecePriorityReadahead)
		}
		set++
	}
}

func (e *Engine) huntStepLocked(it *Item, st torrent.TorrentStats, now time.Time) {
	h := &it.hunt
	if it.dl > 1024 {
		h.sinceData = now
	}
	if it.Finished || it.checking {
		h.Active, h.Step = false, 0
		return
	}
	starving := st.ActivePeers < huntMinPeers || now.Sub(h.sinceData) > huntNoData || !it.HasMeta
	if !starving {
		h.Active, h.Step = false, 0
		return
	}
	h.Active = true
	if now.Sub(h.last) >= huntSteps[min(h.Step, len(huntSteps)-1)] {
		e.huntNowLocked(it)
		h.Step++
	}
}

// huntNowLocked: переобъявиться везде прямо сейчас.
func (e *Engine) huntNowLocked(it *Item) {
	h := &it.hunt
	h.last = time.Now()
	h.Announces++
	for _, tr := range it.trackers {
		if !tr.updating && time.Since(tr.last) >= 15*time.Second {
			tr.next = time.Now()
		}
	}
	if it.t != nil && len(it.Peers) > 0 {
		it.t.AddPeers(peerInfos(it.Peers, torrent.PeerSourceDirect))
	}
	if !it.Private {
		e.lsdAnnounce(it)
		if h.Step%3 == 1 {
			e.addTrackersLocked(it, e.extraTrackers(), true)
		}
	}
}

// statusText - подробный отчет движка (пиры, их флаги choke/interested, куски) для отладки.
func (e *Engine) statusText() string {
	e.mu.Lock()
	cl := e.cl
	e.mu.Unlock()
	var b strings.Builder
	cl.WriteStatus(&b)
	return b.String()
}

// ---------- сохранение ----------

type stateFile struct {
	Items []*Item `json:"items"`
}

func (e *Engine) loadState() {
	b, err := os.ReadFile(e.paths.file("state.json"))
	if err != nil {
		return
	}
	var s stateFile
	if err := json.Unmarshal(b, &s); err != nil {
		logf("state.json не читается: %v", err)
		return
	}
	for _, it := range s.Items {
		if it.ID == "" {
			continue
		}
		var trs [][]string
		if mi, err := metainfo.LoadFromFile(e.metainfoPath(it.ID)); err == nil {
			trs = mi.UpvertedAnnounceList()
		} else if m, err := metainfo.ParseMagnetUri(it.Magnet); err == nil {
			trs = [][]string{m.Trackers}
		}
		e.items[it.ID] = it
		e.buildTrackersLocked(it, trs)
	}
	logf("Восстановлено раздач: %d", len(e.items))
}

func (e *Engine) saveState() {
	e.mu.Lock()
	s := stateFile{}
	for _, it := range e.items {
		cp := *it
		if it.t != nil {
			st := it.t.Stats()
			cp.Uploaded += st.BytesWrittenData.Int64()
			cp.Downloaded += st.BytesReadUsefulData.Int64()
			if peers := knownPeers(it.t); len(peers) > 0 {
				cp.Peers = peers
			}
		}
		s.Items = append(s.Items, &cp)
	}
	e.dirty = false
	e.mu.Unlock()
	sort.Slice(s.Items, func(i, j int) bool { return s.Items[i].Added < s.Items[j].Added })
	b, err := json.MarshalIndent(s, "", " ")
	if err == nil {
		err = writeAtomic(e.paths.file("state.json"), b)
	}
	if err != nil {
		logf("Состояние не сохранено: %v", err)
	}
}

// Shutdown сохраняет все, сообщает трекерам об остановке и закрывает движок.
func (e *Engine) Shutdown() {
	close(e.stop)
	<-e.done
	e.saveState()
	e.mu.Lock()
	for _, it := range e.items {
		if it.t != nil {
			e.announceStoppedLocked(it)
		}
	}
	dhts := e.dhts
	cl := e.cl
	e.mu.Unlock()
	e.saveDHTNodes(dhts)
	time.Sleep(1500 * time.Millisecond) // stopped-запросы трекерам
	if e.lsd != nil {
		e.lsd.Close()
	}
	cl.Close()
	_ = e.pc.Close()
	logf("Движок остановлен")
}

// moveTree переносит файл или папку; между дисками - копированием.
func moveTree(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	err := filepath.Walk(src, func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if fi.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyFile(p, target)
	})
	if err != nil {
		return err
	}
	return os.RemoveAll(src)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := out.ReadFrom(in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
