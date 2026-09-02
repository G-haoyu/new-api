package service

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
)

var ErrSchedulerPolicyUnavailable = errors.New("scheduler effective policy is unavailable")

// schedulerDefaultMode is the platform default routing mode applied when a user
// has no explicit routing preference for the requested model.
const schedulerDefaultMode = "balanced"

type SchedulerClientConfig struct {
	Enabled       bool
	BaseURL       string
	Token         string
	SigningSecret string
	Timeout       time.Duration
	Mode          string
	CanaryPercent int
	CanarySalt    string
}
type SchedulerCandidate struct {
	EndpointID    string   `json:"endpoint_id"`
	ChannelID     int      `json:"channel_id"`
	KeyIndex      int      `json:"key_index"`
	Model         string   `json:"model"`
	UpstreamModel string   `json:"upstream_model,omitempty"`
	Reason        []string `json:"reason"`
}
type schedulerRequest struct {
	RequestID            string         `json:"request_id"`
	UserID               int64          `json:"user_id,omitempty"`
	TokenID              int64          `json:"token_id,omitempty"`
	Group                string         `json:"group,omitempty"`
	Model                string         `json:"model"`
	Workload             string         `json:"workload,omitempty"`
	Capabilities         map[string]any `json:"capabilities"`
	Policy               map[string]any `json:"policy"`
	EstimatedInputTokens int            `json:"estimated_input_tokens"`
	MaxOutputTokens      int            `json:"max_output_tokens"`
}

type schedulerRequestShape struct {
	Stream              bool            `json:"stream"`
	Tools               json.RawMessage `json:"tools"`
	MaxTokens           int             `json:"max_tokens"`
	MaxCompletionTokens int             `json:"max_completion_tokens"`
	ResponseFormat      *struct {
		Type string `json:"type"`
	} `json:"response_format"`
}
type schedulerResponse struct {
	DecisionID        string `json:"decision_id"`
	ScoreVersion      string `json:"score_version,omitempty"`
	PreferenceVersion string `json:"preference_version,omitempty"`
	CatalogVersion    string `json:"catalog_version"`
	Reservation       *struct {
		ReservationID   string `json:"reservation_id"`
		EstimatedTokens int    `json:"estimated_tokens"`
	} `json:"reservation,omitempty"`
	Candidates []SchedulerCandidate `json:"candidates"`
}
type schedulerReserveRequest struct {
	RequestID       string `json:"request_id"`
	DecisionID      string `json:"decision_id"`
	AttemptNo       int    `json:"attempt_no"`
	EndpointID      string `json:"endpoint_id"`
	EstimatedTokens int    `json:"estimated_tokens"`
}
type schedulerResizeRequest struct {
	RequestID       string `json:"request_id"`
	DecisionID      string `json:"decision_id"`
	AttemptNo       int    `json:"attempt_no"`
	EndpointID      string `json:"endpoint_id"`
	ReservationID   string `json:"reservation_id"`
	EstimatedTokens int    `json:"estimated_tokens"`
}
type schedulerAttempt struct {
	RequestID       string `json:"request_id"`
	DecisionID      string `json:"decision_id"`
	AttemptNo       int    `json:"attempt_no"`
	EndpointID      string `json:"endpoint_id"`
	ReservationID   string `json:"reservation_id"`
	StatusCode      int    `json:"status_code"`
	ErrorType       string `json:"error_type"`
	Success         bool   `json:"success"`
	StreamStarted   bool   `json:"stream_started"`
	LatencyMS       int64  `json:"latency_ms"`
	TTFTMS          int64  `json:"ttft_ms,omitempty"`
	InputTokens     int    `json:"input_tokens"`
	OutputTokens    int    `json:"output_tokens"`
	EstimatedTokens int    `json:"estimated_tokens,omitempty"`
	UsageUnverified bool   `json:"usage_unverified,omitempty"`
}

