package tunnel

import "testing"

func TestReplayWindowAcceptsFreshAndRejectsReplay(t *testing.T) {
	var window ReplayWindow
	for _, sequence := range []uint64{1, 3, 2, 68, 67} {
		if !window.Accept(sequence) {
			t.Fatalf("sequence %d rejected", sequence)
		}
	}
	for _, sequence := range []uint64{0, 3, 2, 1} {
		if window.Accept(sequence) {
			t.Fatalf("sequence %d replay accepted", sequence)
		}
	}
}
