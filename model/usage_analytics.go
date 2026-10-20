package model

import (
	"fmt"
	"strings"
)

const (
	UsageAnalyticsGroupSummary      = "summary"
	UsageAnalyticsGroupModel        = "model"
	UsageAnalyticsGroupUser         = "user"
	UsageAnalyticsGroupUserModel    = "user_model"
	UsageAnalyticsGroupModelChannel = "model_channel"
)

// UsageMetrics counts consume requests separately from final failures.
type UsageMetrics struct {
	RequestCount     int64 `json:"request_count"`
	Quota            int64 `json:"quota"`
	TokenUsed        int64 `json:"token_used"`
	InputTokens      int64 `json:"input_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
	CacheReadTokens  int64 `json:"cache_read_tokens"`
	CacheWriteTokens int64 `json:"cache_write_tokens"`
	UseTimeTotal     int64 `json:"use_time_total"`
	UseTimeCount     int64 `json:"use_time_count"`
	StreamErrorCount int64 `json:"stream_error_count"`
	FailedCount      int64 `json:"failed_count"`
	TrackedCount     int64 `json:"tracked_count"`
	LastCalledAt     int64 `json:"last_called_at"`
}

const usageMetricsSelect = "COALESCE(SUM(quota_data.count), 0) AS request_count, " +
	"COALESCE(SUM(quota_data.quota), 0) AS quota, " +
	"COALESCE(SUM(quota_data.token_used), 0) AS token_used, " +
	"COALESCE(SUM(quota_data.input_tokens), 0) AS input_tokens, " +
	"COALESCE(SUM(quota_data.completion_tokens), 0) AS completion_tokens, " +
	"COALESCE(SUM(quota_data.cache_read_tokens), 0) AS cache_read_tokens, " +
	"COALESCE(SUM(quota_data.cache_write_tokens), 0) AS cache_write_tokens, " +
	"COALESCE(SUM(quota_data.use_time_total), 0) AS use_time_total, " +
	"COALESCE(SUM(quota_data.use_time_count), 0) AS use_time_count, " +
	"COALESCE(SUM(quota_data.stream_error_count), 0) AS stream_error_count, " +
	"COALESCE(SUM(quota_data.failed_count), 0) AS failed_count, " +
	"COALESCE(SUM(quota_data.tracked_count), 0) AS tracked_count, " +
	"COALESCE(MAX(quota_data.last_called_at), 0) AS last_called_at"

// Failure-only users are not active users.
const usageActiveUsersSelect = "COUNT(DISTINCT CASE WHEN quota_data.count > 0 THEN quota_data.user_id END)"

type UsageAnalyticsSummary struct {
	UsageMetrics
	ActiveUsers int64 `json:"active_users"`
	ModelCount  int64 `json:"model_count"`
}

// UsageAnalyticsRow uses pointers to omit unused dimensions while retaining zero values.
type UsageAnalyticsRow struct {
	UsageMetrics
	ModelName   *string `json:"model_name,omitempty"`
	UserID      *int    `json:"user_id,omitempty"`
	Username    *string `json:"username,omitempty"`
	DisplayName *string `json:"display_name,omitempty"`
	ChannelID   *int    `json:"channel_id,omitempty"`
	ChannelName *string `json:"channel_name,omitempty"`
	UserCount   *int64  `json:"user_count,omitempty"`
}

type UsageAnalyticsResult struct {
	Summary *UsageAnalyticsSummary `json:"summary"`
	Rows    []UsageAnalyticsRow    `json:"rows"`
}

type UsageAnalyticsParams struct {
	Start     int64
	End       int64
	GroupBy   string
	Username  string
	ModelName string
	// UserID > 0 restricts the query to one user (self analytics).
	UserID int
}

// GetUsageAnalytics aggregates hourly quota data in [Start, End].
func GetUsageAnalytics(params UsageAnalyticsParams) (*UsageAnalyticsResult, error) {
	query := DB.Table("quota_data").
		Where("quota_data.created_at >= ? AND quota_data.created_at <= ?", params.Start, params.End)
	if params.UserID > 0 {
		query = query.Where("quota_data.user_id = ?", params.UserID)
	}
	if username := strings.TrimSpace(params.Username); username != "" {
		query = query.Where("quota_data.username = ?", username)
	}
	if modelName := strings.TrimSpace(params.ModelName); modelName != "" {
		query = query.Where("quota_data.model_name = ?", modelName)
	}

	result := &UsageAnalyticsResult{Rows: []UsageAnalyticsRow{}}
	switch params.GroupBy {
	case UsageAnalyticsGroupSummary:
		summary := &UsageAnalyticsSummary{}
		err := query.
			Select(usageMetricsSelect + ", " + usageActiveUsersSelect + " AS active_users, " +
				"COUNT(DISTINCT CASE WHEN quota_data.count > 0 THEN quota_data.model_name END) AS model_count").
			Scan(summary).Error
		if err != nil {
			return nil, err
		}
		result.Summary = summary
		return result, nil
	case UsageAnalyticsGroupModel:
		query = query.
			Select("quota_data.model_name AS model_name, " + usageActiveUsersSelect + " AS user_count, " + usageMetricsSelect).
			Group("quota_data.model_name").
			Order("request_count DESC, model_name ASC")
	case UsageAnalyticsGroupUser:
		query = query.
			Select("quota_data.user_id AS user_id, quota_data.username AS username, " +
				"COALESCE(MAX(users.display_name), '') AS display_name, " + usageMetricsSelect).
			Joins("LEFT JOIN users ON users.id = quota_data.user_id").
			Group("quota_data.user_id, quota_data.username").
			Order("quota DESC, username ASC")
	case UsageAnalyticsGroupUserModel:
		query = query.
			Select("quota_data.user_id AS user_id, quota_data.username AS username, " +
				"COALESCE(MAX(users.display_name), '') AS display_name, quota_data.model_name AS model_name, " + usageMetricsSelect).
			Joins("LEFT JOIN users ON users.id = quota_data.user_id").
			Group("quota_data.user_id, quota_data.username, quota_data.model_name").
			Order("quota DESC, username ASC, model_name ASC")
	case UsageAnalyticsGroupModelChannel:
		query = query.
			Select("quota_data.model_name AS model_name, quota_data.channel_id AS channel_id, " +
				"COALESCE(MAX(channels.name), '') AS channel_name, " + usageMetricsSelect).
			Joins("LEFT JOIN channels ON channels.id = quota_data.channel_id").
			Group("quota_data.model_name, quota_data.channel_id").
			Order("request_count DESC, model_name ASC, channel_id ASC")
	default:
		return nil, fmt.Errorf("unsupported group_by: %s", params.GroupBy)
	}
	if err := query.Scan(&result.Rows).Error; err != nil {
		return nil, err
	}
	return result, nil
}
