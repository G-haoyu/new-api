package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func resetUsageAnalyticsState(t *testing.T) {
	t.Helper()
	truncateTables(t)
	for _, table := range []string{"quota_data", "logs", "users", "channels"} {
		require.NoError(t, DB.Exec("DELETE FROM "+table).Error)
	}
	CacheQuotaDataLock.Lock()
	CacheQuotaData = make(map[string]*QuotaData)
	CacheQuotaDataLock.Unlock()
	t.Cleanup(func() {
		CacheQuotaDataLock.Lock()
		CacheQuotaData = make(map[string]*QuotaData)
		CacheQuotaDataLock.Unlock()
	})
}

const (
	analyticsH1 = int64(7200)
	analyticsH2 = analyticsH1 + 3600
	analyticsH3 = analyticsH2 + 3600
)

func seedUsageAnalyticsFixture(t *testing.T) {
	t.Helper()
	require.NoError(t, DB.Create(&User{Id: 1, Username: "alice", Password: "password-1", DisplayName: "Alice A", AffCode: "aff-alice"}).Error)
	require.NoError(t, DB.Create(&User{Id: 2, Username: "bob", Password: "password-2", AffCode: "aff-bob"}).Error)
	require.NoError(t, DB.Create(&Channel{Id: 1, Name: "east", Key: "k1"}).Error)
	require.NoError(t, DB.Create(&Channel{Id: 2, Name: "west", Key: "k2"}).Error)

	rows := []QuotaData{
		{UserID: 1, Username: "alice", ModelName: "gpt-a", ChannelID: 1, CreatedAt: analyticsH1, Count: 5, Quota: 500, TokenUsed: 1000,
			InputTokens: 800, CompletionTokens: 200, CacheReadTokens: 300, CacheWriteTokens: 50, UseTimeTotal: 20, UseTimeCount: 4,
			StreamErrorCount: 1, TrackedCount: 5, LastCalledAt: analyticsH1 + 100},
		{UserID: 1, Username: "alice", ModelName: "gpt-b", ChannelID: 2, CreatedAt: analyticsH2, Count: 3, Quota: 200, TokenUsed: 600,
			InputTokens: 500, CompletionTokens: 100, CacheReadTokens: 100, UseTimeTotal: 9, UseTimeCount: 3, TrackedCount: 3, LastCalledAt: analyticsH2 + 200},
		// Consumption and final failure share an hourly bucket.
		{UserID: 2, Username: "bob", ModelName: "gpt-a", ChannelID: 1, CreatedAt: analyticsH2, Count: 3, Quota: 150, TokenUsed: 300,
			InputTokens: 250, CompletionTokens: 50, CacheWriteTokens: 10, UseTimeTotal: 6, UseTimeCount: 3, FailedCount: 1,
			TrackedCount: 3, LastCalledAt: analyticsH2 + 300},
		// Legacy row without detailed counters.
		{UserID: 2, Username: "bob", ModelName: "gpt-a", ChannelID: 2, CreatedAt: analyticsH1, Count: 4, Quota: 200, TokenUsed: 300},
		// Failure-only row with no matching user or channel.
		{UserID: 3, Username: "carol", ModelName: "gpt-c", ChannelID: 9, CreatedAt: analyticsH2, FailedCount: 2, LastCalledAt: analyticsH2 + 400},
		{UserID: 2, Username: "bob", ModelName: "gpt-b", ChannelID: 1, CreatedAt: analyticsH3, Count: 2, Quota: 80, TokenUsed: 100,
			InputTokens: 120, CompletionTokens: 80, UseTimeTotal: 4, UseTimeCount: 2, TrackedCount: 2, LastCalledAt: analyticsH3 + 50},
		{UserID: 1, Username: "alice", ModelName: "gpt-a", ChannelID: 1, CreatedAt: analyticsH1 - 3600, Count: 100, Quota: 9999, TokenUsed: 9999, LastCalledAt: analyticsH1 - 3500},
		{UserID: 1, Username: "alice", ModelName: "gpt-a", ChannelID: 1, CreatedAt: analyticsH3 + 3600, Count: 100, Quota: 9999, TokenUsed: 9999, LastCalledAt: analyticsH3 + 3700},
	}
	for i := range rows {
		require.NoError(t, DB.Create(&rows[i]).Error)
	}
}