var schedulerConfigState struct {
	sync.RWMutex
	loaded bool
	config SchedulerClientConfig
}

func SchedulerClient() SchedulerClientConfig {
	schedulerConfigState.RLock()
	if schedulerConfigState.loaded {
		c := schedulerConfigState.config
		schedulerConfigState.RUnlock()
		return c
	}
	schedulerConfigState.RUnlock()
	schedulerConfigState.Lock()
	defer schedulerConfigState.Unlock()
	if !schedulerConfigState.loaded {
		timeout := time.Duration(schedulerOptionInt("SchedulerShadowTimeoutMS", "SCHEDULER_SHADOW_TIMEOUT_MS", 100)) * time.Millisecond
		canaryPercent, _ := strconv.Atoi(strings.TrimSpace(schedulerOption("SchedulerCanaryPercent", "SCHEDULER_CANARY_PERCENT", "0")))
		canarySalt := strings.TrimSpace(schedulerOption("SchedulerCanarySalt", "SCHEDULER_CANARY_SALT", "scheduler-v2"))
		if canarySalt == "" {
			canarySalt = "scheduler-v2"
		}
		mode := strings.ToLower(strings.TrimSpace(schedulerOption("SchedulerMode", "SCHEDULER_MODE", "shadow")))
		if mode == "" {
			mode = "shadow"
		}
		schedulerConfigState.config = SchedulerClientConfig{Enabled: schedulerOptionBool("SchedulerEnabled", "SCHEDULER_ENABLED", false), BaseURL: strings.TrimRight(strings.TrimSpace(schedulerOption("SchedulerURL", "SCHEDULER_URL", "")), "/"), Token: schedulerOption("SchedulerToken", "SCHEDULER_TOKEN", ""), SigningSecret: schedulerOption("SchedulerSigningSecret", "SCHEDULER_SIGNING_SECRET", ""), Timeout: timeout, Mode: mode, CanaryPercent: canaryPercent, CanarySalt: canarySalt}
		schedulerConfigState.loaded = true
	}
	return schedulerConfigState.config
}

func schedulerOption(key, envKey, fallback string) string {
	common.OptionMapRWMutex.RLock()
	value, ok := common.OptionMap[key]
	common.OptionMapRWMutex.RUnlock()
	if ok {
		return value
	}
	if value = strings.TrimSpace(os.Getenv(envKey)); value != "" {
		return value
	}
	return fallback
}

func schedulerOptionInt(key, envKey string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(schedulerOption(key, envKey, "")))
	if err != nil || value < 1 {
		return fallback
	}
	return value
}

func schedulerOptionBool(key, envKey string, fallback bool) bool {
	value := strings.ToLower(strings.TrimSpace(schedulerOption(key, envKey, "")))
	if value == "true" || value == "1" {
		return true
	}
	if value == "false" || value == "0" {
		return false
	}
	return fallback
}

// ReloadSchedulerClient causes the next request to read the latest persisted
// configuration. It is used by the admin configuration endpoint after a
// successful atomic options update.
func ReloadSchedulerClient() {
	schedulerConfigState.Lock()
	schedulerConfigState.loaded = false
	schedulerConfigState.Unlock()
}

// ConfigureSchedulerClientForTest allows isolated middleware tests to opt in
// without mutating process environment. It is intentionally not used by boot.
func ConfigureSchedulerClientForTest(config SchedulerClientConfig) {
	schedulerConfigState.Lock()
	schedulerConfigState.config = config
	schedulerConfigState.loaded = true
	schedulerConfigState.Unlock()
}

func SchedulerEnforced() bool {
	c := SchedulerClient()
	return c.Enabled && c.Mode == "enforced"
}

