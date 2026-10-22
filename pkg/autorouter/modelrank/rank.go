package modelrank

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"
)

type ranker struct {
	pool        *pool
	minCoverage float64
	// defaultFloor is the share of the best general score the cheap leading
	// default models need.
	defaultFloor float64
	usageBonus   float64
	// latencyTolerance is the latency ratio below which two models count as
	// equally fast.
	latencyTolerance float64
	// ranked holds, per scoring profile, the models with enough coverage in
	// descending score order.
	ranked map[string][]*scored
}

type scored struct {
	c        *candidate
	Quality  float64 // weighted mean of the covered metrics
	Coverage float64 // covered share of the profile's metric weight
	Usage    float64 // bonus from OpenRouter task usage
	Score    float64 // Quality + Usage
}

// score ranks every candidate under every profile. The usage bonus scales
// with the model's share of the profile's busiest tag relative to the most
// used model there, so popularity only reorders models of similar quality.
func (r *ranker) score() {
	r.ranked = map[string][]*scored{}
	for profile, weights := range taskWeights {
		topShare := 0.0
		for _, c := range r.pool.list {
			topShare = max(topShare, usageShare(c, profile))
		}
		var list []*scored
		for _, c := range r.pool.list {
			var sum, covered float64
			for metric, w := range weights {
				if v, ok := c.Norm[metric]; ok {
					sum += w * v
					covered += w
				}
			}
			if covered < r.minCoverage-1e-9 {
				continue
			}
			s := &scored{c: c, Quality: sum / covered, Coverage: covered}
			if topShare > 0 {
				s.Usage = r.usageBonus * usageShare(c, profile) / topShare
			}
			s.Score = s.Quality + s.Usage
			list = append(list, s)
		}
		slices.SortFunc(list, func(a, b *scored) int {
			if c := cmp.Compare(b.Score, a.Score); c != 0 {
				return c
			}
			return cmp.Compare(a.c.ID, b.c.ID)
		})
		r.ranked[profile] = list
	}
}

func usageShare(c *candidate, profile string) float64 {
	share := 0.0
	for _, tag := range usageTags[profile] {
		share = max(share, c.Usage[tag])
	}
	return share
}

// evidenceCandidates are the models any profile ranks; their latency feeds
// the frontier and is recorded with them.
func (r *ranker) evidenceCandidates() []*candidate {
	seen := map[*candidate]bool{}
	var list []*candidate
	for _, profile := range slices.Sorted(maps.Keys(r.ranked)) {
		for _, s := range r.ranked[profile] {
			if !seen[s.c] {
				seen[s.c] = true
				list = append(list, s.c)
			}
		}
	}
	return list
}

// dominates reports whether a is at least as good as b on score, price and
// latency and better on one of them. Latency is a 30-minute snapshot, so it
// only counts when both are known and differ by more than latencyTolerance
// times.
func (r *ranker) dominates(a, b *scored) bool {
	latencyKnown := a.c.LatencyMs > 0 && b.c.LatencyMs > 0
	slower := latencyKnown && a.c.LatencyMs > b.c.LatencyMs*r.latencyTolerance
	faster := latencyKnown && a.c.LatencyMs*r.latencyTolerance < b.c.LatencyMs
	if a.Score < b.Score || a.c.Price > b.c.Price || slower {
		return false
	}
	return a.Score > b.Score || a.c.Price < b.c.Price || faster
}

// frontier returns the profile's models within defaultFloor of the best that
// are on the score / price / latency Pareto frontier.
func (r *ranker) frontier(profile string) map[*candidate]bool {
	list := r.ranked[profile]
	on := map[*candidate]bool{}
	for _, s := range list {
		if s.Score < list[0].Score*r.defaultFloor {
			break
		}
		if !slices.ContainsFunc(list, func(o *scored) bool { return r.dominates(o, s) }) {
			on[s.c] = true
		}
	}
	return on
}

// Mapping is the generated ranking. rank_sources, task_models and
// default_models are autorouter setting values; the rest records how they
// were derived.
type Mapping struct {
	Sources       map[string]string             `json:"rank_sources"`
	Weights       map[string]map[string]float64 `json:"weights"`
	TaskModels    map[string][]string           `json:"task_models"`
	DefaultModels []string                      `json:"default_models"`
	Evidence      map[string][]evidence         `json:"evidence"`
	Models        map[string]modelInfo          `json:"models"`
}

type evidence struct {
	Model    string  `json:"model"`
	Score    float64 `json:"score"`
	Quality  float64 `json:"quality"`
	Coverage float64 `json:"coverage"`
	Usage    float64 `json:"usage,omitempty"`
	Frontier bool    `json:"frontier,omitempty"`
}

type modelInfo struct {
	OpenRouterID string             `json:"openrouter_id"`
	Price        float64            `json:"price_per_mtok"`
	LatencyMs    float64            `json:"latency_p50_ms,omitempty"`
	Efforts      []string           `json:"efforts,omitempty"`
	Metrics      map[string]float64 `json:"metrics"`
}

func round(v float64) float64 {
	return math.Round(v*1e4) / 1e4
}

// mapping routes each task to every model covering it in score order: the
// router takes the first one the caller can use, so whatever the deployment
// carries, the task lands on its best deployed model by the task's own
// ranking. Score order already puts every model after the ones dominating it.
// Default models serve unknown and unmapped tasks: the general-profile
// frontier, cheapest first, then every other general model in score order.
func (r *ranker) mapping(sources map[string]string) (*Mapping, error) {
	m := &Mapping{
		Sources:    sources,
		Weights:    taskWeights,
		TaskModels: map[string][]string{},
		Evidence:   map[string][]evidence{},
		Models:     map[string]modelInfo{},
	}
	for _, task := range TaskOrder {
		for _, s := range r.ranked[task] {
			m.TaskModels[task] = append(m.TaskModels[task], s.c.Name)
		}
		if len(m.TaskModels[task]) == 0 {
			return nil, fmt.Errorf("task %s has no model with coverage >= %.2f", task, r.minCoverage)
		}
	}

	general := r.ranked[generalProfile]
	if len(general) == 0 {
		return nil, errors.New("no model covers the general profile")
	}
	frontier := r.frontier(generalProfile)
	var defaults []*scored
	for _, s := range general {
		if frontier[s.c] {
			defaults = append(defaults, s)
		}
	}
	slices.SortStableFunc(defaults, func(a, b *scored) int { return cmp.Compare(a.c.Price, b.c.Price) })
	for _, s := range general {
		if !frontier[s.c] {
			defaults = append(defaults, s)
		}
	}
	for _, s := range defaults {
		m.DefaultModels = append(m.DefaultModels, s.c.Name)
	}

	for _, profile := range append(slices.Clone(TaskOrder), generalProfile) {
		frontier := r.frontier(profile)
		for _, s := range r.ranked[profile] {
			m.Evidence[profile] = append(m.Evidence[profile], evidence{
				Model:    s.c.Name,
				Score:    round(s.Score),
				Quality:  round(s.Quality),
				Coverage: round(s.Coverage),
				Usage:    round(s.Usage),
				Frontier: frontier[s.c],
			})
			metrics := map[string]float64{}
			for metric, v := range s.c.Raw {
				metrics[metric] = round(v)
			}
			m.Models[s.c.Name] = modelInfo{
				OpenRouterID: s.c.ID,
				Price:        round(s.c.Price),
				LatencyMs:    round(s.c.LatencyMs),
				Efforts:      s.c.Efforts,
				Metrics:      metrics,
			}
		}
	}
	return m, nil
}