func TestGetUsageAnalyticsSummaryAggregatesClosedRange(t *testing.T) {
	resetUsageAnalyticsState(t)
	seedUsageAnalyticsFixture(t)

	result, err := GetUsageAnalytics(UsageAnalyticsParams{Start: analyticsH1, End: analyticsH3, GroupBy: UsageAnalyticsGroupSummary})
	require.NoError(t, err)
	require.NotNil(t, result.Summary)
	assert.Empty(t, result.Rows)
	assert.Equal(t, UsageAnalyticsSummary{
		UsageMetrics: UsageMetrics{
			RequestCount: 17, Quota: 1130, TokenUsed: 2300,
			InputTokens: 1670, CompletionTokens: 430, CacheReadTokens: 400, CacheWriteTokens: 60,
			UseTimeTotal: 39, UseTimeCount: 12, StreamErrorCount: 1, FailedCount: 3,
			// Legacy requests are excluded from tracked_count.
			TrackedCount: 13, LastCalledAt: analyticsH3 + 50,
		},
		// Failure-only rows do not count as active users or used models.
		ActiveUsers: 2,
		ModelCount:  2,
	}, *result.Summary)

	inner, err := GetUsageAnalytics(UsageAnalyticsParams{Start: analyticsH1 + 1, End: analyticsH3 - 1, GroupBy: UsageAnalyticsGroupSummary})
	require.NoError(t, err)
	assert.Equal(t, int64(6), inner.Summary.RequestCount)
	assert.Equal(t, int64(350), inner.Summary.Quota)
	assert.Equal(t, int64(3), inner.Summary.FailedCount)
	assert.Equal(t, int64(2), inner.Summary.ActiveUsers)
}

func TestGetUsageAnalyticsSummaryAppliesFilters(t *testing.T) {
	resetUsageAnalyticsState(t)
	seedUsageAnalyticsFixture(t)

	tests := []struct {
		name         string
		params       UsageAnalyticsParams
		requestCount int64
		quota        int64
		failedCount  int64
		trackedCount int64
		activeUsers  int64
		modelCount   int64
	}{
		{name: "username", params: UsageAnalyticsParams{Username: "bob"}, requestCount: 9, quota: 430, failedCount: 1, trackedCount: 5, activeUsers: 1, modelCount: 2},
		{name: "model", params: UsageAnalyticsParams{ModelName: "gpt-a"}, requestCount: 12, quota: 850, failedCount: 1, trackedCount: 8, activeUsers: 2, modelCount: 1},
		{name: "username and model", params: UsageAnalyticsParams{Username: "bob", ModelName: "gpt-b"}, requestCount: 2, quota: 80, trackedCount: 2, activeUsers: 1, modelCount: 1},
		{name: "user id", params: UsageAnalyticsParams{UserID: 1}, requestCount: 8, quota: 700, trackedCount: 8, activeUsers: 1, modelCount: 2},
		{name: "model prefix is not a match", params: UsageAnalyticsParams{ModelName: "gpt"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := tt.params
			params.Start, params.End, params.GroupBy = analyticsH1, analyticsH3, UsageAnalyticsGroupSummary
			result, err := GetUsageAnalytics(params)
			require.NoError(t, err)
			require.NotNil(t, result.Summary)
			assert.Equal(t, tt.requestCount, result.Summary.RequestCount)
			assert.Equal(t, tt.quota, result.Summary.Quota)
			assert.Equal(t, tt.failedCount, result.Summary.FailedCount)
			assert.Equal(t, tt.trackedCount, result.Summary.TrackedCount)
			assert.Equal(t, tt.activeUsers, result.Summary.ActiveUsers)
			assert.Equal(t, tt.modelCount, result.Summary.ModelCount)
		})
	}
}