// SchedulerEnforcedForRequest applies the stable-hash canary gate. The hash
// input is request-scoped and contains no prompt or credential material.
func SchedulerEnforcedForRequest(c *gin.Context) bool {
	config := SchedulerClient()
	if !config.Enabled {
		return false
	}
	if config.Mode == "enforced" {
		return true
	}
	if config.Mode != "canary" || config.CanaryPercent <= 0 {
		return false
	}
	percent := config.CanaryPercent
	if percent > 100 {
		percent = 100
	}
	key := ""
	if c != nil {
		if userID := c.GetInt("id"); userID > 0 {
			key = "user:" + strconv.Itoa(userID)
		} else if tokenID := c.GetInt("token_id"); tokenID > 0 {
			key = "token:" + strconv.Itoa(tokenID)
		} else {
			key = "request:" + c.GetString(common.RequestIdKey)
		}
	}
	if key == "" {
		return false
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(config.CanarySalt))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(key))
	return int(h.Sum32()%100) < percent
}

// RecordSchedulerUsage stores the usage reported by an upstream handler on
// the request context. Relay reports the attempt after the handler returns,
// so this keeps the scheduler payload aligned with the billing usage that
// new-api actually observed. Missing/negative values are normalized to zero.
func RecordSchedulerUsage(c *gin.Context, usage *dto.Usage, streamStarted bool) {
	if c == nil {
		return
	}
	if usage == nil {
		common.SetContextKey(c, constant.ContextKeySchedulerInputTokens, 0)
		common.SetContextKey(c, constant.ContextKeySchedulerOutputTokens, 0)
		common.SetContextKey(c, constant.ContextKeySchedulerUsageUnverified, true)
		if streamStarted {
			common.SetContextKey(c, constant.ContextKeySchedulerStreamStarted, true)
		}
		return
	}
	input := usage.InputTokens
	if input <= 0 {
		input = usage.PromptTokens
	}
	output := usage.OutputTokens
	if output <= 0 {
		output = usage.CompletionTokens
	}
	if input < 0 {
		input = 0
	}
	if output < 0 {
		output = 0
	}
	common.SetContextKey(c, constant.ContextKeySchedulerInputTokens, input)
	common.SetContextKey(c, constant.ContextKeySchedulerOutputTokens, output)
	usageUnverified := usage.BillingUsage != nil && usage.BillingUsage.Estimated
	common.SetContextKey(c, constant.ContextKeySchedulerUsageUnverified, usageUnverified)
	if streamStarted {
		common.SetContextKey(c, constant.ContextKeySchedulerStreamStarted, true)
	}
}

// ResetSchedulerAttemptMetrics clears usage and stream timing from the
// previous upstream attempt. Relay retries reuse one Gin context; retaining
// these fields would attribute a failed/silent retry the prior attempt's
// tokens or stream state.
func ResetSchedulerAttemptMetrics(c *gin.Context) {
	if c == nil {
		return
	}
	common.SetContextKey(c, constant.ContextKeySchedulerInputTokens, 0)
	common.SetContextKey(c, constant.ContextKeySchedulerOutputTokens, 0)
	common.SetContextKey(c, constant.ContextKeySchedulerUsageUnverified, false)
	common.SetContextKey(c, constant.ContextKeySchedulerStreamStarted, false)
	common.SetContextKey(c, constant.ContextKeySchedulerTTFTMS, 0)
}

// SchedulerCandidateForRetry is intentionally inert unless Enforced is
// explicitly selected. It only selects metadata already obtained by Shadow;
// Reservation re-acquisition per retry is a separate gate.
func SchedulerCandidateForRetry(c *gin.Context, retry int) (SchedulerCandidate, bool) {
	if !SchedulerEnforcedForRequest(c) || retry < 0 {
		return SchedulerCandidate{}, false
	}
	candidates, ok := common.GetContextKeyType[[]SchedulerCandidate](c, constant.ContextKeySchedulerCandidates)
	if !ok || retry >= len(candidates) {
		return SchedulerCandidate{}, false
	}
	candidate := candidates[retry]
	common.SetContextKey(c, constant.ContextKeySchedulerKeyIndex, candidate.KeyIndex)
	if candidate.UpstreamModel != "" {
		common.SetContextKey(c, constant.ContextKeySchedulerUpstreamModel, candidate.UpstreamModel)
	}
	return candidate, true
}

