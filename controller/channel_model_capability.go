package controller

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/capability"

	"github.com/gin-gonic/gin"
)

// GetModelCapabilityTargets 返回"模型 → 服务渠道"清单，供渠道能力设置页使用。
func GetModelCapabilityTargets(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	channelID, err := strconv.Atoi(c.Query("channel_id"))
	if err != nil {
		channelID = 0
	}
	targets, total, err := model.GetCapabilityTargets(model.CapabilityTargetQuery{
		Keyword:   c.Query("keyword"),
		ChannelID: channelID,
		Offset:    pageInfo.GetStartIdx(),
		Limit:     pageInfo.GetPageSize(),
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{
		"items":     targets,
		"total":     total,
		"page":      pageInfo.GetPage(),
		"page_size": pageInfo.GetPageSize(),
	})
}

type channelModelCapabilityRequest struct {
	ChannelId    int    `json:"channel_id"`
	Model        string `json:"model"`
	Capabilities string `json:"capabilities"`
}

// GetChannelModelCapability 返回渠道收窄、模型级基线与合并后的生效能力。
func GetChannelModelCapability(c *gin.Context) {
	channelID, err := strconv.Atoi(c.Query("channel_id"))
	if err != nil || channelID <= 0 {
		common.ApiErrorMsg(c, "channel_id is required")
		return
	}
	modelName := strings.TrimSpace(c.Query("model"))
	if modelName == "" {
		common.ApiErrorMsg(c, "model is required")
		return
	}
	base, declared := model.ResolveModelCapability(modelName)
	if !declared {
		common.ApiErrorMsg(c, "model has no model-level capability declaration")
		return
	}
	over, configured := model.GetChannelModelCapability(channelID, modelName)
	effective := capability.Merge(base, over)
	baseRaw, _ := capability.Encode(base)
	effectiveRaw, _ := capability.Encode(effective)
	overRaw := ""
	if configured {
		overRaw, _ = capability.Encode(over)
	}
	common.ApiSuccess(c, gin.H{
		"channel_id":         channelID,
		"model":              modelName,
		"configured":         configured,
		"capabilities":       overRaw,
		"model_capabilities": baseRaw,
		"effective":          effectiveRaw,
	})
}

// SaveChannelModelCapability 保存一条渠道能力收窄。
func SaveChannelModelCapability(c *gin.Context) {
	var request channelModelCapabilityRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.SetChannelModelCapability(request.ChannelId, request.Model, request.Capabilities); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

// DeleteChannelModelCapability 删除一条渠道能力收窄，回到继承模型级声明。
func DeleteChannelModelCapability(c *gin.Context) {
	channelID, err := strconv.Atoi(c.Query("channel_id"))
	if err != nil || channelID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "channel_id is required"})
		return
	}
	modelName := strings.TrimSpace(c.Query("model"))
	if modelName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "model is required"})
		return
	}
	if err := model.DeleteChannelModelCapability(channelID, modelName); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}
