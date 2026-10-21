package autorouter

import "slices"

// AutoModel is the model name a client sends to let the gateway pick one.
const AutoModel = "auto"

// Tiers in ascending capability order. A tier maps to an ordered model list;
// reasoning effort is expressed by an effort-suffixed model name.
const (
	TierLow  = "low"
	TierMid  = "mid"
	TierHigh = "high"
)

var tierOrder = []string{TierLow, TierMid, TierHigh}

// Policy holds the tunable routing rules. The rules are provisional and will
// be recalibrated against a labeled set.
type Policy struct {
	// Tiers maps a tier to its models in preference order.
	Tiers       map[string][]string
	DefaultTier string
	// UpgradeBenefit is the larger_model_benefit at or above which the tier
	// goes up one rung.
	UpgradeBenefit float64
	// MinTaskTypeProbability is the task type probability below which the
	// classification is not trusted and the previous tier is inherited.
	MinTaskTypeProbability float64
	// ContinuationTaskChange is the task_change below which the request
	// continues the previous task, so the tier may go up but never down — a
	// downgrade mid-task would switch models and drop the prompt cache.
	ContinuationTaskChange float64
}

// SelectTier maps a jev decision to a tier. previousTier is the tier the
// previous instruction was routed to, empty when unknown. inherited reports
// that the decision was not trusted and the tier came from previousTier or
// the default.
func SelectTier(decision Decision, previousTier string, policy Policy) (tier string, inherited bool) {
	fallback := policy.DefaultTier
	if previousTier != "" {
		fallback = previousTier
	}
	if decision.TaskType == "" || decision.TaskType == TaskTypeUnknown ||
		decision.TaskTypeProbability < policy.MinTaskTypeProbability || decision.Difficulty == 0 {
		return fallback, true
	}

	level := 0
	switch {
	case decision.Difficulty >= 3:
		level = 2
	case decision.Difficulty == 2:
		level = 1
	}
	if decision.LargerModelBenefit != nil && *decision.LargerModelBenefit >= policy.UpgradeBenefit {
		level = min(level+1, len(tierOrder)-1)
	}
	if previousLevel := slices.Index(tierOrder, previousTier); previousLevel > level &&
		decision.TaskChange != nil && *decision.TaskChange < policy.ContinuationTaskChange {
		level = previousLevel
	}
	return tierOrder[level], false
}

// Where a tier came from.
const (
	SourceJev     = "jev"     // jev ran and its decision was trusted
	SourceCache   = "cache"   // the same instruction was routed before
	SourceInherit = "inherit" // jev was untrusted or failed; previous tier kept
	SourceDefault = "default" // no usable signal and no previous tier
)

// Result is the routing outcome of one request.
type Result struct {
	ResolvedModel string    `json:"resolved_model"`
	Tier          string    `json:"tier"`
	Source        string    `json:"source"`
	Decision      *Decision `json:"decision,omitempty"`
	// Jev is set only when this request called jev, cache hits leave it nil.
	Jev *JevCall `json:"jev,omitempty"`
}

type JevCall struct {
	Endpoint  string `json:"endpoint"`
	LatencyMs int64  `json:"latency_ms"`
	Error     string `json:"error,omitempty"`
	// Response is the raw jev response body.
	Response []byte `json:"-"`
}

// PickModel returns the first available model of tier. When the tier has none,
// it walks to the nearest tier, preferring the higher one on a tie so an empty
// tier never silently degrades quality. It returns the tier actually used.
func PickModel(tier string, tiers map[string][]string, available func(model string) bool) (model, usedTier string, ok bool) {
	start := slices.Index(tierOrder, tier)
	if start < 0 {
		start = 1
	}
	for distance := 0; distance < len(tierOrder); distance++ {
		levels := []int{start + distance}
		if distance > 0 {
			levels = append(levels, start-distance)
		}
		for _, level := range levels {
			if level < 0 || level >= len(tierOrder) {
				continue
			}
			for _, candidate := range tiers[tierOrder[level]] {
				if available(candidate) {
					return candidate, tierOrder[level], true
				}
			}
		}
	}
	return "", "", false
}