func SchedulerEndpointForChannel(c *gin.Context, channelID int) string {
	candidates, ok := common.GetContextKeyType[[]SchedulerCandidate](c, constant.ContextKeySchedulerCandidates)
	if !ok {
		return ""
	}
	for _, candidate := range candidates {
		if candidate.ChannelID == channelID {
			return candidate.EndpointID
		}
	}
	// In Shadow the native route may intentionally differ; release the
	// Scheduler reservation bound to the first candidate in that case.
	if len(candidates) > 0 {
		return candidates[0].EndpointID
	}
	return ""
}

func ReserveSchedulerCandidate(c *gin.Context, candidate SchedulerCandidate, attemptNo int) error {
	config := SchedulerClient()
	if !SchedulerEnforcedForRequest(c) || attemptNo < 2 {
		return nil
	}
	if candidate.EndpointID == "" {
		return fmt.Errorf("scheduler candidate endpoint is empty")
	}
	if config.Timeout <= 0 {
		config.Timeout = 100 * time.Millisecond
	}
	estimatedTokens := common.GetContextKeyInt(c, constant.ContextKeySchedulerEstimatedTokens)
	if estimatedTokens <= 0 {
		estimatedTokens = common.GetContextKeyInt(c, constant.ContextKeyEstimatedTokens)
	}
	body := schedulerReserveRequest{RequestID: c.GetString(common.RequestIdKey), DecisionID: common.GetContextKeyString(c, constant.ContextKeySchedulerDecisionID), AttemptNo: attemptNo, EndpointID: candidate.EndpointID, EstimatedTokens: estimatedTokens}
	payload, err := common.Marshal(body)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), config.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, config.BaseURL+"/v1/reserve", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+config.Token)
	req.Header.Set("Content-Type", "application/json")
	signSchedulerRequest(req, config.SigningSecret, payload)
	resp, err := (&http.Client{Timeout: config.Timeout}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("scheduler reserve status %d", resp.StatusCode)
	}
	var reservation struct {
		ReservationID string `json:"reservation_id"`
	}
	if err := common.DecodeJson(resp.Body, &reservation); err != nil {
		return err
	}
	if reservation.ReservationID == "" {
		return fmt.Errorf("scheduler reserve returned no reservation")
	}
	common.SetContextKey(c, constant.ContextKeySchedulerReservationID, reservation.ReservationID)
	return nil
}

// ResizeSchedulerReservation updates the first-attempt estimate after Relay
// has computed canonical prompt tokens. It is enforced-only: Shadow keeps the
// original best-effort reservation and records the canonical estimate solely
// for Attempt telemetry.
func ResizeSchedulerReservation(c *gin.Context, estimatedTokens int) error {
	config := SchedulerClient()
	if !SchedulerEnforcedForRequest(c) || config.BaseURL == "" || config.Token == "" {
		return nil
	}
	if estimatedTokens < 0 {
		estimatedTokens = 0
	}
	decisionID := common.GetContextKeyString(c, constant.ContextKeySchedulerDecisionID)
	reservationID := common.GetContextKeyString(c, constant.ContextKeySchedulerReservationID)
	if decisionID == "" || reservationID == "" {
		return nil
	}
	candidates, ok := common.GetContextKeyType[[]SchedulerCandidate](c, constant.ContextKeySchedulerCandidates)
	if !ok || len(candidates) == 0 {
		return fmt.Errorf("scheduler resize candidates are empty")
	}
	// Schedule reserves the first candidate. Distributor may have selected a
	// different native channel before Relay consumes the enforced chain, so do
	// not derive this identity from the current ChannelID.
	endpointID := candidates[0].EndpointID
	if endpointID == "" {
		return fmt.Errorf("scheduler resize endpoint is empty")
	}
	if config.Timeout <= 0 {
		config.Timeout = 100 * time.Millisecond
	}
	body := schedulerResizeRequest{RequestID: c.GetString(common.RequestIdKey), DecisionID: decisionID, AttemptNo: 1, EndpointID: endpointID, ReservationID: reservationID, EstimatedTokens: estimatedTokens}
	payload, err := common.Marshal(body)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), config.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, config.BaseURL+"/v1/resize", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+config.Token)
	req.Header.Set("Content-Type", "application/json")
	signSchedulerRequest(req, config.SigningSecret, payload)
	resp, err := (&http.Client{Timeout: config.Timeout}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("scheduler resize status %d", resp.StatusCode)
	}
	common.SetContextKey(c, constant.ContextKeySchedulerEstimatedTokens, estimatedTokens)
	return nil
}

