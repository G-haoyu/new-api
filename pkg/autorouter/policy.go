package autorouter

import "slices"

// AutoModel is the model name a client sends to let the gateway pick one.
const AutoModel = "auto"

// effortLadder lists reasoning efforts in ascending strength. A model supports
// an effort when the effort-suffixed name (model + "-" + effort) is usable;
// the channel adaptors turn that suffix into the provider's reasoning field.
var effortLadder = []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"}

// Policy holds the tunable routing rules: the task type picks the model, the
// difficulty picks the reasoning effort.
type Policy struct {
	// TaskModels maps a jev task type to its models in preference order.
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
	// TaskType is the task type the model was chosen for, empty for defaults.
	TaskType string `json:"task_type,omitempty"`
	Model    string `json:"model"`
	Effort   string `json:"effort"`
}

// SelectRoute maps a jev decision to a route. decision is nil when jev failed;
// previous is the route of the previous instruction, nil when unknown.
// inherited reports that the decision was not used and the route came from
// previous or the defaults. ok is false when no model is available at all.
//
// available judges a model against the request exactly as the client sent it;
// the inherited fallbacks use it because they leave the client's reasoning
// fields alone. overridable judges a model with the client's reasoning fields
// set aside, because a trusted decision picks the effort itself and rewrites them.
func SelectRoute(decision *Decision, previous *Route, policy Policy, available, overridable func(model string) bool) (route Route, inherited, ok bool) {
	trusted := decision != nil && decision.TaskType != "" && decision.TaskType != TaskTypeUnknown &&
		decision.TaskTypeProbability >= policy.MinTaskTypeProbability && decision.Difficulty > 0
	if !trusted {
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
		ModelAvailable(previous.Model, overridable) {
		if slices.Index(effortLadder, previous.Effort) > slices.Index(effortLadder, effort) {
			effort = previous.Effort
		}
		return Route{TaskType: previous.TaskType, Model: previous.Model, Effort: effort}, false, true
	}

	if model, found := firstAvailable(policy.TaskModels[decision.TaskType], overridable); found {
		return Route{TaskType: decision.TaskType, Model: model, Effort: effort}, false, true
	}
	model, found := firstAvailable(policy.DefaultModels, overridable)
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

// AnyModelAvailable reports whether the defaults or any task list holds a
// usable model. Without one no classification can lead to a route, so the
// caller rejects the request before paying for the classifier call.
func (p Policy) AnyModelAvailable(available func(model string) bool) bool {
	if _, found := firstAvailable(p.DefaultModels, available); found {
		return true
	}
	for _, models := range p.TaskModels {
		if _, found := firstAvailable(models, available); found {
			return true
		}
	}
	return false
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
		// 下面的循环从相邻档位找起，不会探测 medium 本身。
		if available(model + "-medium") {
			return model + "-medium", true
		}
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
	SourceCache   = "cache"   // the trusted decision came from the cache; the route is recomputed
	SourceInherit = "inherit" // jev was untrusted or failed; previous route kept
	SourceDefault = "default" // no usable signal and no previous route
)

// Result is the routing outcome of one request.
type Result struct {
	ResolvedModel string `json:"resolved_model"`
	Route
	Source   string    `json:"source"`
	Decision *Decision `json:"decision,omitempty"`
	// EffortInBody 表示档位写进请求体而不是拼在模型名后缀上：只有模型声明了
	// 推理维度，并且裸模型名本身可用时才这样做，其余沿用后缀。
	EffortInBody bool `json:"effort_in_body,omitempty"`
	// EffortOverride 表示这条路由来自可信的 jev 判断，可以覆盖客户端自己写的推理字段；
	// 兜底路径（辅助请求、jev 失败或不可信）为 false，不碰客户端字段。
	EffortOverride bool `json:"effort_override,omitempty"`
	// AppliedEffort 和 EffortAction 是按模型的推理声明收敛后真正写进请求体的结果。
	AppliedEffort string       `json:"applied_effort,omitempty"`
	EffortAction  EffortAction `json:"effort_action,omitempty"`
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
