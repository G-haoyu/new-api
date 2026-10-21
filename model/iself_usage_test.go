package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetIselfUsageAnalytics(t *testing.T) {
	if common.UsingLogDatabase(common.DatabaseTypeSQLite) {
		var version string
		require.NoError(t, LOG_DB.Raw("SELECT sqlite_version()").Scan(&version).Error)
		t.Logf("SQLite %s", version)
	}
	clearDialectTestLogs(t)
	t.Cleanup(func() { clearDialectTestLogs(t) })
	require.NoError(t, LOG_DB.Create(&[]Log{
		{UserId: 99, Username: "gateway", IselfEmail: " Alice@Example.com ", ModelName: "model-a", Type: LogTypeConsume, CreatedAt: 1000, Quota: 100, PromptTokens: 10, CompletionTokens: 5},
		{UserId: 98, Username: "other-gateway", IselfEmail: "alice@example.com", ModelName: "model-a", Type: LogTypeConsume, CreatedAt: 2000, Quota: 50, PromptTokens: 20, CompletionTokens: 10},
		{UserId: 99, Username: "gateway", IselfEmail: "alice@example.com", ModelName: "model-b", Type: LogTypeConsume, CreatedAt: 1500, Quota: 20, PromptTokens: 3, CompletionTokens: 2},
		{UserId: 99, Username: "gateway", IselfEmail: "bob@example.com", ModelName: "model-a", Type: LogTypeConsume, CreatedAt: 1500, Quota: 40, PromptTokens: 4, CompletionTokens: 2},
		{UserId: 99, Username: "gateway", IselfEmail: "", ModelName: "direct", Type: LogTypeConsume, CreatedAt: 1500, Quota: 999},
		{UserId: 99, Username: "gateway", IselfEmail: "   ", ModelName: "missing-email", Type: LogTypeConsume, CreatedAt: 1500, Quota: 999},
		{UserId: 99, Username: "gateway", IselfEmail: "alice@example.com", ModelName: "error", Type: LogTypeError, CreatedAt: 1500, Quota: 999},
		{UserId: 99, Username: "gateway", IselfEmail: "alice@example.com", ModelName: "refund", Type: LogTypeRefund, CreatedAt: 1500, Quota: 999},
		{UserId: 99, Username: "gateway", IselfEmail: "alice@example.com", ModelName: "before", Type: LogTypeConsume, CreatedAt: 999, Quota: 999},
		{UserId: 99, Username: "gateway", IselfEmail: "alice@example.com", ModelName: "after", Type: LogTypeConsume, CreatedAt: 2001, Quota: 999},
	}).Error)

	rows, err := GetIselfUsageAnalytics(IselfUsageParams{Start: 1000, End: 2000})
	require.NoError(t, err)
	require.Len(t, rows, 3)
	assert.Equal(t, []IselfUsageRow{
		{Email: "alice@example.com", Source: "iself", ModelName: "model-a", RequestCount: 2, Quota: 150, InputTokens: 30, CompletionTokens: 15, TokenUsed: 45, LastCalledAt: 2000},
		{Email: "bob@example.com", Source: "iself", ModelName: "model-a", RequestCount: 1, Quota: 40, InputTokens: 4, CompletionTokens: 2, TokenUsed: 6, LastCalledAt: 1500},
		{Email: "alice@example.com", Source: "iself", ModelName: "model-b", RequestCount: 1, Quota: 20, InputTokens: 3, CompletionTokens: 2, TokenUsed: 5, LastCalledAt: 1500},
	}, rows)

	filtered, err := GetIselfUsageAnalytics(IselfUsageParams{Start: 1000, End: 2000, Username: " other-gateway ", ModelName: " model-a "})
	require.NoError(t, err)
	assert.Equal(t, []IselfUsageRow{
		{Email: "alice@example.com", Source: "iself", ModelName: "model-a", RequestCount: 1, Quota: 50, InputTokens: 20, CompletionTokens: 10, TokenUsed: 30, LastCalledAt: 2000},
	}, filtered)

	empty, err := GetIselfUsageAnalytics(IselfUsageParams{Start: 3000, End: 4000})
	require.NoError(t, err)
	assert.Equal(t, []IselfUsageRow{}, empty)

	// Match the bound email across forwarding accounts.
	require.NoError(t, DB.Create(&User{Id: 71001, Username: "iself-alice", Password: "test-password", Email: " ALICE@example.com ", AffCode: "iself-alice"}).Error)
	t.Cleanup(func() { require.NoError(t, DB.Unscoped().Delete(&User{}, 71001).Error) })
	self, err := GetIselfUsageAnalytics(IselfUsageParams{Start: 1000, End: 2000, UserID: 71001, Username: "other-gateway"})
	require.NoError(t, err)
	assert.Equal(t, []IselfUsageRow{rows[0], rows[2]}, self)
	missingUser, err := GetIselfUsageAnalytics(IselfUsageParams{Start: 1000, End: 2000, UserID: 71002})
	require.NoError(t, err)
	assert.Equal(t, []IselfUsageRow{}, missingUser)
}
