package controller

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func GetChannelAffinityCacheStats(c *gin.Context) {
	stats := service.GetChannelAffinityCacheStats()
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    stats,
	})
}

// ClearChannelAffinityCache 清亲和缓存，三种范围：all=true 全部；rule_name 单条规则；
// model / user_id 某个模型或某个用户的绑定（可再用 rule_name 限定到一条规则）。
func ClearChannelAffinityCache(c *gin.Context) {
	all := strings.TrimSpace(c.Query("all"))
	ruleName := strings.TrimSpace(c.Query("rule_name"))
	modelName := strings.TrimSpace(c.Query("model"))
	userIDText := strings.TrimSpace(c.Query("user_id"))

	if all == "true" {
		deleted := service.ClearChannelAffinityCacheAll()
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "",
			"data": gin.H{
				"deleted": deleted,
			},
		})
		return
	}

	var deleted int
	var err error
	switch {
	case modelName != "" || userIDText != "":
		userID := 0
		if userIDText != "" {
			userID, err = strconv.Atoi(userIDText)
			if err != nil || userID <= 0 {
				common.ApiErrorStatus(c, http.StatusBadRequest, common.NewMessage("user_id must be a positive integer"))
				return
			}
		}
		deleted, err = service.ClearChannelAffinityCacheByScope(ruleName, modelName, userID)
	case ruleName != "":
		deleted, err = service.ClearChannelAffinityCacheByRuleName(ruleName)
	default:
		common.ApiErrorStatus(c, http.StatusBadRequest, common.NewMessage("Missing parameter rule_name, or use all=true to clear everything"))
		return
	}
	if err != nil {
		common.ApiErrorStatus(c, http.StatusBadRequest, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"deleted": deleted,
		},
	})
}

func GetChannelAffinityUsageCacheStats(c *gin.Context) {
	ruleName := strings.TrimSpace(c.Query("rule_name"))
	usingGroup := strings.TrimSpace(c.Query("using_group"))
	keyFp := strings.TrimSpace(c.Query("key_fp"))

	if ruleName == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "missing param: rule_name",
		})
		return
	}
	if keyFp == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "missing param: key_fp",
		})
		return
	}

	stats := service.GetChannelAffinityUsageCacheStats(ruleName, usingGroup, keyFp)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    stats,
	})
}
