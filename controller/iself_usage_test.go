package controller

import (
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type iselfUsageResponse struct {
	Success bool                  `json:"success"`
	Message string                `json:"message"`
	Data    []model.IselfUsageRow `json:"data"`
}

func TestSelfIselfUsageScopesBoundEmailAndIgnoresClientIdentity(t *testing.T) {
	setupUsageAnalyticsControllerTestDB(t)
	require.NoError(t, model.DB.Create(&[]model.User{
		{Id: 1, Username: "alice", Password: "test-password", Email: " Alice@Example.com ", AffCode: "alice"},
		{Id: 2, Username: "bob", Password: "test-password", Email: "bob@example.com", AffCode: "bob"},
		{Id: 3, Username: "unbound", Password: "test-password", AffCode: "unbound"},
		{Id: 4, Username: "unused", Password: "test-password", Email: "unused@example.com", AffCode: "unused"},
	}).Error)
	require.NoError(t, model.LOG_DB.Create(&[]model.Log{
		{UserId: 99, Username: "gateway", IselfEmail: "alice@example.com", Type: model.LogTypeConsume, ModelName: "model-a", CreatedAt: 1000, Quota: 20, PromptTokens: 6, CompletionTokens: 4},
		{UserId: 99, Username: "gateway", IselfEmail: "bob@example.com", Type: model.LogTypeConsume, ModelName: "model-b", CreatedAt: 1000, Quota: 80},
		{UserId: 1, Username: "alice", Type: model.LogTypeConsume, ModelName: "direct", CreatedAt: 1000, Quota: 999},
	}).Error)
	const target = "/api/data/iself/self?start_timestamp=1&end_timestamp=2000&username=bob&email=bob@example.com&iself_email=bob@example.com&user_id=2"
	self := serveUsageAnalytics[iselfUsageResponse](t, GetSelfIselfUsageAnalytics, 1, target)
	require.True(t, self.Success, self.Message)
	assert.Equal(t, []model.IselfUsageRow{
		{Email: "alice@example.com", Source: "iself", ModelName: "model-a", RequestCount: 1, Quota: 20, InputTokens: 6, CompletionTokens: 4, TokenUsed: 10, LastCalledAt: 1000},
	}, self.Data)
	for _, id := range []int{3, 4, 999} {
		payload := serveUsageAnalytics[iselfUsageResponse](t, GetSelfIselfUsageAnalytics, id, target)
		require.True(t, payload.Success, payload.Message)
		assert.Equal(t, []model.IselfUsageRow{}, payload.Data)
	}
	unauthenticated := serveUsageAnalytics[apiStatusResponse](t, GetSelfIselfUsageAnalytics, 0, target)
	assert.False(t, unauthenticated.Success)

	admin := serveUsageAnalytics[iselfUsageResponse](t, GetAdminIselfUsageAnalytics, 1,
		"/api/data/iself?start_timestamp=1&end_timestamp=2000")
	require.True(t, admin.Success, admin.Message)
	require.Len(t, admin.Data, 2)
	assert.Equal(t, "bob@example.com", admin.Data[0].Email)
}

func TestIselfUsageEndpointsEnforceTimeRangeLimits(t *testing.T) {
	setupUsageAnalyticsControllerTestDB(t)
	for _, tt := range []struct {
		name    string
		handler gin.HandlerFunc
		query   string
		message string
	}{
		{"missing start", GetAdminIselfUsageAnalytics, "end_timestamp=2000", "invalid start_timestamp"},
		{"invalid end", GetSelfIselfUsageAnalytics, "start_timestamp=2000&end_timestamp=1", "invalid end_timestamp"},
		{"admin over limit", GetAdminIselfUsageAnalytics, fmt.Sprintf("start_timestamp=1&end_timestamp=%d", 2+92*86400), "time range cannot exceed 92 days"},
		{"self over limit", GetSelfIselfUsageAnalytics, fmt.Sprintf("start_timestamp=1&end_timestamp=%d", 2+30*86400), "time range cannot exceed 30 days"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			payload := serveUsageAnalytics[apiStatusResponse](t, tt.handler, 1, "/api/data/iself?"+tt.query)
			assert.False(t, payload.Success)
			assert.Equal(t, tt.message, payload.Message)
		})
	}
}
