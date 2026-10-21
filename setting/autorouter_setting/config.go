package autorouter_setting

import (
	"github.com/QuantumNous/new-api/pkg/autorouter"
	"github.com/QuantumNous/new-api/setting/config"
)

// AutoRouterSetting configures resolution of the "auto" model. Option keys are
// autorouter_setting.<json name>; map values are stored as JSON.
type AutoRouterSetting struct {
	Enabled          bool   `json:"enabled"`
	JevEndpoint      string `json:"jev_endpoint"`
	JevTimeoutMs     int    `json:"jev_timeout_ms"`
	MaxRunes         int    `json:"max_runes"`
	PreviousMaxRunes int    `json:"previous_max_runes"`
	CacheTTLSeconds  int    `json:"cache_ttl_seconds"`
	// Tiers maps low/mid/high to models in preference order. Effort-suffixed
	// names (e.g. gpt-5-high) pick the reasoning effort too.
	Tiers                  map[string][]string `json:"tiers"`
	DefaultTier            string              `json:"default_tier"`
	UpgradeBenefit         float64             `json:"upgrade_benefit"`
	MinTaskTypeProbability float64             `json:"min_task_type_probability"`
	ContinuationTaskChange float64             `json:"continuation_task_change"`
}

var autoRouterSetting = AutoRouterSetting{
	JevEndpoint:            "http://hybrid-scheduling-llm-test.intsig.net/startlux-decision-27b/v1/systemone",
	JevTimeoutMs:           1000,
	MaxRunes:               2000,
	PreviousMaxRunes:       500,
	CacheTTLSeconds:        7200,
	Tiers:                  map[string][]string{},
	DefaultTier:            autorouter.TierMid,
	UpgradeBenefit:         0.5,
	MinTaskTypeProbability: 0.5,
	ContinuationTaskChange: 0.5,
}

func init() {
	config.GlobalConfig.Register("autorouter_setting", &autoRouterSetting)
}

func GetAutoRouterSetting() *AutoRouterSetting {
	return &autoRouterSetting
}

func (s *AutoRouterSetting) Policy() autorouter.Policy {
	return autorouter.Policy{
		Tiers:                  s.Tiers,
		DefaultTier:            s.DefaultTier,
		UpgradeBenefit:         s.UpgradeBenefit,
		MinTaskTypeProbability: s.MinTaskTypeProbability,
		ContinuationTaskChange: s.ContinuationTaskChange,
	}
}
