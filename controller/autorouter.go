package controller

import (
	"maps"
	"slices"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/autorouter"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/autorouter_setting"

	"github.com/gin-gonic/gin"
)

// GetAutoRouterStatus reports what the settings page cannot read from the
// options: whether the router can run, where each connection value comes from
// (never the secret values themselves), and how the last leaderboard refresh ended.
func GetAutoRouterStatus(c *gin.Context) {
	setting := autorouter_setting.GetAutoRouterSetting()
	connections := setting.Connections()
	status := gin.H{
		"enabled":          setting.Enabled,
		"jev_ready":        connections.JevEndpoint.Value != "",
		"rank_keys_ready":  connections.RankKeysReady(),
		"ranking_current":  setting.RankingCurrent(),
		"taxonomy_version": autorouter.TaxonomyVersion,
		"rank_sources":     setting.Ranking().Sources,
		"sources": map[string]string{
			"jev_endpoint":                connections.JevEndpoint.Source,
			"jev_model":                   connections.JevModel.Source,
			"jev_api_key":                 connections.JevAPIKey.Source,
			"artificial_analysis_api_key": connections.ArtificialAnalysisKey.Source,
			"design_arena_api_key":        connections.DesignArenaKey.Source,
			"openrouter_api_key":          connections.OpenRouterKey.Source,
		},
	}
	latest, err := model.GetLatestSystemTasks([]string{model.SystemTaskTypeAutoRouterRank})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if task := latest[model.SystemTaskTypeAutoRouterRank]; task != nil {
		status["rank_refresh"] = task.ToResponse()
	}
	common.ApiSuccess(c, status)
}

type autoRouterRankedModel struct {
	Model string `json:"model"`
	// Score is nil for a model the lists name but the scores do not cover.
	Score *float64 `json:"score"`
}

type autoRouterRankedProfile struct {
	Profile string                  `json:"profile"`
	Models  []autoRouterRankedModel `json:"models"`
}

// GetAutoRouterRanking returns the ranking the router routes by right now,
// which is the embedded one while the stored ranking predates the task types.
func GetAutoRouterRanking(c *gin.Context) {
	setting := autorouter_setting.GetAutoRouterSetting()
	ranking := setting.Ranking()
	lists := make(map[string][]string, len(ranking.TaskModels)+1)
	maps.Copy(lists, ranking.TaskModels)
	lists[autorouter.DefaultProfile] = ranking.DefaultModels

	profiles := make([]autoRouterRankedProfile, 0, len(lists))
	for _, profile := range slices.Sorted(maps.Keys(lists)) {
		models := make([]autoRouterRankedModel, 0, len(lists[profile]))
		for _, name := range lists[profile] {
			ranked := autoRouterRankedModel{Model: name}
			if score, scored := ranking.TaskScores[profile][name]; scored {
				ranked.Score = &score
			}
			models = append(models, ranked)
		}
		profiles = append(profiles, autoRouterRankedProfile{Profile: profile, Models: models})
	}
	common.ApiSuccess(c, gin.H{
		"current":          setting.RankingCurrent(),
		"taxonomy_version": autorouter.TaxonomyVersion,
		"sources":          ranking.Sources,
		"profiles":         profiles,
	})
}

// RefreshAutoRouterRanking runs a leaderboard refresh now, as a system task so it
// takes the same lease as the scheduled one. It works while the periodic refresh
// is switched off.
func RefreshAutoRouterRanking(c *gin.Context) {
	if !autorouter_setting.GetAutoRouterSetting().Connections().RankKeysReady() {
		common.ApiErrorT(c, "The leaderboard keys are not configured. Set the Artificial Analysis key and either the Design Arena or the OpenRouter key first.")
		return
	}
	task, created, err := service.EnqueueSystemTask(model.SystemTaskTypeAutoRouterRank, nil)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if created {
		recordManageAudit(c, "autorouter.rank_refresh", nil)
	}
	common.ApiSuccess(c, gin.H{"task": task.ToResponse(), "created": created})
}

// RestoreAutoRouterRanking writes the embedded ranking back into the options.
// A refresh in flight would overwrite it a moment later, so it must finish first.
func RestoreAutoRouterRanking(c *gin.Context) {
	active, err := model.GetActiveSystemTask(model.SystemTaskTypeAutoRouterRank)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if active != nil {
		common.ApiErrorT(c, "A leaderboard refresh is running. Restore the built-in ranking after it finishes.")
		return
	}
	options, err := autorouter_setting.ShippedOptions()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.UpdateOptionsBulk(options); err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "autorouter.rank_restore", nil)
	common.ApiSuccess(c, nil)
}