// RunSchedulerShadow is best-effort by design: errors never alter native
// channel selection. A successful response is stored only as request metadata;
// Enforced candidate consumption is a separate, explicitly gated phase.
func schedulerEffectivePolicy(c *gin.Context, modelName string) (map[string]any, error) {
	// Default to the balanced routing policy so a request always carries a valid
	// mode even when the user has no routing preference configured. User-level
	// preferences, when present, override these platform defaults field by field.
	policy := map[string]any{
		"mode":            schedulerDefaultMode,
		"allow_fallbacks": true,
		"max_attempts":    3,
	}
	setting, ok := common.GetContextKeyType[dto.UserSetting](c, constant.ContextKeyUserSetting)
	if !ok || len(setting.RoutingPreferences) == 0 {
		return policy, nil
	}
	pref, ok := setting.RoutingPreferences[modelName]
	if !ok {
		pref, ok = setting.RoutingPreferences["*"]
	}
	if !ok {
		return policy, nil
	}
	if mode := strings.ToLower(strings.TrimSpace(pref.Mode)); mode != "" {
		policy["mode"] = mode
	}
	if pref.AllowFallbacks != nil {
		policy["allow_fallbacks"] = *pref.AllowFallbacks
	}
	if pref.MaxAttempts > 0 {
		policy["max_attempts"] = pref.MaxAttempts
	}
	if pref.MaxPrice > 0 {
		policy["max_price"] = pref.MaxPrice
	}
	if pref.MinQualityScore > 0 {
		policy["min_quality_score"] = pref.MinQualityScore
	}
	if len(pref.ProviderOrder) > 0 {
		policy["provider_order"] = pref.ProviderOrder
	}
	if len(pref.ProviderOnly) > 0 {
		policy["provider_only"] = pref.ProviderOnly
	}
	if len(pref.ProviderIgnore) > 0 {
		policy["provider_ignore"] = pref.ProviderIgnore
	}
	if len(pref.PreferredRegions) > 0 {
		policy["preferred_regions"] = pref.PreferredRegions
	}
	if pref.DataPolicy != "" {
		policy["data_policy"] = pref.DataPolicy
	}
	if pref.PreferenceVersion != "" {
		policy["preference_version"] = pref.PreferenceVersion
	}
	return policy, nil
}

