package controller

import (
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"

	"github.com/gin-gonic/gin"
)

// updateTokenRoutingPreferenceRequest 中 routing_preference 为 null 表示清除，
// 令牌回退到平台默认调度策略。
type updateTokenRoutingPreferenceRequest struct {
	RoutingPreference *dto.RoutingPreference `json:"routing_preference"`
}

// GetTokenRoutingPreference 返回当前用户某个令牌的 Scheduler 路由倾向。
// 未配置时 routing_preference 为 null。
func GetTokenRoutingPreference(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	token, err := model.GetTokenByIds(id, c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	preference, err := token.GetRoutingPreference()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"routing_preference": preference})
}

// UpdateTokenRoutingPreference 覆盖写入令牌级路由倾向（每个令牌一条，与模型无关）。
// 令牌归属由 GetTokenByIds 按当前用户校验。
func UpdateTokenRoutingPreference(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	var req updateTokenRoutingPreferenceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	if req.RoutingPreference != nil {
		req.RoutingPreference.Normalize()
		if err := req.RoutingPreference.Validate(); err != nil {
			common.ApiError(c, err)
			return
		}
	}
	token, err := model.GetTokenByIds(id, c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if err := token.SetRoutingPreference(req.RoutingPreference); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := token.UpdateRoutingPreference(); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"routing_preference": req.RoutingPreference})
}
