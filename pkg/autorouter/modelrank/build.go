// Package modelrank builds the autorouter task → model mapping from public
// leaderboards: OpenRouter benchmarks (Artificial Analysis indexes, Design
// Arena Elo, OpenRouter's own GPQA / tau-bench runs), OpenRouter task usage,
// LiveBench and LMArena. Each jev task type is scored as a weighted mean of
// the metrics relevant to it and routes to every covered model in score order;
// the cheapest general models on the score / price / latency Pareto frontier
// lead the defaults.
package modelrank

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"time"
)

// Config holds the data sources and ranking parameters of a build.
type Config struct {
	OpenRouter string // OpenRouter API base
	APIKey     string // OpenRouter key; /benchmarks and /classifications need one
	LiveBench  string // LiveBench site
	// LiveBenchRelease pins a release directory (YYYYMMDD); empty takes the
	// newest published one.
	LiveBenchRelease string
	LMArena          string // LMArena leaderboard dataset base
	MinContext       int    // minimum context length of a candidate
	// MinCoverage is the share of a profile's metric weight a model needs data
	// for to be ranked under it.
	MinCoverage float64
	// DefaultFloor is the share of the best general score the cheap leading
	// default models need.
	DefaultFloor float64
	// UsageBonus is the score bonus of the most used model of a task's
	// OpenRouter tags.
	UsageBonus float64
	// LatencyTolerance is the latency ratio below which two models count as
	// equally fast.
	LatencyTolerance float64
	HTTPClient       *http.Client
	// Logf receives progress and the leaderboard names left unmatched.
	Logf func(format string, args ...any)
}

// DefaultConfig returns the public sources and the ranking parameters the
// shipped mapping was built with.
func DefaultConfig() Config {
	return Config{
		OpenRouter:       "https://openrouter.ai/api/v1",
		LiveBench:        "https://livebench.ai",
		LMArena:          "https://hf-mirror.com/datasets/lmarena-ai/leaderboard-dataset/resolve/main",
		MinContext:       128000,
		MinCoverage:      0.5,
		DefaultFloor:     0.9,
		UsageBonus:       0.03,
		LatencyTolerance: 1.5,
		HTTPClient:       &http.Client{Timeout: 120 * time.Second},
		Logf:             func(string, ...any) {},
	}
}

// Build fetches every source and ranks the candidates. It fails when any
// source fails or a task ends up without a model, so a partial fetch never
// replaces a complete mapping.
func Build(ctx context.Context, cfg Config) (*Mapping, error) {
	src := &sources{client: cfg.HTTPClient, openRouter: cfg.OpenRouter, apiKey: cfg.APIKey, logf: cfg.Logf}
	pool, err := src.loadCandidates(ctx, cfg.MinContext)
	if err != nil {
		return nil, fmt.Errorf("openrouter_models: %w", err)
	}
	cfg.Logf("candidates=%d", len(pool.list))

	meta := map[string]string{"generated_at": time.Now().UTC().Format(time.RFC3339)}
	steps := []struct {
		name string
		load func() (string, error)
	}{
		{"openrouter_benchmarks", func() (string, error) { return src.loadBenchmarks(ctx, pool) }},
		{"livebench", func() (string, error) { return src.loadLiveBench(ctx, pool, cfg.LiveBench, cfg.LiveBenchRelease) }},
		{"lmarena", func() (string, error) { return src.loadLMArena(ctx, pool, cfg.LMArena) }},
		{"openrouter_usage", func() (string, error) { return src.loadUsage(ctx, pool) }},
	}
	for _, step := range steps {
		asOf, err := step.load()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", step.name, err)
		}
		meta[step.name] = asOf
	}
	for _, name := range slices.Sorted(maps.Keys(pool.matches)) {
		cfg.Logf("alias %s -> %s", name, pool.matches[name])
	}
	pool.normalize()

	r := ranker{
		pool:             pool,
		minCoverage:      cfg.MinCoverage,
		defaultFloor:     cfg.DefaultFloor,
		usageBonus:       cfg.UsageBonus,
		latencyTolerance: cfg.LatencyTolerance,
	}
	r.score()
	src.loadLatency(ctx, r.evidenceCandidates())
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return r.mapping(meta)
}