func RunSchedulerShadow(c *gin.Context, modelName, group string) error {
	config := SchedulerClient()
	if !config.Enabled || config.BaseURL == "" || config.Token == "" {
		return nil
	}
	if config.Timeout <= 0 {
		config.Timeout = 100 * time.Millisecond
	}
	policy, err := schedulerEffectivePolicy(c, modelName)
	if err != nil {
		return err
	}
	requestID := c.GetString(common.RequestIdKey)
	if requestID == "" {
		requestID = common.NewRequestId()
		c.Set(common.RequestIdKey, requestID)
	}
	estimatedInput := common.GetContextKeyInt(c, constant.ContextKeyEstimatedTokens)
	maxOutput := schedulerMaxOutputTokens(c)
	body := schedulerRequest{RequestID: requestID, UserID: int64(c.GetInt("id")), TokenID: int64(c.GetInt("token_id")), Group: group, Model: modelName, Workload: common.GetContextKeyString(c, constant.ContextKeySchedulerWorkload), Capabilities: schedulerCapabilities(c), Policy: policy, EstimatedInputTokens: estimatedInput, MaxOutputTokens: maxOutput}
	payload, err := common.Marshal(body)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), config.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, config.BaseURL+"/v1/schedule", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+config.Token)
	req.Header.Set("Content-Type", "application/json")
	signSchedulerRequest(req, config.SigningSecret, payload)
	resp, err := (&http.Client{Timeout: config.Timeout}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("scheduler status %d", resp.StatusCode)
	}
	var scheduled schedulerResponse
	if err := common.DecodeJson(resp.Body, &scheduled); err != nil {
		return err
	}
	if scheduled.DecisionID == "" || scheduled.CatalogVersion == "" || len(scheduled.Candidates) == 0 {
		return fmt.Errorf("scheduler returned incomplete response")
	}
	seen := make(map[string]struct{}, len(scheduled.Candidates))
	for _, candidate := range scheduled.Candidates {
		if candidate.EndpointID == "" || candidate.ChannelID < 0 || candidate.KeyIndex < 0 || candidate.Model == "" || len(candidate.Reason) == 0 {
			return fmt.Errorf("scheduler returned invalid candidate")
		}
		if _, exists := seen[candidate.EndpointID]; exists {
			return fmt.Errorf("scheduler returned duplicate candidate")
		}
		seen[candidate.EndpointID] = struct{}{}
	}
	common.SetContextKey(c, constant.ContextKeySchedulerCandidates, scheduled.Candidates)
	common.SetContextKey(c, constant.ContextKeySchedulerDecisionID, scheduled.DecisionID)
	common.SetContextKey(c, constant.ContextKeySchedulerScoreVersion, scheduled.ScoreVersion)
	preferenceVersion := scheduled.PreferenceVersion
	if preferenceVersion == "" {
		preferenceVersion, _ = policy["preference_version"].(string)
	}
	common.SetContextKey(c, constant.ContextKeySchedulerPreferenceVersion, preferenceVersion)
	if preferenceVersion, ok := policy["preference_version"].(string); ok {
		common.SetContextKey(c, constant.ContextKeySchedulerPreferenceVersion, preferenceVersion)
	}
	if scheduled.Reservation != nil {
		common.SetContextKey(c, constant.ContextKeySchedulerReservationID, scheduled.Reservation.ReservationID)
		if scheduled.Reservation.EstimatedTokens > 0 {
			common.SetContextKey(c, constant.ContextKeySchedulerEstimatedTokens, scheduled.Reservation.EstimatedTokens)
		}
	}
	match := false
	for _, candidate := range scheduled.Candidates {
		if candidate.ChannelID == common.GetContextKeyInt(c, constant.ContextKeyChannelId) {
			match = true
			break
		}
	}
	common.SetContextKey(c, constant.ContextKeySchedulerShadowMatch, match)
	return nil
}

// schedulerCapabilities reads only the small capability envelope from the
// already cached request body. The body is rewound on every path so Relay
// handlers see the exact same payload. Non-JSON or unavailable bodies simply
// retain conservative defaults.
func schedulerCapabilities(c *gin.Context) map[string]any {
	capabilities := map[string]any{"stream": false, "tools": false, "json_mode": false, "vision": false}
	if c == nil || c.Request == nil {
		return capabilities
	}
	storage, err := common.GetBodyStorage(c)
	if err != nil || storage == nil {
		return capabilities
	}
	if _, err := storage.Seek(0, io.SeekStart); err != nil {
		return capabilities
	}
	var shape schedulerRequestShape
	err = common.DecodeJson(storage, &shape)
	_, _ = storage.Seek(0, io.SeekStart)
	if err != nil {
		return capabilities
	}
	capabilities["stream"] = shape.Stream
	capabilities["tools"] = len(shape.Tools) > 0 && string(shape.Tools) != "null" && string(shape.Tools) != "[]"
	if shape.ResponseFormat != nil {
		capabilities["json_mode"] = shape.ResponseFormat.Type == "json_object" || shape.ResponseFormat.Type == "json_schema"
	}
	return capabilities
}

