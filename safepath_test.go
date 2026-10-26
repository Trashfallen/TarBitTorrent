package main

import (
	"path/filepath"
	"testing"

	"github.com/anacrolix/torrent/metainfo"
)

func TestSafeName(t *testing.T) {
	cases := map[string]string{
		"..":         "_",
		".":          "_",
		"":           "_",
		`..\..\x`:    `.._.._x`,
		"a/b":        "a_b",
		"C:":         "C_",
		"file.txt. ": "file.txt",
		"ok.mkv":     "ok.mkv",
	}
	for in, want := range cases {
		if got := safeName(in); got != want {
			t.Errorf("safeName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRelFilePathStaysInside(t *testing.T) {
	base := filepath.Join(`C:\`, "Downloads", "t")
	single := &metainfo.Info{Name: "..", Length: 1}
	multi := &metainfo.Info{Name: "..", Files: []metainfo.FileInfo{
		{Path: []string{"..", "..", "AppData", "x.dll"}, Length: 1},
		{Path: []string{"", ".", "a", "b.txt"}, Length: 1},
		{Path: []string{`C:\Windows`, "y"}, Length: 1},
	}}
	for _, f := range single.UpvertedFiles() {
		if p := filepath.Join(base, relFilePath(single, &f)); !within(base, p) {
			t.Errorf("single: %s outside %s", p, base)
		}
	}
	for _, f := range multi.UpvertedFiles() {
		if p := filepath.Join(base, relFilePath(multi, &f)); !within(base, p) {
			t.Errorf("multi: %s outside %s", p, base)
		}
	}
	if got := relFilePath(multi, &multi.Files[1]); got != filepath.Join("a", "b.txt") {
		t.Errorf("empty and dot parts: got %q", got)
	}
	if got := folderFor(nil, multi); got != "" {
		t.Errorf("folderFor(nil, ..) = %q, want empty", got)
	}
}

func TestWithin(t *testing.T) {
	base := filepath.Join(`C:\`, "Users", "me", "Downloads")
	cases := map[string]bool{
		filepath.Join(base, "a"):                true,
		filepath.Join(base, "a", "b"):           true,
		base:                                    false,
		filepath.Join(base, ".."):               false,
		filepath.Join(base, "..", "x"):          false,
		filepath.Join(base, "..", "Downloads2"): false,
		filepath.Join(`D:\`, "x"):               false,
	}
	for p, want := range cases {
		if got := within(base, p); got != want {
			t.Errorf("within(%q) = %v, want %v", p, got, want)
		}
	}
}
