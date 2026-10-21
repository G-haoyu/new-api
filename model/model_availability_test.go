package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQueryModelAvailabilityCountsFinalOutcomes(t *testing.T) {
	db := useSQLiteLogTestDB(t)
	streamError := common.MapToJsonStr(map[string]interface{}{
		"stream_status": map[string]interface{}{"status": "error"},
	})
	streamOk := common.MapToJsonStr(map[string]interface{}{
		"stream_status": map[string]interface{}{"status": "ok"},
	})
	require.NoError(t, db.Create(&[]Log{
		// Two failed attempts followed by a successful retry.
		{RequestId: "retry", Type: LogTypeError, ModelName: "m-retry", CreatedAt: 1100},
		{RequestId: "retry", Type: LogTypeError, ModelName: "m-retry", CreatedAt: 1101},
		{RequestId: "retry", Type: LogTypeConsume, ModelName: "m-retry", CreatedAt: 1102, PromptTokens: 10, CompletionTokens: 5},
		{RequestId: "fail", Type: LogTypeError, ModelName: "m-fail", CreatedAt: 1200},
		{RequestId: "fail", Type: LogTypeError, ModelName: "m-fail", CreatedAt: 1201},
		{RequestId: "stream-error", Type: LogTypeConsume, ModelName: "m-stream", CreatedAt: 1300, PromptTokens: 3, CompletionTokens: 4, Other: streamError},
		{RequestId: "stream-ok", Type: LogTypeConsume, ModelName: "m-stream", CreatedAt: 1301, PromptTokens: 1, CompletionTokens: 2, Other: streamOk},
		{RequestId: "legacy", Type: LogTypeConsume, ModelName: "m-stream", CreatedAt: 1302, PromptTokens: 5},
		{RequestId: "no-model", Type: LogTypeConsume, ModelName: "", CreatedAt: 1400},
		{RequestId: "refund", Type: LogTypeRefund, ModelName: "m-stream", CreatedAt: 1500},
	}).Error)

	models, err := queryModelAvailability(1000, 2000)
	require.NoError(t, err)
	assert.Equal(t, []ModelAvailability{
		{ModelName: "m-fail", FailedCount: 1, LastCalledAt: 1201},
		{ModelName: "m-retry", SuccessCount: 1, TotalTokens: 15, LastCalledAt: 1102},
		{ModelName: "m-stream", SuccessCount: 2, StreamErrorCount: 1, TotalTokens: 15, LastCalledAt: 1302},
	}, models)
}

func TestQueryModelAvailabilityUsesClosedWindow(t *testing.T) {
	db := useSQLiteLogTestDB(t)
	require.NoError(t, db.Create(&[]Log{
		{RequestId: "before", Type: LogTypeConsume, ModelName: "m", CreatedAt: 999, PromptTokens: 100},
		{RequestId: "at-start", Type: LogTypeConsume, ModelName: "m", CreatedAt: 1000, PromptTokens: 1},
		{RequestId: "at-end", Type: LogTypeConsume, ModelName: "m", CreatedAt: 2000, PromptTokens: 2},
		{RequestId: "after", Type: LogTypeConsume, ModelName: "m", CreatedAt: 2001, PromptTokens: 100},
		{RequestId: "fail-before", Type: LogTypeError, ModelName: "m", CreatedAt: 999},
		{RequestId: "fail-at-start", Type: LogTypeError, ModelName: "m", CreatedAt: 1000},
		{RequestId: "fail-at-end", Type: LogTypeError, ModelName: "m", CreatedAt: 2000},
		{RequestId: "fail-after", Type: LogTypeError, ModelName: "m", CreatedAt: 2001},
	}).Error)

	models, err := queryModelAvailability(1000, 2000)
	require.NoError(t, err)
	assert.Equal(t, []ModelAvailability{
		{ModelName: "m", SuccessCount: 2, FailedCount: 2, TotalTokens: 3, LastCalledAt: 2000},
	}, models)
}

func TestQueryModelAvailabilityIgnoresRetriesThatSucceedAfterTheWindow(t *testing.T) {
	db := useSQLiteLogTestDB(t)
	require.NoError(t, db.Create(&[]Log{
		// The retry succeeds after the window ends.
		{RequestId: "late-success", Type: LogTypeError, ModelName: "m", CreatedAt: 1990},
		{RequestId: "late-success", Type: LogTypeConsume, ModelName: "m", CreatedAt: 2040, PromptTokens: 9},
		{RequestId: "real-fail", Type: LogTypeError, ModelName: "m", CreatedAt: 1500},
	}).Error)

	models, err := queryModelAvailability(1000, 2000)
	require.NoError(t, err)
	assert.Equal(t, []ModelAvailability{
		{ModelName: "m", FailedCount: 1, LastCalledAt: 1500},
	}, models)
}

func TestGetRecentModelAvailabilityFiltersAfterCache(t *testing.T) {
	db := useSQLiteLogTestDB(t)
	resetCache := func() {
		modelAvailabilityCacheMu.Lock()
		clear(modelAvailabilityCache)
		modelAvailabilityCacheMu.Unlock()
	}
	resetCache()
	t.Cleanup(resetCache)

	createdAt := time.Now().Unix() - 120
	require.NoError(t, db.Create(&[]Log{
		{RequestId: "a", Type: LogTypeConsume, ModelName: "m-a", CreatedAt: createdAt},
		{RequestId: "b", Type: LogTypeConsume, ModelName: "m-b", CreatedAt: createdAt},
	}).Error)

	subset, err := GetRecentModelAvailability(10, map[string]struct{}{"m-a": {}})
	require.NoError(t, err)
	assert.Equal(t, 10, subset.Minutes)
	assert.Equal(t, int64(600), subset.EndTimestamp-subset.StartTimestamp)
	assert.Zero(t, subset.EndTimestamp%60)
	assert.Equal(t, []ModelAvailability{{ModelName: "m-a", SuccessCount: 1, LastCalledAt: createdAt}}, subset.Models)

	empty, err := GetRecentModelAvailability(10, map[string]struct{}{})
	require.NoError(t, err)
	assert.NotNil(t, empty.Models)
	assert.Empty(t, empty.Models)

	// Caller filters must not alter the shared cache.
	all, err := GetRecentModelAvailability(10, nil)
	require.NoError(t, err)
	assert.Equal(t, []ModelAvailability{
		{ModelName: "m-a", SuccessCount: 1, LastCalledAt: createdAt},
		{ModelName: "m-b", SuccessCount: 1, LastCalledAt: createdAt},
	}, all.Models)
}
