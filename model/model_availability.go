package model

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
)

type ModelAvailability struct {
	ModelName        string `json:"model_name"`
	SuccessCount     int64  `json:"success_count"`
	StreamErrorCount int64  `json:"stream_error_count"`
	FailedCount      int64  `json:"failed_count"`
	TotalTokens      int64  `json:"total_tokens"`
	LastCalledAt     int64  `json:"last_called_at"`
}

type ModelAvailabilityWindow struct {
	Minutes         int                 `json:"minutes"`
	StartTimestamp  int64               `json:"start_timestamp"`
	EndTimestamp    int64               `json:"end_timestamp"`
	Models          []ModelAvailability `json:"models"`
	ErrorLogEnabled bool                `json:"error_log_enabled"`
}

const modelAvailabilityCacheTTL = 30 * time.Second

type modelAvailabilityCacheEntry struct {
	window    ModelAvailabilityWindow
	expiresAt time.Time
}

var (
	modelAvailabilityCacheMu sync.Mutex
	modelAvailabilityCache   = make(map[int]modelAvailabilityCacheEntry)
)

// GetRecentModelAvailability filters a cached, minute-aligned window.
func GetRecentModelAvailability(minutes int, allowedModels map[string]struct{}) (ModelAvailabilityWindow, error) {
	now := time.Now()
	end := now.Unix() / 60 * 60

	modelAvailabilityCacheMu.Lock()
	entry, cached := modelAvailabilityCache[minutes]
	modelAvailabilityCacheMu.Unlock()
	if !cached || !now.Before(entry.expiresAt) || entry.window.EndTimestamp != end {
		start := end - int64(minutes)*60
		models, err := queryModelAvailability(start, end)
		if err != nil {
			return ModelAvailabilityWindow{}, err
		}
		entry = modelAvailabilityCacheEntry{
			window: ModelAvailabilityWindow{
				Minutes:        minutes,
				StartTimestamp: start,
				EndTimestamp:   end,
				Models:         models,
			},
			expiresAt: now.Add(modelAvailabilityCacheTTL),
		}
		modelAvailabilityCacheMu.Lock()
		modelAvailabilityCache[minutes] = entry
		modelAvailabilityCacheMu.Unlock()
	}

	// Do not let caller filters mutate the shared cache.
	window := entry.window
	window.Models = make([]ModelAvailability, 0, len(entry.window.Models))
	for _, item := range entry.window.Models {
		if allowedModels != nil {
			if _, allowed := allowedModels[item.ModelName]; !allowed {
				continue
			}
		}
		window.Models = append(window.Models, item)
	}
	return window, nil
}

// queryModelAvailability aggregates log outcomes in [startTimestamp, endTimestamp].
func queryModelAvailability(startTimestamp, endTimestamp int64) ([]ModelAvailability, error) {
	statusExpr, ok := logStreamStatusExpr("other")
	if !ok {
		return nil, fmt.Errorf("model availability unsupported on log database type: %s", common.LogDatabaseType())
	}
	streamErrorExpr := "CASE WHEN " + statusExpr + " = 'error' THEN 1 ELSE 0 END"

	var consumed []struct {
		ModelName        string
		ConsumeCount     int64
		StreamErrorCount int64
		TotalTokens      int64
		LastCalledAt     int64
	}
	err := LOG_DB.Table("logs").
		Select("model_name, COUNT(*) AS consume_count, "+
			"COALESCE(SUM("+streamErrorExpr+"), 0) AS stream_error_count, "+
			"COALESCE(SUM(prompt_tokens + completion_tokens), 0) AS total_tokens, "+
			"MAX(created_at) AS last_called_at").
		Where("type = ?", LogTypeConsume).
		Where("created_at >= ? AND created_at <= ?", startTimestamp, endTimestamp).
		Where("model_name <> ''").
		Group("model_name").
		Scan(&consumed).Error
	if err != nil {
		return nil, err
	}

	// A later consume log also excludes an earlier error from failures.
	succeededRequests := LOG_DB.Table("logs").
		Select("request_id").
		Where("type = ?", LogTypeConsume).
		Where("created_at >= ?", startTimestamp)
	var failed []struct {
		ModelName    string
		FailedCount  int64
		LastCalledAt int64
	}
	err = LOG_DB.Table("logs").
		Select("model_name, COUNT(DISTINCT request_id) AS failed_count, MAX(created_at) AS last_called_at").
		Where("type = ?", LogTypeError).
		Where("created_at >= ? AND created_at <= ?", startTimestamp, endTimestamp).
		Where("model_name <> ''").
		Where("request_id NOT IN (?)", succeededRequests).
		Group("model_name").
		Scan(&failed).Error
	if err != nil {
		return nil, err
	}

	byModel := make(map[string]*ModelAvailability, len(consumed)+len(failed))
	for _, row := range consumed {
		byModel[row.ModelName] = &ModelAvailability{
			ModelName:        row.ModelName,
			SuccessCount:     row.ConsumeCount - row.StreamErrorCount,
			StreamErrorCount: row.StreamErrorCount,
			TotalTokens:      row.TotalTokens,
			LastCalledAt:     row.LastCalledAt,
		}
	}
	for _, row := range failed {
		item, exists := byModel[row.ModelName]
		if !exists {
			item = &ModelAvailability{ModelName: row.ModelName}
			byModel[row.ModelName] = item
		}
		item.FailedCount = row.FailedCount
		item.LastCalledAt = max(item.LastCalledAt, row.LastCalledAt)
	}

	models := make([]ModelAvailability, 0, len(byModel))
	for _, item := range byModel {
		models = append(models, *item)
	}
	sort.Slice(models, func(i, j int) bool {
		return models[i].ModelName < models[j].ModelName
	})
	return models, nil
}
