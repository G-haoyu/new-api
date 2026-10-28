package autorouter

import (
	"maps"
	"slices"
)

// AutoModel is the model name a client sends to let the gateway pick one.
const AutoModel = "auto"

// effortLadder lists reasoning efforts in ascending strength. A model supports
// an effort when the effort-suffixed name (model + "-" + effort) is usable;
// the channel adaptors turn that suffix into the provider's reasoning field.
var effortLadder = []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"}

// Policy holds the tunable routing rules: the task type picks the model, the
// difficulty picks the reasoning effort.
type Policy struct {
	// TaskModels maps a task type or task class to its models in preference
	// order.
	TaskModels map[string][]string
	// DefaultModels serve a task type with no available model in TaskModels
	// and a request with no usable classification.
	DefaultModels []string
	// DifficultyEfforts maps difficulty rungs (1-based) to efforts; rungs past
	// the end use the last entry.
	DifficultyEfforts []string
	DefaultEffort     string
	// UpgradeBenefit is the larger_model_benefit at or above which the effort
	// goes up one rung.
	UpgradeBenefit float64
	// MinTaskTypeProbability is the task type probability below which the
	// classification is not trusted and the previous route is inherited.
	MinTaskTypeProbability float64
	// ContinuationTaskChange is the task_change below which the request
	// continues the previous task: the model stays, so the prompt cache
	// survives, and the effort may go up but never down.
	ContinuationTaskChange float64
}

// Route is a routing choice before it is resolved to a usable model name.
type Route struct {
	// Profile is the ranking the model came from: a task type, its class, or
	// empty for the default models.
	Profile string `json:"profile,omitempty"`
	Model   string `json:"model"`
	Effort  string `json:"effort"`
}

// Fallback names the level of Profile: leaf, class or default.
func (r Route) Fallback() string {
	switch {
	case r.Profile == "":
		return "default"
	case TaskClass(r.Profile) == "":
		return "class"
	default:
		return "leaf"
	}
}

// trustedProfiles returns the rankings a decision may route by, most specific
// first: the chosen task type and its class when the task type is trusted,
// else the most probable class when that is trusted. The class of a task type
// sums the probabilities of its task types. Nil means the decision is not
// trusted.
func trustedProfiles(decision *Decision, policy Policy) []string {
	if decision == nil || decision.Difficulty <= 0 {
		return nil
	}
	if class := TaskClass(decision.TaskType); class != "" && decision.TaskTypeProbability >= policy.MinTaskTypeProbability {
		return []string{decision.TaskType, class}
	}
	classes := map[string]float64{}
	for taskType, probability := range decision.TaskTypeProbabilities {
		if class := TaskClass(taskType); class != "" {
			classes[class] += probability
		}
	}
	best, bestProbability := "", 0.0
	for _, class := range slices.Sorted(maps.Keys(classes)) {
		if classes[class] > bestProbability {
			best, bestProbability = class, classes[class]
		}
	}
	if best == "" || bestProbability < policy.MinTaskTypeProbability {
		return nil
	}
	return []string{best}
}

// SelectRoute maps a jev decision to a route. decision is nil when jev failed;
// previous is the route of the previous instruction, nil when unknown.
// inherited reports that the decision was not used and the route came from
// previous or the defaults. ok is false when no model is available at all.
func SelectRoute(decision *Decision, previous *Route, policy Policy, available func(model string) bool) (route Route, inherited, ok bool) {
	profiles := trustedProfiles(decision, policy)
	if profiles == nil {
		if previous != nil && ModelAvailable(previous.Model, available) {
			return *previous, true, true
		}
		model, found := firstAvailable(policy.DefaultModels, available)
		return Route{Model: model, Effort: policy.DefaultEffort}, true, found
	}

	effort := policy.DefaultEffort
	if len(policy.DifficultyEfforts) > 0 {
		effort = policy.DifficultyEfforts[min(decision.Difficulty, len(policy.DifficultyEfforts))-1]
	}
	if decision.LargerModelBenefit != nil && *decision.LargerModelBenefit >= policy.UpgradeBenefit {
		if rank := slices.Index(effortLadder, effort); rank >= 0 && rank < len(effortLadder)-1 {
			effort = effortLadder[rank+1]
		}
	}

	if previous != nil && decision.TaskChange != nil && *decision.TaskChange < policy.ContinuationTaskChange &&
		ModelAvailable(previous.Model, available) {
		if slices.Index(effortLadder, previous.Effort) > slices.Index(effortLadder, effort) {
			effort = previous.Effort
		}
		return Route{Profile: previous.Profile, Model: previous.Model, Effort: effort}, false, true
	}

	for _, profile := range profiles {
		if model, found := firstAvailable(policy.TaskModels[profile], available); found {
			return Route{Profile: profile, Model: model, Effort: effort}, false, true
		}
	}
	model, found := firstAvailable(policy.DefaultModels, available)
	return Route{Model: model, Effort: effort}, false, found
}

func firstAvailable(models []string, available func(model string) bool) (string, bool) {
	for _, model := range models {
		if ModelAvailable(model, available) {
			return model, true
		}
	}
	return "", false
}

// ModelAvailable reports whether model is usable bare or in any effort variant.
func ModelAvailable(model string, available func(model string) bool) bool {
	_, ok := ResolveVariant(model, "", available)
	return ok
}

// ResolveVariant returns the usable name of model at effort: the exact
// effort-suffixed name, else the bare name (the provider's default effort),
// else the nearest supported effort, preferring the stronger one on a tie so a
// missing variant never silently weakens reasoning. An effort outside the
// ladder searches from medium.
func ResolveVariant(model, effort string, available func(model string) bool) (string, bool) {
	if model == "" {
		return "", false
	}
	if effort != "" && available(model+"-"+effort) {
		return model + "-" + effort, true
	}
	if available(model) {
		return model, true
	}
	start := slices.Index(effortLadder, effort)
	if start < 0 {
		start = slices.Index(effortLadder, "medium")
	}
	for distance := 1; distance < len(effortLadder); distance++ {
		for _, rank := range []int{start + distance, start - distance} {
			if rank >= 0 && rank < len(effortLadder) && available(model+"-"+effortLadder[rank]) {
				return model + "-" + effortLadder[rank], true
			}
		}
	}
	return "", false
}

// Where a route came from.
const (
	SourceJev     = "jev"     // jev ran and its decision was trusted
	SourceCache   = "cache"   // the same instruction was routed before
	SourceInherit = "inherit" // jev was untrusted or failed; previous route kept
	SourceDefault = "default" // no usable signal and no previous route
)

// Result is the routing outcome of one request.
type Result struct {
	ResolvedModel string `json:"resolved_model"`
	Route
	Source   string    `json:"source"`
	Decision *Decision `json:"decision,omitempty"`
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
