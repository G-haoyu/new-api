package controller

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

const (
	schedulerEnabledKey         = "SchedulerEnabled"
	schedulerURLKey             = "SchedulerURL"
	schedulerTokenKey           = "SchedulerToken"
	schedulerModeKey            = "SchedulerMode"
	schedulerCanaryPercentKey   = "SchedulerCanaryPercent"
	schedulerCanarySaltKey      = "SchedulerCanarySalt"
	schedulerShadowTimeoutMSKey = "SchedulerShadowTimeoutMS"
	schedulerRuntimePrefixKey   = "SchedulerRuntimePrefix"
	schedulerSigningSecretKey   = "SchedulerSigningSecret"
)

type SchedulerConfigResponse struct {
	Enabled          bool   `json:"enabled"`
	URL              string `json:"url"`
	TokenSet         bool   `json:"token_set"`
	Mode             string `json:"mode"`
	CanaryPercent    int    `json:"canary_percent"`
	CanarySalt       string `json:"canary_salt"`
	ShadowTimeoutMS  int    `json:"shadow_timeout_ms"`
	RuntimePrefix    string `json:"runtime_prefix"`
	SigningSecretSet bool   `json:"signing_secret_set"`
	CatalogTokenSet  bool   `json:"catalog_token_set"`
}

type SchedulerConfigUpdateRequest struct {
	Enabled         *bool   `json:"enabled"`
	URL             *string `json:"url"`
	Token           *string `json:"token"`
	Mode            *string `json:"mode"`
	CanaryPercent   *int    `json:"canary_percent"`
	CanarySalt      *string `json:"canary_salt"`
	ShadowTimeoutMS *int    `json:"shadow_timeout_ms"`
	RuntimePrefix   *string `json:"runtime_prefix"`
	SigningSecret   *string `json:"signing_secret"`
}

func schedulerConfigValue(key, envKey, fallback string) string {
	common.OptionMapRWMutex.RLock()
	value, ok := common.OptionMap[key]
	common.OptionMapRWMutex.RUnlock()
	if ok && strings.TrimSpace(value) != "" {
		return value
	}
	if value = strings.TrimSpace(getenv(envKey)); value != "" {
		return value
	}
	return fallback
}

// getenv is isolated to keep configuration access easy to test.
var getenv = func(key string) string { return os.Getenv(key) }

func schedulerConfigResponse() SchedulerConfigResponse {
	percent, _ := strconv.Atoi(schedulerConfigValue(schedulerCanaryPercentKey, "SCHEDULER_CANARY_PERCENT", "0"))
	timeout, _ := strconv.Atoi(schedulerConfigValue(schedulerShadowTimeoutMSKey, "SCHEDULER_SHADOW_TIMEOUT_MS", "100"))
	return SchedulerConfigResponse{
		Enabled:          schedulerConfigValue(schedulerEnabledKey, "SCHEDULER_ENABLED", "false") == "true",
		URL:              strings.TrimRight(schedulerConfigValue(schedulerURLKey, "SCHEDULER_URL", ""), "/"),
		TokenSet:         schedulerConfigValue(schedulerTokenKey, "SCHEDULER_TOKEN", "") != "",
		Mode:             strings.ToLower(schedulerConfigValue(schedulerModeKey, "SCHEDULER_MODE", "shadow")),
		CanaryPercent:    percent,
		CanarySalt:       schedulerConfigValue(schedulerCanarySaltKey, "SCHEDULER_CANARY_SALT", "scheduler-v2"),
		ShadowTimeoutMS:  timeout,
		RuntimePrefix:    schedulerConfigValue(schedulerRuntimePrefixKey, "SCHEDULER_RUNTIME_PREFIX", service.SchedulerRuntimePrefix),
		SigningSecretSet: schedulerConfigValue(schedulerSigningSecretKey, "SCHEDULER_SIGNING_SECRET", "") != "",
		CatalogTokenSet:  strings.TrimSpace(getenv("SCHEDULER_CATALOG_TOKEN")) != "",
	}
}

func GetSchedulerConfig(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"success": true, "data": schedulerConfigResponse()})
}

