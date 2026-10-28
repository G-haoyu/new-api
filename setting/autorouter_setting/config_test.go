package autorouter_setting

import (
	"testing"

	"github.com/QuantumNous/new-api/pkg/autorouter"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultsFromTaskModelsJSON(t *testing.T) {
	s := GetAutoRouterSetting()
	require.NotEmpty(t, s.DefaultModels)
	for _, taskType := range autorouter.TaskTypes() {
		assert.NotEmpty(t, s.TaskModels[taskType], taskType)
		if class := autorouter.TaskClass(taskType); class != "general" {
			assert.NotEmpty(t, s.TaskModels[class], class)
		}
	}
	assert.Equal(t, []string{"low", "medium", "high", "xhigh"}, s.DifficultyEfforts)
	// The embedded mapping says which leaderboard snapshots the lists are from
	// and which task types they are keyed by.
	for _, source := range []string{"generated_at", "livebench", "lmarena", "artificial_analysis", "design_arena"} {
		assert.NotEmpty(t, s.RankSources[source], source)
	}
	assert.True(t, s.RankingCurrent())
	assert.True(t, s.RankRefreshEnabled)
	assert.Equal(t, 24, s.RankRefreshIntervalHours)
}

func TestRankingFallsBackWhenTaxonomyChanged(t *testing.T) {
	s := GetAutoRouterSetting()
	saved := *s
	t.Cleanup(func() { *s = saved })
	s.TaskModels = map[string][]string{"coding": {"stale"}}
	s.DefaultModels = []string{"stale"}

	s.RankSources = map[string]string{"taxonomy_version": "1"}
	taskModels, defaultModels := s.Ranking()
	assert.Equal(t, shippedRanking.TaskModels, taskModels, "lists keyed by older task types are not used")
	assert.Equal(t, shippedRanking.DefaultModels, defaultModels)

	s.RankSources = map[string]string{"taxonomy_version": autorouter.TaxonomyVersion}
	taskModels, defaultModels = s.Ranking()
	assert.Equal(t, s.TaskModels, taskModels)
	assert.Equal(t, []string{"stale"}, defaultModels)
}
