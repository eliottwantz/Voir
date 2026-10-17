// Package graph lays out the commit graph: for each commit, in the order of
// the log, the column of its dot and the lines that join it to the rows
// above and below.
package graph

// Node is the Seg end that sits on the commit's dot.
const Node = -1

// Seg is a line of a row. From is the column where it meets the row's top
// edge, To where it meets the bottom edge; Node (-1) on either end puts that
// end on the commit's dot.
type Seg struct {
	From, To int
	Color    int
}

// Row is the graph of one commit.
type Row struct {
	// Col is the column of the commit's dot.
	Col   int
	Color int
	Segs  []Seg
	// Cols is how many columns the row uses, its lines included.
	Cols int
}

type lane struct {
	hash  string
	color int
}

// Layout lays out commits one after another; it keeps the lanes that are
// still open between calls, so a log loaded in pages continues where the
// last page stopped.
type Layout struct {
	lanes []lane
	color int
	// MaxCols is the most columns any row used.
	MaxCols int
}

func (l *Layout) newColor() int {
	l.color++
	return l.color - 1
}

func (l *Layout) slot(avoid int) int {
	for i := range l.lanes {
		if l.lanes[i].hash == "" && i != avoid {
			return i
		}
	}
	l.lanes = append(l.lanes, lane{})
	return len(l.lanes) - 1
}

// Add lays out the next commit of the log.
func (l *Layout) Add(hash string, parents []string) Row {
	var row Row
	before := len(l.lanes)

	var idxs []int
	for i, ln := range l.lanes {
		if ln.hash == hash {
			idxs = append(idxs, i)
		}
	}
	node := -1
	color := 0
	if len(idxs) == 0 {
		node = l.slot(-1)
		color = l.newColor()
		l.lanes[node] = lane{hash: hash, color: color}
	} else {
		node = idxs[0]
		color = l.lanes[node].color
	}
	row.Col, row.Color = node, color

	// Lines in: every lane that was waiting for this commit.
	isNode := map[int]bool{}
	for _, i := range idxs {
		isNode[i] = true
		row.Segs = append(row.Segs, Seg{From: i, To: Node, Color: l.lanes[i].color})
	}
	// Lines through: the other lanes go straight down.
	for i, ln := range l.lanes {
		if ln.hash != "" && !isNode[i] && i != node {
			row.Segs = append(row.Segs, Seg{From: i, To: i, Color: ln.color})
		}
	}
	// The lanes that met here close; the first stays with the first parent.
	for _, i := range idxs[min(1, len(idxs)):] {
		l.lanes[i] = lane{}
	}
	if len(parents) == 0 {
		l.lanes[node] = lane{}
	} else {
		l.lanes[node] = lane{hash: parents[0], color: color}
		row.Segs = append(row.Segs, Seg{From: Node, To: node, Color: color})
		for _, p := range parents[1:] {
			target := -1
			for i, ln := range l.lanes {
				if ln.hash == p {
					target = i
					break
				}
			}
			c := 0
			if target >= 0 {
				c = l.lanes[target].color
			} else {
				target = l.slot(node)
				c = l.newColor()
				l.lanes[target] = lane{hash: p, color: c}
			}
			row.Segs = append(row.Segs, Seg{From: Node, To: target, Color: c})
		}
	}
	row.Cols = max(before, len(l.lanes), node+1)
	for len(l.lanes) > 0 && l.lanes[len(l.lanes)-1].hash == "" {
		l.lanes = l.lanes[:len(l.lanes)-1]
	}
	l.MaxCols = max(l.MaxCols, row.Cols)
	return row
}
