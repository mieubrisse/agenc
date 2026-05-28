package server

import "testing"

func TestComputeMissionAttached(t *testing.T) {
	attachedPane := "42"
	unlinkedPane := "99"
	linked := map[string]bool{"42": true}

	if !computeMissionAttached(&attachedPane, linked) {
		t.Errorf("pane in linked set should report attached")
	}
	if computeMissionAttached(&unlinkedPane, linked) {
		t.Errorf("pane absent from linked set should report not attached")
	}
	if computeMissionAttached(nil, linked) {
		t.Errorf("nil pane should report not attached")
	}
}
