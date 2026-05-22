package tunnel

const replayWindowBits = 64

type ReplayWindow struct {
	max  uint64
	seen uint64
}

// Accept returns true once for packets inside the most recent 64 sequences.
func (w *ReplayWindow) Accept(sequence uint64) bool {
	if sequence == 0 {
		return false
	}
	if sequence > w.max {
		shift := sequence - w.max
		if shift >= replayWindowBits {
			w.seen = 1
		} else {
			w.seen = w.seen<<shift | 1
		}
		w.max = sequence
		return true
	}
	delta := w.max - sequence
	if delta >= replayWindowBits {
		return false
	}
	mask := uint64(1) << delta
	if w.seen&mask != 0 {
		return false
	}
	w.seen |= mask
	return true
}