func UpdateSchedulerConfig(c *gin.Context) {
	var req SchedulerConfigUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的调度配置"})
		return
	}
	values := make(map[string]string)
	if req.Enabled != nil {
		values[schedulerEnabledKey] = strconv.FormatBool(*req.Enabled)
	}
	if req.URL != nil {
		value := strings.TrimSpace(*req.URL)
		if value != "" {
			if _, err := url.ParseRequestURI(value); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Scheduler 地址无效"})
				return
			}
		}
		values[schedulerURLKey] = strings.TrimRight(value, "/")
	}
	if req.Token != nil && strings.TrimSpace(*req.Token) != "" {
		values[schedulerTokenKey] = strings.TrimSpace(*req.Token)
	}
	if req.SigningSecret != nil {
		values[schedulerSigningSecretKey] = strings.TrimSpace(*req.SigningSecret)
	}
	if req.Mode != nil {
		mode := strings.ToLower(strings.TrimSpace(*req.Mode))
		if mode != "shadow" && mode != "enforced" && mode != "canary" {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "调度模式无效"})
			return
		}
		values[schedulerModeKey] = mode
	}
	if req.CanaryPercent != nil && (*req.CanaryPercent < 0 || *req.CanaryPercent > 100) {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Canary 百分比必须在 0-100 之间"})
		return
	}
	if req.CanaryPercent != nil {
		values[schedulerCanaryPercentKey] = strconv.Itoa(*req.CanaryPercent)
	}
	if req.CanarySalt != nil {
		values[schedulerCanarySaltKey] = strings.TrimSpace(*req.CanarySalt)
	}
	if req.ShadowTimeoutMS != nil && *req.ShadowTimeoutMS < 1 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "超时时间必须大于 0"})
		return
	}
	if req.ShadowTimeoutMS != nil {
		values[schedulerShadowTimeoutMSKey] = strconv.Itoa(*req.ShadowTimeoutMS)
	}
	if req.RuntimePrefix != nil {
		value := strings.TrimSpace(*req.RuntimePrefix)
		if value == "" {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Runtime 前缀不能为空"})
			return
		}
		values[schedulerRuntimePrefixKey] = value
	}
	if err := model.UpdateOptionsBulk(values); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	service.ReloadSchedulerClient()
	recordManageAudit(c, "scheduler.config.update", map[string]interface{}{"keys": keys(values)})
	c.JSON(http.StatusOK, gin.H{"success": true, "data": schedulerConfigResponse()})
}

func keys(values map[string]string) []string {
	result := make([]string, 0, len(values))
	for key := range values {
		result = append(result, key)
	}
	return result
}

func GetSchedulerMonitor(c *gin.Context) {
	config := service.SchedulerClient()
	result := gin.H{"configured": config.Enabled && config.BaseURL != "", "url": config.BaseURL, "reachable": false, "checked_at": time.Now()}
	if config.BaseURL == "" {
		c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
		return
	}
	client := &http.Client{Timeout: config.Timeout}
	resp, err := client.Get(config.BaseURL + "/health/live")
	if err == nil {
		result["reachable"] = resp.StatusCode >= 200 && resp.StatusCode < 300
		_ = resp.Body.Close()
	}
	if config.Token != "" {
		req, _ := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, config.BaseURL+"/admin/observability", nil)
		req.Header.Set("Authorization", "Bearer "+config.Token)
		if response, requestErr := client.Do(req); requestErr == nil {
			defer response.Body.Close()
			var payload json.RawMessage
			if common.DecodeJson(response.Body, &payload) == nil {
				result["observability"] = payload
			}
		}
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

func TestSchedulerConnection(c *gin.Context) {
	config := service.SchedulerClient()
	if config.BaseURL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Scheduler 地址未配置"})
		return
	}
	client := &http.Client{Timeout: config.Timeout}
	resp, err := client.Get(config.BaseURL + "/health/live")
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"success": false, "message": fmt.Sprintf("连接失败: %v", err)})
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		c.JSON(http.StatusBadGateway, gin.H{"success": false, "message": fmt.Sprintf("Scheduler 返回 HTTP %d", resp.StatusCode)})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "Scheduler 连接正常"})
}
