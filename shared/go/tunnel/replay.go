package tunnel

// replayWindowBits 是重放窗口的位数，使用 64 位滑动窗口。
// 这意味着可以检测最近 64 个序列号内的重放包。
const replayWindowBits = 64

// ReplayWindow 是一个 64 位滑动窗口，用于 UDP 隧道帧的防重放攻击检测。
//
// 工作原理：
//   - 维护一个 max（已见最大序列号）和一个 seen 位图（已见序列号集合）
//   - 序列号 > max：右移窗口，标记新序列号
//   - 序列号在窗口内但未见过：标记为已见，接受
//   - 序列号在窗口内但已见过：拒绝（重放攻击）
//   - 序列号 < 窗口下界：拒绝（过期包）
//
// 序列号 0 是无效的，会被拒绝。
type ReplayWindow struct {
	max  uint64 // 已见最大序列号
	seen uint64 // 位图：bit[i] 表示 max-i 对应的序列号是否已见
}

// Accept 检查序列号是否可以接受（首次出现在窗口内）。
// 返回 true 表示接受，false 表示拒绝（重复包或过期包）。
// 序列号 0 始终被拒绝。
func (w *ReplayWindow) Accept(sequence uint64) bool {
	if sequence == 0 {
		return false
	}
	if sequence > w.max {
		shift := sequence - w.max
		if shift >= replayWindowBits {
			// 窗口完全刷新，只标记当前序列号
			w.seen = 1
		} else {
			// 右移窗口，标记当前序列号
			w.seen = w.seen<<shift | 1
		}
		w.max = sequence
		return true
	}
	delta := w.max - sequence
	if delta >= replayWindowBits {
		// 序列号太旧，在窗口之外
		return false
	}
	mask := uint64(1) << delta
	if w.seen&mask != 0 {
		// 序列号已见过，是重放包
		return false
	}
	w.seen |= mask
	return true
}
