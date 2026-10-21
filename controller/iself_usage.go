package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

func GetAdminIselfUsageAnalytics(c *gin.Context) {
	start, end, ok := parseUsageAnalyticsRange(c, adminUsageAnalyticsMaxDays)
	if !ok {
		return
	}
	rows, err := model.GetIselfUsageAnalytics(model.IselfUsageParams{
		Start: start, End: end, Username: c.Query("username"), ModelName: c.Query("model"),
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, rows)
}

func GetSelfIselfUsageAnalytics(c *gin.Context) {
	userID := c.GetInt("id")
	if userID <= 0 {
		common.ApiErrorMsg(c, "invalid user")
		return
	}
	start, end, ok := parseUsageAnalyticsRange(c, selfUsageAnalyticsMaxDays)
	if !ok {
		return
	}
	// Identity comes from authentication and the database, never client filters.
	rows, err := model.GetIselfUsageAnalytics(model.IselfUsageParams{
		Start: start, End: end, UserID: userID, ModelName: c.Query("model"),
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, rows)
}
