package attemptlog

import (
	"sync"
	"sync/atomic"
)

// inflightChannelState tracks concurrent load for a single channel. The gateway
// has no such counter of its own, so it is derived here from the attempt
// lifecycle: an attempt is in flight between BeginAttempt and Finish.
type inflightChannelState struct {
	requests  atomic.Int64
	tokensEst atomic.Int64
}

var inflightByChannel sync.Map // channelId int -> *inflightChannelState

func channelInflight(channelId int) *inflightChannelState {
	if state, ok := inflightByChannel.Load(channelId); ok {
		return state.(*inflightChannelState)
	}
	state, _ := inflightByChannel.LoadOrStore(channelId, &inflightChannelState{})
	return state.(*inflightChannelState)
}

// readInflight samples current load for a channel. It is called before the
// caller's own attempt is added, so the returned numbers describe what the
// channel looked like at the moment this attempt was dispatched, excluding this
// attempt itself. That is the reading a routing model needs.
//
// The two counters are read independently, so a concurrent attempt can land
// between them. For telemetry that skew is acceptable and is not worth a lock
// on the relay hot path.
func readInflight(channelId int) (requests int, tokensEst int) {
	state := channelInflight(channelId)
	return int(state.requests.Load()), int(state.tokensEst.Load())
}

func addInflight(channelId int, tokensEst int) {
	state := channelInflight(channelId)
	state.requests.Add(1)
	state.tokensEst.Add(int64(tokensEst))
}

func removeInflight(channelId int, tokensEst int) {
	state := channelInflight(channelId)
	if state.requests.Add(-1) < 0 {
		// A decrement without a matching increment would leave the counter
		// permanently negative and poison every later snapshot. Clamp instead.
		state.requests.Store(0)
	}
	if state.tokensEst.Add(-int64(tokensEst)) < 0 {
		state.tokensEst.Store(0)
	}
}