func TestGetUsageAnalyticsGroupByModel(t *testing.T) {
	resetUsageAnalyticsState(t)
	seedUsageAnalyticsFixture(t)

	result, err := GetUsageAnalytics(UsageAnalyticsParams{Start: analyticsH1, End: analyticsH3, GroupBy: UsageAnalyticsGroupModel})
	require.NoError(t, err)
	assert.Nil(t, result.Summary)
	assert.Equal(t, []UsageAnalyticsRow{
		{
			UsageMetrics: UsageMetrics{RequestCount: 12, Quota: 850, TokenUsed: 1600, InputTokens: 1050, CompletionTokens: 250,
				CacheReadTokens: 300, CacheWriteTokens: 60, UseTimeTotal: 26, UseTimeCount: 7, StreamErrorCount: 1, FailedCount: 1,
				TrackedCount: 8, LastCalledAt: analyticsH2 + 300},
			ModelName: common.GetPointer("gpt-a"),
			UserCount: common.GetPointer(int64(2)),
		},
		{
			UsageMetrics: UsageMetrics{RequestCount: 5, Quota: 280, TokenUsed: 700, InputTokens: 620, CompletionTokens: 180,
				CacheReadTokens: 100, UseTimeTotal: 13, UseTimeCount: 5, TrackedCount: 5, LastCalledAt: analyticsH3 + 50},
			ModelName: common.GetPointer("gpt-b"),
			UserCount: common.GetPointer(int64(2)),
		},
		{
			UsageMetrics: UsageMetrics{FailedCount: 2, LastCalledAt: analyticsH2 + 400},
			ModelName:    common.GetPointer("gpt-c"),
			UserCount:    common.GetPointer(int64(0)),
		},
	}, result.Rows)
}

func TestGetUsageAnalyticsGroupByUserJoinsDisplayName(t *testing.T) {
	resetUsageAnalyticsState(t)
	seedUsageAnalyticsFixture(t)

	result, err := GetUsageAnalytics(UsageAnalyticsParams{Start: analyticsH1, End: analyticsH3, GroupBy: UsageAnalyticsGroupUser})
	require.NoError(t, err)
	require.Len(t, result.Rows, 3)

	type userRow struct {
		userID       int
		username     string
		displayName  string
		requestCount int64
		quota        int64
		failedCount  int64
		trackedCount int64
	}
	got := make([]userRow, 0, len(result.Rows))
	for _, row := range result.Rows {
		require.NotNil(t, row.UserID)
		require.NotNil(t, row.Username)
		require.NotNil(t, row.DisplayName)
		assert.Nil(t, row.ModelName)
		assert.Nil(t, row.ChannelID)
		assert.Nil(t, row.UserCount)
		got = append(got, userRow{*row.UserID, *row.Username, *row.DisplayName, row.RequestCount, row.Quota, row.FailedCount, row.TrackedCount})
	}
	assert.Equal(t, []userRow{
		{userID: 1, username: "alice", displayName: "Alice A", requestCount: 8, quota: 700, trackedCount: 8},
		{userID: 2, username: "bob", displayName: "", requestCount: 9, quota: 430, failedCount: 1, trackedCount: 5},
		// Missing users retain an empty display name.
		{userID: 3, username: "carol", displayName: "", failedCount: 2},
	}, got)
}

func TestGetUsageAnalyticsGroupByUserModel(t *testing.T) {
	resetUsageAnalyticsState(t)
	seedUsageAnalyticsFixture(t)

	result, err := GetUsageAnalytics(UsageAnalyticsParams{Start: analyticsH1, End: analyticsH3, GroupBy: UsageAnalyticsGroupUserModel})
	require.NoError(t, err)

	type userModelRow struct {
		username     string
		displayName  string
		modelName    string
		requestCount int64
		quota        int64
	}
	got := make([]userModelRow, 0, len(result.Rows))
	for _, row := range result.Rows {
		require.NotNil(t, row.UserID)
		require.NotNil(t, row.Username)
		require.NotNil(t, row.DisplayName)
		require.NotNil(t, row.ModelName)
		got = append(got, userModelRow{*row.Username, *row.DisplayName, *row.ModelName, row.RequestCount, row.Quota})
	}
	assert.Equal(t, []userModelRow{
		{username: "alice", displayName: "Alice A", modelName: "gpt-a", requestCount: 5, quota: 500},
		{username: "bob", modelName: "gpt-a", requestCount: 7, quota: 350},
		{username: "alice", displayName: "Alice A", modelName: "gpt-b", requestCount: 3, quota: 200},
		{username: "bob", modelName: "gpt-b", requestCount: 2, quota: 80},
		{username: "carol", modelName: "gpt-c"},
	}, got)
}

