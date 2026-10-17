package github

import "testing"

func TestADraftWithoutALineStaysOnItsLines(t *testing.T) {
	ten, twelve := 10, 12
	pending := []reviewCommentJSON{{ID: 5, Path: "a.go", Side: Right, OriginalLine: &twelve, StartLine: &ten, Body: "draft"}}
	published := []reviewCommentJSON{{ID: 4, Path: "a.go", Side: Right, OriginalLine: &ten, Body: "old"}}
	out := normalize(published, []reviewJSON{{ID: 9, State: "PENDING"}}, pending, nil)
	var draft, old *Thread
	for _, th := range out.Threads {
		if th.Pending {
			draft = th
		} else {
			old = th
		}
	}
	if draft == nil || draft.Outdated || draft.Line != 12 || draft.StartLine != 10 {
		t.Errorf("draft = %+v", draft)
	}
	// A published comment with no line is outdated.
	if old == nil || !old.Outdated || old.Line != 0 {
		t.Errorf("published = %+v", old)
	}
}
