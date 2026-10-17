package graph

import "testing"

func TestLinear(t *testing.T) {
	var l Layout
	a := l.Add("c", []string{"b"})
	b := l.Add("b", []string{"a"})
	c := l.Add("a", nil)
	for i, r := range []Row{a, b, c} {
		if r.Col != 0 {
			t.Fatalf("row %d: col %d, want 0", i, r.Col)
		}
	}
	if l.MaxCols != 1 {
		t.Fatalf("MaxCols = %d, want 1", l.MaxCols)
	}
	if len(c.Segs) != 1 || c.Segs[0].To != Node {
		t.Fatalf("root has %v, want one line in", c.Segs)
	}
}

func TestMergeAndFork(t *testing.T) {
	// m merges b (first parent a) and x; x also comes from a.
	//   m
	//   |\
	//   a x   (a,x both children of base)
	//   |/
	//   base
	var l Layout
	m := l.Add("m", []string{"a", "x"})
	if m.Col != 0 || len(m.Segs) != 2 {
		t.Fatalf("merge: %+v", m)
	}
	var toX Seg
	for _, s := range m.Segs {
		if s.From == Node && s.To != 0 {
			toX = s
		}
	}
	if toX.To != 1 {
		t.Fatalf("second parent goes to column %d, want 1", toX.To)
	}
	a := l.Add("a", []string{"base"})
	if a.Col != 0 {
		t.Fatalf("a in column %d", a.Col)
	}
	x := l.Add("x", []string{"base"})
	if x.Col != 1 {
		t.Fatalf("x in column %d", x.Col)
	}
	base := l.Add("base", nil)
	if base.Col != 0 {
		t.Fatalf("base in column %d", base.Col)
	}
	in := 0
	for _, s := range base.Segs {
		if s.To == Node {
			in++
		}
	}
	if in != 2 {
		t.Fatalf("base joins %d lines, want 2", in)
	}
	if len(l.lanes) != 0 {
		t.Fatalf("lanes left open: %v", l.lanes)
	}
}

func TestTwoTips(t *testing.T) {
	var l Layout
	a := l.Add("t1", []string{"base"})
	b := l.Add("t2", []string{"base"})
	if a.Col == b.Col {
		t.Fatalf("two tips share column %d", a.Col)
	}
	if c := l.Add("base", nil); c.Col != 0 {
		t.Fatalf("base in column %d", c.Col)
	}
}