func schedulerMaxOutputTokens(c *gin.Context) int {
	if c == nil || c.Request == nil {
		return 0
	}
	storage, err := common.GetBodyStorage(c)
	if err != nil || storage == nil {
		return 0
	}
	if _, err := storage.Seek(0, io.SeekStart); err != nil {
		return 0
	}
	var shape schedulerRequestShape
	err = common.DecodeJson(storage, &shape)
	_, _ = storage.Seek(0, io.SeekStart)
	if err != nil {
		return 0
	}
	if shape.MaxCompletionTokens > 0 {
		return shape.MaxCompletionTokens
	}
	return shape.MaxTokens
}

// SchedulerMaxOutputTokens exposes the request's output cap to the Relay
// controller when it corrects the first Reservation estimate.
func SchedulerMaxOutputTokens(c *gin.Context) int {
	return schedulerMaxOutputTokens(c)
}

// ReportSchedulerShadowAttempt releases the Scheduler reservation without
// affecting the response path. It is best-effort and should be called after
// c.Next() so the final HTTP status is available.
func ReportSchedulerShadowAttempt(c *gin.Context) error {
	config := SchedulerClient()
	if !config.Enabled || config.BaseURL == "" || config.Token == "" {
		return nil
	}
	decisionID := common.GetContextKeyString(c, constant.ContextKeySchedulerDecisionID)
	if decisionID == "" {
		return nil
	}
	candidates, ok := common.GetContextKeyType[[]SchedulerCandidate](c, constant.ContextKeySchedulerCandidates)
	if !ok || len(candidates) == 0 {
		return nil
	}
	// Shadow reserves only the first Scheduler candidate. The native channel
	// may intentionally differ, but the Attempt identity must still match the
	// Reservation endpoint so capacity settlement and audit reconciliation do
	// not commit one Endpoint while reporting another.
	endpoint := candidates[0]
	requestID := c.GetString(common.RequestIdKey)
	reservationID := common.GetContextKeyString(c, constant.ContextKeySchedulerReservationID)
	status := c.Writer.Status()
	if status == 0 {
		status = http.StatusOK
	}
	start := common.GetContextKeyTime(c, constant.ContextKeyRequestStartTime)
	latency := int64(0)
	if !start.IsZero() {
		latency = time.Since(start).Milliseconds()
	}
	inputTokens := common.GetContextKeyInt(c, constant.ContextKeySchedulerInputTokens)
	outputTokens := common.GetContextKeyInt(c, constant.ContextKeySchedulerOutputTokens)
	usageUnverified := common.GetContextKeyBool(c, constant.ContextKeySchedulerUsageUnverified)
	if !usageUnverified && inputTokens == 0 && outputTokens == 0 && status >= 200 && status < 400 {
		usageUnverified = true
	}
	attempt := schedulerAttempt{
		RequestID: requestID, DecisionID: decisionID, AttemptNo: 1,
		EndpointID: endpoint.EndpointID, ReservationID: reservationID,
		StatusCode: status, Success: status >= 200 && status < 400,
		StreamStarted:   common.GetContextKeyBool(c, constant.ContextKeySchedulerStreamStarted),
		LatencyMS:       latency,
		TTFTMS:          int64(common.GetContextKeyInt(c, constant.ContextKeySchedulerTTFTMS)),
		InputTokens:     inputTokens,
		OutputTokens:    outputTokens,
		EstimatedTokens: schedulerEstimatedTokens(c),
		UsageUnverified: usageUnverified,
	}
	return postSchedulerAttempt(c, config, attempt)
}

