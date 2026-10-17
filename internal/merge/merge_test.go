package merge

import (
	"strings"
	"testing"
)

const base = "a\nb\nc\nd\ne\nf\ng\n"

func TestNoConflictMergesBothSides(t *testing.T) {
	ours := "A\nb\nc\nd\ne\nf\ng\n"
	theirs := "a\nb\nc\nd\ne\nf\nG\n"
	chunks, err := Merge3(ours, base, theirs)
	if err != nil {
		t.Fatal(err)
	}
	if Conflicts(chunks) != 0 {
		t.Fatalf("chunks = %+v", chunks)
	}
	if got := Resolve(chunks, nil); got != "A\nb\nc\nd\ne\nf\nG\n" {
		t.Fatalf("merged = %q", got)
	}
}

func TestConflictChunks(t *testing.T) {
	ours := "a\nOURS\nc\nd\ne\nf\nours-g\n"
	theirs := "a\nTHEIRS\nc\nd\ne\nf\ntheirs-g\n"
	chunks, err := Merge3(ours, base, theirs)
	if err != nil {
		t.Fatal(err)
	}
	if Conflicts(chunks) != 2 {
		t.Fatalf("chunks = %+v", chunks)
	}
	first := chunks[1]
	if !first.Conflict || strings.Join(first.Ours, "") != "OURS\n" || strings.Join(first.Theirs, "") != "THEIRS\n" || strings.Join(first.Base, "") != "b\n" {
		t.Fatalf("first conflict = %+v", first)
	}
	// Each way of resolving.
	if got := Resolve(chunks, []Choice{0, TakeOurs, 0, TakeTheirs}); got != "a\nOURS\nc\nd\ne\nf\ntheirs-g\n" {
		t.Fatalf("ours+theirs = %q", got)
	}
	if got := Resolve(chunks, []Choice{0, TakeBoth, 0, TakeBothBack}); got != "a\nOURS\nTHEIRS\nc\nd\ne\nf\ntheirs-g\nours-g\n" {
		t.Fatalf("both = %q", got)
	}
	if got := Resolve(chunks, []Choice{0, TakeBase, 0, TakeBase}); got != base {
		t.Fatalf("base = %q", got)
	}
	// Left alone, the markers stay, and HasMarkers sees them.
	left := Resolve(chunks, nil)
	if !HasMarkers(left) || !strings.Contains(left, "<<<<<<< ours") {
		t.Fatalf("unresolved = %q", left)
	}
	if HasMarkers(Resolve(chunks, []Choice{0, TakeOurs, 0, TakeOurs})) {
		t.Fatal("resolved text still has markers")
	}
}

func TestBothAddedWithoutABase(t *testing.T) {
	chunks, err := Merge3("mine\n", "", "yours\n")
	if err != nil {
		t.Fatal(err)
	}
	if Conflicts(chunks) != 1 || strings.Join(chunks[0].Base, "") != "" {
		t.Fatalf("chunks = %+v", chunks)
	}
}

func TestFileWithoutTrailingNewline(t *testing.T) {
	chunks, err := Merge3("x\nours", "x\nbase", "x\ntheirs")
	if err != nil {
		t.Fatal(err)
	}
	if Conflicts(chunks) != 1 {
		t.Fatalf("chunks = %+v", chunks)
	}
	if got := Resolve(chunks, []Choice{0, TakeTheirs}); !strings.HasPrefix(got, "x\ntheirs") {
		t.Fatalf("got %q", got)
	}
}

func TestTextThatLooksLikeMarkers(t *testing.T) {
	// A file that has seven-character marker lines of its own.
	ours := "<<<<<<< not a marker\nours\n"
	chunks, err := Merge3(ours, "<<<<<<< not a marker\nbase\n", "<<<<<<< not a marker\ntheirs\n")
	if err != nil {
		t.Fatal(err)
	}
	if Conflicts(chunks) != 1 || strings.Join(chunks[0].Common, "") != "<<<<<<< not a marker\n" {
		t.Fatalf("chunks = %+v", chunks)
	}
}
