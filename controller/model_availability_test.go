package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type modelAvailabilityResponse struct {
	Success bool                          `json:"success"`
	Message string                        `json:"message"`
	Data    model.ModelAvailabilityWindow `json:"data"`
}

func serveModelAvailability(t *testing.T, role int, userID int, target string) modelAvailabilityResponse {
	t.Helper()
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("id", userID)
	ctx.Set("role", role)
	ctx.Request = httptest.NewRequest(http.MethodGet, target, nil)
	GetModelAvailability(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)
	var payload modelAvailabilityResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	return payload
}

func availabilityModelNames(window model.ModelAvailabilityWindow) []string {
	names := make([]string, 0, len(window.Models))
	for _, item := range window.Models {
		names = append(names, item.ModelName)
	}
	return names
}

func setupModelAvailabilityControllerTestDB(t *testing.T) {
	t.Helper()
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Log{}))
	// Keep fixtures inside the window and stable across cached queries.
	createdAt := time.Now().Unix()/60*60 - 120
	require.NoError(t, db.Create(&[]model.Log{
		{RequestId: "a", Type: model.LogTypeConsume, ModelName: "gpt-a", CreatedAt: createdAt},
		{RequestId: "b", Type: model.LogTypeConsume, ModelName: "gpt-b", CreatedAt: createdAt},
	}).Error)
	require.NoError(t, db.Create(&model.User{
		Id: 3001, Username: "availability-user", Password: "password",
		Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default",
	}).Error)
}

func TestGetModelAvailabilityValidatesMinutes(t *testing.T) {
	setupModelAvailabilityControllerTestDB(t)

	tests := []struct {
		query   string
		minutes int
		success bool
	}{
		{query: "", minutes: 10, success: true},
		{query: "?minutes=30", minutes: 30, success: true},
		{query: "?minutes=60", minutes: 60, success: true},
		{query: "?minutes=5"},
		{query: "?minutes=15"},
		{query: "?minutes=abc"},
	}
	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			payload := serveModelAvailability(t, common.RoleAdminUser, 1, "/api/perf-metrics/availability"+tt.query)
			require.Equal(t, tt.success, payload.Success, payload.Message)
			if !tt.success {
				assert.Equal(t, "minutes must be 10, 30, or 60", payload.Message)
				return
			}
			assert.Equal(t, tt.minutes, payload.Data.Minutes)
			assert.Equal(t, int64(tt.minutes*60), payload.Data.EndTimestamp-payload.Data.StartTimestamp)
		})
	}
}

func TestGetModelAvailabilityFiltersModelsForNonAdmins(t *testing.T) {
	setupModelAvailabilityControllerTestDB(t)

	admin := serveModelAvailability(t, common.RoleAdminUser, 1, "/api/perf-metrics/availability")
	require.True(t, admin.Success, admin.Message)
	assert.Equal(t, []string{"gpt-a", "gpt-b"}, availabilityModelNames(admin.Data))

	// An empty allowed set must hide all models.
	require.NoError(t, model.DB.Create(&model.Ability{Group: "premium", Model: "gpt-b", ChannelId: 1, Enabled: true}).Error)
	none := serveModelAvailability(t, common.RoleCommonUser, 3001, "/api/perf-metrics/availability")
	require.True(t, none.Success, none.Message)
	assert.Empty(t, none.Data.Models)

	require.NoError(t, model.DB.Create(&model.Ability{Group: "default", Model: "gpt-a", ChannelId: 1, Enabled: true}).Error)
	user := serveModelAvailability(t, common.RoleCommonUser, 3001, "/api/perf-metrics/availability")
	require.True(t, user.Success, user.Message)
	assert.Equal(t, []string{"gpt-a"}, availabilityModelNames(user.Data))
}
