package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseLFS(t *testing.T) {
	track := "Listing tracked patterns\n    *.psd (.gitattributes)\n    assets/big.bin (sub/.gitattributes)\nListing excluded patterns\n"
	got := parseLFSTrack(track)
	if len(got) != 2 || got[0].Pattern != "*.psd" || got[1].Source != "sub/.gitattributes" {
		t.Fatalf("patterns = %+v", got)
	}
	files := parseLFSFiles("4d7a214614 * art/hero.psd\n0123456789 - space name.bin\n")
	if len(files) != 2 || !files[0].Present || files[1].Present || files[1].Path != "space name.bin" {
		t.Fatalf("files = %+v", files)
	}
	oid, size, ok := LFSPointer("version https://git-lfs.github.com/spec/v1\noid sha256:abc123\nsize 42\n")
	if !ok || oid != "abc123" || size != "42" {
		t.Fatalf("pointer = %q %q %v", oid, size, ok)
	}
}

func TestLFSTrackAddAndList(t *testing.T) {
	if !LFSAvailable() {
		t.Skip("git-lfs is not installed")
	}
	r := newRepo(t)
	if r.LFSEnabled() {
		t.Fatal("a new repository has no LFS filters")
	}
	if err := r.LFSInstall(); err != nil {
		t.Fatal(err)
	}
	if !r.LFSEnabled() {
		t.Fatal("LFS is not enabled after install")
	}
	if err := r.LFSTrack("*.bin"); err != nil {
		t.Fatal(err)
	}
	ps, err := r.LFSPatterns()
	if err != nil || len(ps) != 1 || ps[0].Pattern != "*.bin" || ps[0].Source != ".gitattributes" {
		t.Fatalf("patterns = %+v, %v", ps, err)
	}
	if err := os.WriteFile(filepath.Join(r.Root, "data.bin"), []byte(strings.Repeat("x", 100)), 0o644); err != nil {
		t.Fatal(err)
	}
	commit(t, r, "add data")
	files, err := r.LFSFiles()
	if err != nil || len(files) != 1 || files[0].Path != "data.bin" || !files[0].Present {
		t.Fatalf("files = %+v, %v", files, err)
	}
	// The committed blob is a pointer.
	blob, _ := r.ReadFile("HEAD", "data.bin")
	if _, size, ok := LFSPointer(string(blob)); !ok || size != "100" {
		t.Fatalf("blob = %q", blob)
	}
	if err := r.LFSUntrack("*.bin"); err != nil {
		t.Fatal(err)
	}
	if ps, _ = r.LFSPatterns(); len(ps) != 0 {
		t.Fatalf("still tracked: %+v", ps)
	}
	if _, err := r.LFSStatus(); err != nil {
		t.Fatal(err)
	}
}
