package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestNewerVersion(t *testing.T) {
	cases := []struct {
		tag, cur string
		want     bool
	}{
		{"v0.1.1", "0.1.0", true},
		{"v1.0", "0.9.9", true},
		{"0.2.0", "0.1.10", true},
		{"v0.1.0", "0.1.0", false},
		{"v0.1", "0.1.0", false},
		{"v0.0.9", "0.1.0", false},
		{"garbage", "0.1.0", false},
		{"v0.2.0", "dev", false},
	}
	for _, c := range cases {
		if got := newerVersion(c.tag, c.cur); got != c.want {
			t.Errorf("newerVersion(%q, %q) = %v", c.tag, c.cur, got)
		}
	}
}

func TestParseTrackerList(t *testing.T) {
	got := parseTrackerList("udp://a.example:1/announce\n\n# comment\nwss://b.example/announce\r\nhttp://c.example/announce\n")
	want := []string{"udp://a.example:1/announce", "http://c.example/announce"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
	if m := mergeTrackers([]string{"udp://A.example:1/"}, []string{"udp://a.example:1"}); len(m) != 1 {
		t.Fatalf("merge did not dedup: %v", m)
	}
}

func TestTrackerPoolCustomFirst(t *testing.T) {
	p := newTrackerPool(filepath.Join(t.TempDir(), "trackers.json"))
	got := p.Get("udp://mine.example:6969/announce")
	if got[0] != "udp://mine.example:6969/announce" {
		t.Fatalf("custom tracker not first: %v", got[:3])
	}
	if !p.Stale() {
		t.Fatal("empty pool must be stale")
	}
}

func TestConfigCoerce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	c := loadConfig(path)
	if p := c.Int("listen_port"); p < 20000 || p > 60000 {
		t.Fatalf("random port %d", p)
	}
	changed := c.Update(map[string]any{"dl_limit": "500", "theme": "nope", "listen_port": 80.0, "unknown": 1.0, "dht": false})
	if len(changed) != 2 || changed["dl_limit"] != 500.0 || changed["dht"] != false {
		t.Fatalf("changed = %v", changed)
	}
	again := loadConfig(path)
	if again.Int("dl_limit") != 500 || again.Bool("dht") {
		t.Fatal("settings not persisted")
	}
}

func TestCellsFromPieces(t *testing.T) {
	have := []bool{true, true, false, false, true, false}
	if got := cellsFromPieces(have, 3); !reflect.DeepEqual(got, []int{9, 0, 5}) {
		t.Fatalf("got %v", got)
	}
	if got := cellsFromPieces(have, 100); len(got) != 6 {
		t.Fatalf("cells must not exceed pieces: %v", got)
	}
}

func TestParseLSD(t *testing.T) {
	msg := "BT-SEARCH * HTTP/1.1\r\nHost: 239.192.152.143:6771\r\nPort: 6881\r\n" +
		"Infohash: DD8255ECDC7CA55FB0BBF81323D87062DB1F6D1C\r\nInfohash: 08ada5a7a6183aae1e09d831df6748d566095a10\r\n" +
		"cookie: abc\r\n\r\n\r\n"
	port, hashes, cookie, ok := parseLSD([]byte(msg))
	if !ok || port != 6881 || cookie != "abc" || len(hashes) != 2 || hashes[0] != "dd8255ecdc7ca55fb0bbf81323d87062db1f6d1c" {
		t.Fatalf("got %d %v %q %v", port, hashes, cookie, ok)
	}
	if _, _, _, ok := parseLSD([]byte("GET / HTTP/1.1\r\n\r\n")); ok {
		t.Fatal("non BT-SEARCH accepted")
	}
}

func TestParseSourceMagnet(t *testing.T) {
	src, err := parseSource("magnet:?xt=urn:btih:dd8255ecdc7ca55fb0bbf81323d87062db1f6d1c&dn=Test&tr=udp%3A%2F%2Ftracker.example.com%3A80", nil)
	if err != nil {
		t.Fatal(err)
	}
	p := src.preview()
	if p.ID != "dd8255ecdc7ca55fb0bbf81323d87062db1f6d1c" || p.Name != "Test" || p.HasMetadata {
		t.Fatalf("preview %+v", p)
	}
	if _, err := parseSource("", []byte("not a torrent")); err == nil {
		t.Fatal("garbage accepted as .torrent")
	}
}

func TestUIEmbedded(t *testing.T) {
	for _, f := range []string{"ui/index.html", "ui/app.js", "ui/style.css", "ui/icon-64.png"} {
		if _, err := uiFiles.ReadFile(f); err != nil {
			t.Errorf("%s not embedded: %v", f, err)
		}
	}
	b, _ := os.ReadFile("VERSION")
	if v := strings.TrimSpace(string(b)); v == "" || strings.Count(v, ".") != 2 {
		t.Errorf("VERSION file: %q", v)
	}
}

func TestCreateShortcut(t *testing.T) {
	dir := t.TempDir()
	target, _ := os.Executable()
	lnk := filepath.Join(dir, "it's a test.lnk") // апостроф: проверка экранирования
	if err := createShortcut(lnk, target); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(lnk); err != nil || fi.Size() == 0 {
		t.Fatalf("shortcut not written: %v", err)
	}
}