func ReportSchedulerAttempt(c *gin.Context, endpointID string, attemptNo, statusCode int, success, streamStarted bool, inputTokens, outputTokens int) error {
	config := SchedulerClient()
	if !config.Enabled || config.BaseURL == "" || config.Token == "" {
		return nil
	}
	decisionID := common.GetContextKeyString(c, constant.ContextKeySchedulerDecisionID)
	if decisionID == "" || endpointID == "" {
		return nil
	}
	if config.Timeout <= 0 {
		config.Timeout = 100 * time.Millisecond
	}
	if statusCode == 0 {
		statusCode = http.StatusOK
	}
	start := common.GetContextKeyTime(c, constant.ContextKeyRequestStartTime)
	latency := int64(0)
	if !start.IsZero() {
		latency = time.Since(start).Milliseconds()
	}
	usageUnverified := common.GetContextKeyBool(c, constant.ContextKeySchedulerUsageUnverified)
	if !usageUnverified && inputTokens == 0 && outputTokens == 0 && (success || streamStarted) {
		usageUnverified = true
	}
	attempt := schedulerAttempt{RequestID: c.GetString(common.RequestIdKey), DecisionID: decisionID, AttemptNo: attemptNo, EndpointID: endpointID, ReservationID: common.GetContextKeyString(c, constant.ContextKeySchedulerReservationID), StatusCode: statusCode, Success: success, StreamStarted: streamStarted, LatencyMS: latency, TTFTMS: int64(common.GetContextKeyInt(c, constant.ContextKeySchedulerTTFTMS)), InputTokens: inputTokens, OutputTokens: outputTokens, EstimatedTokens: schedulerEstimatedTokens(c), UsageUnverified: usageUnverified}
	err := postSchedulerAttempt(c, config, attempt)
	if err == nil {
		common.SetContextKey(c, constant.ContextKeySchedulerAttemptReported, true)
	}
	return err
}

func schedulerEstimatedTokens(c *gin.Context) int {
	if c == nil {
		return 0
	}
	if estimate := common.GetContextKeyInt(c, constant.ContextKeySchedulerEstimatedTokens); estimate > 0 {
		return estimate
	}
	return common.GetContextKeyInt(c, constant.ContextKeyEstimatedTokens)
}

func postSchedulerAttempt(c *gin.Context, config SchedulerClientConfig, attempt schedulerAttempt) error {
	payload, err := common.Marshal(attempt)
	if err != nil {
		return err
	}
	// Attempt reporting happens after Relay may have completed or after the
	// client has disconnected. Do not inherit the request context here: its
	// cancellation must not strand the Scheduler Reservation. The bounded
	// background context keeps this best-effort side effect independent while
	// still enforcing the client timeout.
	ctx, cancel := context.WithTimeout(context.Background(), config.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, config.BaseURL+"/v1/attempt", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+config.Token)
	req.Header.Set("Content-Type", "application/json")
	signSchedulerRequest(req, config.SigningSecret, payload)
	resp, err := (&http.Client{Timeout: config.Timeout}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("scheduler attempt status %d", resp.StatusCode)
	}
	return nil
}

func signSchedulerRequest(req *http.Request, secret string, payload []byte) {
	if req == nil || strings.TrimSpace(secret) == "" {
		return
	}
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(req.Method))
	_, _ = mac.Write([]byte("\n"))
	_, _ = mac.Write([]byte(req.URL.Path))
	_, _ = mac.Write([]byte("\n"))
	_, _ = mac.Write([]byte(timestamp))
	_, _ = mac.Write([]byte("\n"))
	_, _ = mac.Write(payload)
	req.Header.Set("X-Scheduler-Timestamp", timestamp)
	req.Header.Set("X-Scheduler-Signature", hex.EncodeToString(mac.Sum(nil)))
}
