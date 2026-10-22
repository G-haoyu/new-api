package modelrank

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testPool(ids ...[2]string) *pool {
	p := newPool()
	for _, id := range ids {
		p.add(&candidate{ID: id[0], Slug: id[1], Name: gatewayName(id[0])})
	}
	return p
}

func TestMatch(t *testing.T) {
	p := testPool(
		[2]string{"anthropic/claude-opus-5.5", "anthropic/claude-opus-5.5-20260921"},
		[2]string{"openai/o4-mini", "openai/o4-mini-20250416"},
		[2]string{"openai/o4-mini-high", "openai/o4-mini-high-20250416"},
		[2]string{"qwen/qwen3.8-max-prime", "qwen/qwen3.8-max-prime-20260923"},
		[2]string{"google/gemini-3.1-pro-preview", "google/gemini-3.1-pro-preview-20260219"},
		[2]string{"qwen/qwen3-vl-235b-a22b-instruct", "qwen/qwen3-vl-235b-a22b-instruct"},
		[2]string{"deepseek/deepseek-v4-pro", "deepseek/deepseek-v4-pro-20260423"},
		[2]string{"deepseek/deepseek-v4-pro-0813", "deepseek/deepseek-v4-pro-20260813"},
		[2]string{"meta/muse-spark-1.3", "meta/muse-spark-1.3-20261001"},
	)
	tests := []struct {
		name string
		want string
	}{
		{"Claude Opus 5.5 (Max)", "anthropic/claude-opus-5.5"},
		{"claude-opus-5.5-medium", "anthropic/claude-opus-5.5"},
		{"o4-mini-high", "openai/o4-mini-high"},
		{"o4-mini (2025-04-16)", "openai/o4-mini"},
		{"gemini-3.1-pro", "google/gemini-3.1-pro-preview"},
		{"DeepSeek-V4-Pro (High, 20260813)", "deepseek/deepseek-v4-pro-0813"},
		{"DeepSeek-V4-Pro (High)", "deepseek/deepseek-v4-pro"},
		{"muse-spark-1.3-max", "meta/muse-spark-1.3"},
		{"qwen3.8-max", ""},
		{"Qwen3-VL-235B-A22B-Thinking", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ""
			if c := p.match(tt.name); c != nil {
				got = c.ID
			}
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestGatewayName(t *testing.T) {
	assert.Equal(t, "claude-opus-5-5", gatewayName("anthropic/claude-opus-5.5"))
	assert.Equal(t, "gpt-6.1-sol", gatewayName("openai/gpt-6.1-sol"))
	assert.Equal(t, "deepseek-v4.1-flash", gatewayName("deepseek/deepseek-v4.1-flash"))
}

func TestNormalize(t *testing.T) {
	p := testPool([2]string{"a/x", "a/x"}, [2]string{"a/y", "a/y"}, [2]string{"a/z", "a/z"})
	x, y, z := p.list[0], p.list[1], p.list[2]
	x.record("aa.coding", 80)
	x.record("aa.coding", 60) // a weaker effort variant keeps the best
	y.record("aa.coding", 40)
	x.record("lma.overall", 1500)
	y.record("lma.overall", 1400)
	x.record("lma.agent", 0.2)
	y.record("lma.agent", -0.2)
	z.record("lma.agent", 0)
	p.normalize()

	assert.InDelta(t, 1, x.Norm["aa.coding"], 1e-9)
	assert.InDelta(t, 0.5, y.Norm["aa.coding"], 1e-9)
	assert.InDelta(t, 1, x.Norm["lma.overall"], 1e-9)
	assert.InDelta(t, 2/(1+math.Pow(10, 100.0/400)), y.Norm["lma.overall"], 1e-9)
	assert.InDelta(t, 0.5, z.Norm["lma.agent"], 1e-9)
	assert.InDelta(t, 0, y.Norm["lma.agent"], 1e-9)
	assert.NotContains(t, z.Norm, "aa.coding")
}

func TestScoreAndFrontier(t *testing.T) {
	p := testPool(
		[2]string{"a/top", "a/top"},
		[2]string{"a/pricey", "a/pricey"},
		[2]string{"a/cheap", "a/cheap"},
		[2]string{"a/sparse", "a/sparse"},
		[2]string{"a/weak", "a/weak"},
	)
	price := map[string]float64{"a/top": 8, "a/pricey": 20, "a/cheap": 1, "a/sparse": 0.5, "a/weak": 0.1}
	coding := map[string]float64{"a/top": 100, "a/pricey": 95, "a/cheap": 90, "a/weak": 50}
	for _, c := range p.list {
		c.Price = price[c.ID]
		c.LatencyMs = 1000
		if v, ok := coding[c.ID]; ok {
			for metric := range taskWeights["coding"] {
				if metricScales[metric] == ratio {
					c.record(metric, v)
				}
			}
		}
	}
	// sparse only has one small-weight metric: below min coverage.
	p.list[3].record("da.website", 1600)
	p.list[2].Usage["code:general_impl"] = 0.4
	p.normalize()

	r := ranker{pool: p, minCoverage: 0.5, defaultFloor: 0.8, usageBonus: 0.03, latencyTolerance: 1.5}
	r.score()
	ranked := r.ranked["coding"]
	require.Len(t, ranked, 4)
	assert.Equal(t, "a/top", ranked[0].c.ID)
	assert.InDelta(t, 0.03, ranked[2].Usage, 1e-9)

	frontier := r.frontier("coding")
	assert.True(t, frontier[p.list[0]])
	assert.False(t, frontier[p.list[1]], "pricier and weaker than top")
	assert.True(t, frontier[p.list[2]])
	assert.False(t, frontier[p.list[4]], "below the score floor")

	// Latency within tolerance does not keep a dominated model.
	p.list[1].LatencyMs = 800
	assert.False(t, r.frontier("coding")[p.list[1]])
	p.list[1].LatencyMs = 500
	assert.True(t, r.frontier("coding")[p.list[1]])
}

func TestMapping(t *testing.T) {
	p := testPool(
		[2]string{"a/top", "a/top"},
		[2]string{"a/mid", "a/mid"},
		[2]string{"a/cheap", "a/cheap"},
		[2]string{"a/weak", "a/weak"},
	)
	strength := map[string]float64{"a/top": 100, "a/mid": 95, "a/cheap": 92, "a/weak": 40}
	price := map[string]float64{"a/top": 10, "a/mid": 12, "a/cheap": 1, "a/weak": 0.5}
	for _, c := range p.list {
		c.Price = price[c.ID]
		for metric, scale := range metricScales {
			v := strength[c.ID]
			if scale == elo {
				v = 1000 + 5*v
			}
			c.record(metric, v)
		}
	}
	p.normalize()
	r := ranker{pool: p, minCoverage: 0.5, defaultFloor: 0.9, latencyTolerance: 1.5}
	r.score()
	m, err := r.mapping(map[string]string{})
	require.NoError(t, err)

	// Every covered model is routed, weak ones last as fallbacks.
	for _, task := range TaskOrder {
		assert.Equal(t, []string{"top", "mid", "cheap", "weak"}, m.TaskModels[task], task)
	}
	// The cheap frontier models lead the defaults; dominated and weak ones follow in score order.
	assert.Equal(t, []string{"cheap", "top", "mid", "weak"}, m.DefaultModels)
}

func TestLoadLiveBenchNewestRelease(t *testing.T) {
	// The bundle names a newer release that has no table yet; the newest one
	// with a table wins.
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<script src="./static/js/main.074b2ee8.js"></script>`))
	})
	mux.HandleFunc("/static/js/main.074b2ee8.js", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`_e=["20261014"],x="20261101",y=["20250805"]`))
	})
	mux.HandleFunc("/data/20261014/table.csv", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("model,coding,reasoning,unknown_column\nclaude-opus-5.5-high,81.5,77,1\nunlisted-model,50,50,1\n"))
	})
	mux.HandleFunc("/data/20261101/table.csv", http.NotFound)
	server := httptest.NewServer(mux)
	defer server.Close()

	p := testPool([2]string{"anthropic/claude-opus-5.5", "anthropic/claude-opus-5.5-20260921"})
	src := &sources{client: server.Client(), logf: func(string, ...any) {}}
	release, err := src.loadLiveBench(context.Background(), p, server.URL, "")
	require.NoError(t, err)
	assert.Equal(t, "20261014", release)
	assert.Equal(t, map[string]float64{"lb.coding": 81.5, "lb.reasoning": 77}, p.list[0].Raw)
}
