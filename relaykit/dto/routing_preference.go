package dto

import (
	"fmt"
	"strings"
)

// RoutingPreference is the token-level, credential-free policy sent to
// Scheduler after new-api authentication. Each API key (token) carries at most
// one preference that applies to every model it requests. Only the provider
// sort mode is user-configurable; constraints such as fallbacks, attempts and
// price/quality limits stay with the platform default policy.
type RoutingPreference struct {
	Mode string `json:"mode,omitempty"`
}

// Normalize trims and lower-cases the mode in place.
func (p *RoutingPreference) Normalize() {
	p.Mode = strings.ToLower(strings.TrimSpace(p.Mode))
}

// Validate rejects unknown modes. An empty mode is allowed and means
// "inherit the platform default".
func (p *RoutingPreference) Validate() error {
	switch p.Mode {
	case "", "balanced", "price", "latency", "throughput", "quality":
		return nil
	default:
		return fmt.Errorf("invalid routing preference mode: %s", p.Mode)
	}
}