func TestGetUsageAnalyticsGroupByModelChannelJoinsChannelName(t *testing.T) {
	resetUsageAnalyticsState(t)
	seedUsageAnalyticsFixture(t)

	result, err := GetUsageAnalytics(UsageAnalyticsParams{Start: analyticsH1, End: analyticsH3, GroupBy: UsageAnalyticsGroupModelChannel})
	require.NoError(t, err)

	type modelChannelRow struct {
		modelName    string
		channelID    int
		channelName  string
		requestCount int64
		failedCount  int64
	}
	got := make([]modelChannelRow, 0, len(result.Rows))
	for _, row := range result.Rows {
		require.NotNil(t, row.ModelName)
		require.NotNil(t, row.ChannelID)
		require.NotNil(t, row.ChannelName)
		assert.Nil(t, row.UserID)
		assert.Nil(t, row.Username)
		got = append(got, modelChannelRow{*row.ModelName, *row.ChannelID, *row.ChannelName, row.RequestCount, row.FailedCount})
	}
	assert.Equal(t, []modelChannelRow{
		{modelName: "gpt-a", channelID: 1, channelName: "east", requestCount: 8, failedCount: 1},
		{modelName: "gpt-a", channelID: 2, channelName: "west", requestCount: 4},
		{modelName: "gpt-b", channelID: 2, channelName: "west", requestCount: 3},
		{modelName: "gpt-b", channelID: 1, channelName: "east", requestCount: 2},
		// Missing channels retain an empty name.
		{modelName: "gpt-c", channelID: 9, channelName: "", failedCount: 2},
	}, got)
}

func TestGetPeakTPMTakesHourlyMaxOfMinuteTotals(t *testing.T) {
	resetUsageAnalyticsState(t)
	hour := int64(72000)
	logs := []Log{
		// Sum different users in the same minute before taking the hourly peak.
		{Username: "alice", ModelName: "gpt-a", Type: LogTypeConsume, CreatedAt: hour + 10, PromptTokens: 100, CompletionTokens: 50},
		{Username: "bob", ModelName: "gpt-b", Type: LogTypeConsume, CreatedAt: hour + 59, PromptTokens: 900, CompletionTokens: 100},
		{Username: "alice", ModelName: "gpt-a", Type: LogTypeConsume, CreatedAt: hour + 300, PromptTokens: 200, CompletionTokens: 100},
		{Username: "alice", ModelName: "gpt-a", Type: LogTypeConsume, CreatedAt: hour + 330, PromptTokens: 150, CompletionTokens: 50},
		{Username: "alice", ModelName: "gpt-a", Type: LogTypeError, CreatedAt: hour + 300, PromptTokens: 99999},
		{Username: "alice", ModelName: "gpt-a", Type: LogTypeConsume, CreatedAt: hour + 3720, PromptTokens: 80, CompletionTokens: 20},
		{Username: "alice", ModelName: "gpt-a", Type: LogTypeConsume, CreatedAt: hour - 1, PromptTokens: 99999},
		{Username: "alice", ModelName: "gpt-a", Type: LogTypeConsume, CreatedAt: hour + 3721, PromptTokens: 99999},
	}
	for i := range logs {
		require.NoError(t, LOG_DB.Create(&logs[i]).Error)
	}

	tests := []struct {
		name     string
		params   PeakTPMParams
		expected []PeakTPMPoint
	}{
		{name: "all traffic", expected: []PeakTPMPoint{{Hour: hour, PeakTPM: 1150}, {Hour: hour + 3600, PeakTPM: 100}}},
		{name: "username", params: PeakTPMParams{Username: "alice"}, expected: []PeakTPMPoint{{Hour: hour, PeakTPM: 500}, {Hour: hour + 3600, PeakTPM: 100}}},
		{name: "model", params: PeakTPMParams{ModelName: "gpt-b"}, expected: []PeakTPMPoint{{Hour: hour, PeakTPM: 1000}}},
		{name: "no traffic", params: PeakTPMParams{Username: "alice", ModelName: "gpt-b"}, expected: []PeakTPMPoint{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := tt.params
			params.Start, params.End = hour, hour+3720
			points, err := GetPeakTPM(params)
			require.NoError(t, err)
			require.NotNil(t, points)
			assert.Equal(t, tt.expected, points)
		})
	}
}
