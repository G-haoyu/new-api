package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
)

func TestSignSchedulerRequestUsesCanonicalPayload(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "http://scheduler.test/v1/schedule", nil)
	body := []byte(`{"request_id":"r"}`)
	signSchedulerRequest(req, "secret", body)
	timestamp := req.Header.Get("X-Scheduler-Timestamp")
	if _, err := strconv.ParseInt(timestamp, 10, 64); err != nil || timestamp == "" {
		t.Fatalf("timestamp=%q err=%v", timestamp, err)
	}
	// Recompute the protocol using the emitted timestamp; the helper uses the
	// same method/path/body canonicalization as Scheduler's verifier.
	mac := hmac.New(sha256.New, []byte("secret"))
	_, _ = mac.Write([]byte("POST\n/v1/schedule\n" + timestamp + "\n"))
	_, _ = mac.Write(body)
	if got, want := req.Header.Get("X-Scheduler-Signature"), hex.EncodeToString(mac.Sum(nil)); got != want {
		t.Fatalf("signature=%s want=%s", got, want)
	}
}

func TestRunSchedulerShadowStoresOnlyMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer scheduler-test" {
			t.Fatalf("missing scheduler auth")
		}
		if r.URL.Path != "/v1/schedule" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		var request schedulerRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode schedule request: %v", err)
		}
		if request.Policy["mode"] != "price" || request.Policy["max_price"] != float64(1.5) || request.Policy["preference_version"] != "pref-test" {
			t.Fatalf("effective policy=%v", request.Policy)
		}
		if request.MaxOutputTokens != 123 {
			t.Fatalf("max output estimate=%d", request.MaxOutputTokens)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"decision_id":"d1","catalog_version":"catalog-v1","reservation":{"reservation_id":"res1","estimated_tokens":123},"candidates":[{"endpoint_id":"ep1","channel_id":7,"key_index":2,"model":"m","reason":["healthy"]}]}`))
	}))
	defer server.Close()
	ConfigureSchedulerClientForTest(SchedulerClientConfig{Enabled: true, BaseURL: server.URL, Token: "scheduler-test", Timeout: time.Second})
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"m","max_tokens":123}`))
	c.Set(common.RequestIdKey, "req1")
	common.SetContextKey(c, constant.ContextKeyChannelId, 7)
	common.SetContextKey(c, constant.ContextKeyUserSetting, dto.UserSetting{RoutingPreferences: map[string]dto.RoutingPreference{
		"m": {Mode: "price", MaxPrice: 1.5, PreferenceVersion: "pref-test"},
	}})
	if err := RunSchedulerShadow(c, "m", "default"); err != nil {
		t.Fatal(err)
	}
	if got := common.GetContextKeyString(c, constant.ContextKeySchedulerDecisionID); got != "d1" {
		t.Fatalf("decision=%s", got)
	}
	if got := common.GetContextKeyString(c, constant.ContextKeySchedulerReservationID); got != "res1" {
		t.Fatalf("reservation=%s", got)
	}
	if got := common.GetContextKeyInt(c, constant.ContextKeySchedulerEstimatedTokens); got != 123 {
		t.Fatalf("scheduler estimate=%d", got)
	}
	if !common.GetContextKeyBool(c, constant.ContextKeySchedulerShadowMatch) {
		t.Fatal("shadow match not recorded")
	}
	if candidates, ok := common.GetContextKeyType[[]SchedulerCandidate](c, constant.ContextKeySchedulerCandidates); !ok || len(candidates) != 1 || candidates[0].KeyIndex != 2 {
		t.Fatalf("candidates=%+v ok=%v", candidates, ok)
	}
	if got := SchedulerEndpointForChannel(c, 99); got != "ep1" {
		t.Fatalf("shadow fallback endpoint=%s", got)
	}
}

func TestRunSchedulerShadowDefaultsToBalancedPolicy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request schedulerRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode schedule request: %v", err)
		}
		if request.Policy["mode"] != "balanced" || request.Policy["allow_fallbacks"] != true || request.Policy["max_attempts"] != float64(3) {
			t.Fatalf("default policy=%v", request.Policy)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"decision_id":"d1","catalog_version":"catalog-v1","candidates":[{"endpoint_id":"ep1","channel_id":7,"key_index":2,"model":"m","reason":["healthy"]}]}`))
	}))
	defer server.Close()
	ConfigureSchedulerClientForTest(SchedulerClientConfig{Enabled: true, BaseURL: server.URL, Token: "scheduler-test", Timeout: time.Second})
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Set(common.RequestIdKey, "req1")
	if err := RunSchedulerShadow(c, "m", "default"); err != nil {
		t.Fatalf("shadow with default policy failed: %v", err)
	}
	if got := common.GetContextKeyString(c, constant.ContextKeySchedulerDecisionID); got != "d1" {
		t.Fatalf("decision=%s", got)
	}
}

func TestResizeSchedulerReservationUsesReservedCandidate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/resize" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer scheduler-test" {
			t.Fatalf("missing scheduler auth")
		}
		var got schedulerResizeRequest
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode resize: %v", err)
		}
		if got.RequestID != "resize-request" || got.DecisionID != "resize-decision" || got.EndpointID != "reserved-first" || got.ReservationID != "resize-reservation" || got.EstimatedTokens != 77 {
			t.Fatalf("resize request=%+v", got)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	ConfigureSchedulerClientForTest(SchedulerClientConfig{Enabled: true, Mode: "enforced", BaseURL: server.URL, Token: "scheduler-test", Timeout: time.Second})
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Set(common.RequestIdKey, "resize-request")
	common.SetContextKey(c, constant.ContextKeySchedulerDecisionID, "resize-decision")
	common.SetContextKey(c, constant.ContextKeySchedulerReservationID, "resize-reservation")
	common.SetContextKey(c, constant.ContextKeySchedulerCandidates, []SchedulerCandidate{{EndpointID: "reserved-first", ChannelID: 1, KeyIndex: 0, Model: "m", Reason: []string{"healthy"}}, {EndpointID: "native-second", ChannelID: 2, KeyIndex: 0, Model: "m", Reason: []string{"fallback"}}})
	common.SetContextKey(c, constant.ContextKeyChannelId, 2)
	if err := ResizeSchedulerReservation(c, 77); err != nil {
		t.Fatal(err)
	}
	if got := common.GetContextKeyInt(c, constant.ContextKeySchedulerEstimatedTokens); got != 77 {
		t.Fatalf("estimate=%d", got)
	}
}

func TestReportSchedulerShadowAttemptReleasesReservation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/attempt" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer scheduler-test" {
			t.Fatalf("missing scheduler auth")
		}
		var got schedulerAttempt
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode attempt: %v", err)
		}
		if got.DecisionID != "d1" || got.EndpointID != "ep1" || got.AttemptNo != 1 {
			t.Fatalf("attempt identity=%+v", got)
		}
		if got.InputTokens != 13 || got.OutputTokens != 5 || !got.StreamStarted || got.TTFTMS != 42 {
			t.Fatalf("attempt metrics=%+v", got)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	ConfigureSchedulerClientForTest(SchedulerClientConfig{Enabled: true, BaseURL: server.URL, Token: "scheduler-test", Timeout: time.Second})
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	requestCtx, cancel := context.WithCancel(context.Background())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil).WithContext(requestCtx)
	cancel()
	c.Set(common.RequestIdKey, "req1")
	// Native Shadow routing intentionally selects a different Channel than the
	// Scheduler's reserved first candidate; the reported endpoint must remain
	// the reservation owner (ep1), not the native second candidate.
	common.SetContextKey(c, constant.ContextKeyChannelId, 8)
	common.SetContextKey(c, constant.ContextKeySchedulerDecisionID, "d1")
	common.SetContextKey(c, constant.ContextKeySchedulerReservationID, "res1")
	common.SetContextKey(c, constant.ContextKeySchedulerCandidates, []SchedulerCandidate{{EndpointID: "ep1", ChannelID: 7, KeyIndex: 0, Model: "m", Reason: []string{"healthy"}}, {EndpointID: "ep2", ChannelID: 8, KeyIndex: 0, Model: "m", Reason: []string{"fallback"}}})
	common.SetContextKey(c, constant.ContextKeySchedulerInputTokens, 13)
	common.SetContextKey(c, constant.ContextKeySchedulerOutputTokens, 5)
	common.SetContextKey(c, constant.ContextKeySchedulerStreamStarted, true)
	common.SetContextKey(c, constant.ContextKeySchedulerTTFTMS, 42)
	c.Set(string(constant.ContextKeyRequestStartTime), time.Now().Add(-time.Millisecond))
	c.Writer.WriteHeader(http.StatusOK)
	if err := ReportSchedulerShadowAttempt(c); err != nil {
		t.Fatal(err)
	}
}

func TestRunSchedulerShadowIsDisabledByDefault(t *testing.T) {
	ConfigureSchedulerClientForTest(SchedulerClientConfig{})
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	if err := RunSchedulerShadow(c, "m", "default"); err != nil {
		t.Fatal(err)
	}
	if _, ok := common.GetContextKey(c, constant.ContextKeySchedulerCandidates); ok {
		t.Fatal("disabled shadow mutated context")
	}
}

func TestSchedulerCapabilitiesReadsCachedBodyAndRewinds(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"m","stream":true,"tools":[{"type":"function"}],"response_format":{"type":"json_schema"}}`))
	c.Request.Header.Set("Content-Type", "application/json")
	caps := schedulerCapabilities(c)
	if caps["stream"] != true || caps["tools"] != true || caps["json_mode"] != true {
		t.Fatalf("capabilities=%v", caps)
	}
	var body struct {
		Model  string `json:"model"`
		Stream bool   `json:"stream"`
	}
	if err := common.UnmarshalBodyReusable(c, &body); err != nil {
		t.Fatal(err)
	}
	if body.Model != "m" || !body.Stream {
		t.Fatalf("body was not rewound: %+v", body)
	}
}

func TestRunSchedulerShadowRejectsIncompleteResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"candidates":[{"endpoint_id":"ep1","channel_id":1,"key_index":0,"model":"m","reason":["healthy"]}]}`))
	}))
	defer server.Close()
	ConfigureSchedulerClientForTest(SchedulerClientConfig{Enabled: true, BaseURL: server.URL, Token: "scheduler-test", Timeout: time.Second})
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	if err := RunSchedulerShadow(c, "m", "default"); err == nil {
		t.Fatal("expected incomplete response error")
	}
}

func TestSchedulerCandidateForRetryIsEnforcedOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	candidates := []SchedulerCandidate{{EndpointID: "ep1", ChannelID: 1, KeyIndex: 0, Model: "m"}, {EndpointID: "ep2", ChannelID: 2, KeyIndex: 3, Model: "m", UpstreamModel: "m-upstream"}}
	common.SetContextKey(c, constant.ContextKeySchedulerCandidates, candidates)
	ConfigureSchedulerClientForTest(SchedulerClientConfig{Enabled: true, Mode: "shadow"})
	if _, ok := SchedulerCandidateForRetry(c, 0); ok {
		t.Fatal("shadow mode selected enforced candidate")
	}
	ConfigureSchedulerClientForTest(SchedulerClientConfig{Enabled: true, Mode: "enforced"})
	got, ok := SchedulerCandidateForRetry(c, 1)
	if !ok || got.ChannelID != 2 || common.GetContextKeyInt(c, constant.ContextKeySchedulerKeyIndex) != 3 || common.GetContextKeyString(c, constant.ContextKeySchedulerUpstreamModel) != "m-upstream" {
		t.Fatalf("candidate=%+v ok=%v", got, ok)
	}
}

func TestSchedulerCanaryUsesStableHashAndDisabledConfig(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ConfigureSchedulerClientForTest(SchedulerClientConfig{Enabled: true, Mode: "canary", CanaryPercent: 50, CanarySalt: "test-salt"})
	firstUserRequest, _ := gin.CreateTestContext(httptest.NewRecorder())
	firstUserRequest.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	firstUserRequest.Set("id", 42)
	firstUserRequest.Set(common.RequestIdKey, "request-a")
	secondUserRequest, _ := gin.CreateTestContext(httptest.NewRecorder())
	secondUserRequest.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	secondUserRequest.Set("id", 42)
	secondUserRequest.Set(common.RequestIdKey, "request-b")
	if SchedulerEnforcedForRequest(firstUserRequest) != SchedulerEnforcedForRequest(secondUserRequest) {
		t.Fatal("same user was assigned to different canary cohorts")
	}
	selected := map[bool]bool{}
	for i := 0; i < 200; i++ {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		c.Set(common.RequestIdKey, fmt.Sprintf("canary-%d", i))
		first := SchedulerEnforcedForRequest(c)
		second := SchedulerEnforcedForRequest(c)
		if first != second {
			t.Fatalf("canary decision was not stable for request %d", i)
		}
		selected[first] = true
	}
	if !selected[true] || !selected[false] {
		t.Fatalf("50%% canary did not produce both cohorts: %v", selected)
	}
	ConfigureSchedulerClientForTest(SchedulerClientConfig{Enabled: false, Mode: "canary", CanaryPercent: 100, CanarySalt: "test-salt"})
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Set(common.RequestIdKey, "canary-kill-switch")
	if SchedulerEnforcedForRequest(c) {
		t.Fatal("disabled Scheduler config did not disable canary")
	}
}

func TestReserveSchedulerCandidateUsesDecisionAndAttempt(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/reserve" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer scheduler-test" {
			t.Fatalf("missing scheduler auth")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"reservation_id":"retry-res"}`))
	}))
	defer server.Close()
	ConfigureSchedulerClientForTest(SchedulerClientConfig{Enabled: true, Mode: "enforced", BaseURL: server.URL, Token: "scheduler-test", Timeout: time.Second})
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Set(common.RequestIdKey, "req1")
	common.SetContextKey(c, constant.ContextKeySchedulerDecisionID, "d1")
	if err := ReserveSchedulerCandidate(c, SchedulerCandidate{EndpointID: "ep2"}, 2); err != nil {
		t.Fatal(err)
	}
	if got := common.GetContextKeyString(c, constant.ContextKeySchedulerReservationID); got != "retry-res" {
		t.Fatalf("reservation=%s", got)
	}
}

func TestRecordSchedulerUsageNormalizesOpenAIAndClaudeFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	RecordSchedulerUsage(c, &dto.Usage{PromptTokens: 11, CompletionTokens: 7}, true)
	if got := common.GetContextKeyInt(c, constant.ContextKeySchedulerInputTokens); got != 11 {
		t.Fatalf("input=%d", got)
	}
	if got := common.GetContextKeyInt(c, constant.ContextKeySchedulerOutputTokens); got != 7 {
		t.Fatalf("output=%d", got)
	}
	if !common.GetContextKeyBool(c, constant.ContextKeySchedulerStreamStarted) {
		t.Fatal("stream start not recorded")
	}
	RecordSchedulerUsage(c, &dto.Usage{InputTokens: 13, OutputTokens: 5}, false)
	if got := common.GetContextKeyInt(c, constant.ContextKeySchedulerInputTokens); got != 13 {
		t.Fatalf("claude input=%d", got)
	}
	if got := common.GetContextKeyInt(c, constant.ContextKeySchedulerOutputTokens); got != 5 {
		t.Fatalf("claude output=%d", got)
	}
}

func TestRecordSchedulerUsageMarksMissingProviderUsageUnverified(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	RecordSchedulerUsage(c, nil, false)
	if !common.GetContextKeyBool(c, constant.ContextKeySchedulerUsageUnverified) {
		t.Fatal("missing usage must be marked unverified")
	}
}

func TestRecordSchedulerUsageMarksEstimatedBillingUnverified(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	RecordSchedulerUsage(c, &dto.Usage{PromptTokens: 10, CompletionTokens: 2, BillingUsage: &dto.BillingUsage{Estimated: true}}, false)
	if !common.GetContextKeyBool(c, constant.ContextKeySchedulerUsageUnverified) {
		t.Fatal("estimated provider usage must remain unverified")
	}
}

func TestResetSchedulerAttemptMetricsClearsPriorRetryState(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	RecordSchedulerUsage(c, &dto.Usage{PromptTokens: 10, CompletionTokens: 2}, true)
	common.SetContextKey(c, constant.ContextKeySchedulerTTFTMS, 42)
	ResetSchedulerAttemptMetrics(c)
	if got := common.GetContextKeyInt(c, constant.ContextKeySchedulerInputTokens); got != 0 {
		t.Fatalf("input=%d", got)
	}
	if got := common.GetContextKeyInt(c, constant.ContextKeySchedulerOutputTokens); got != 0 {
		t.Fatalf("output=%d", got)
	}
	if common.GetContextKeyBool(c, constant.ContextKeySchedulerUsageUnverified) || common.GetContextKeyBool(c, constant.ContextKeySchedulerStreamStarted) {
		t.Fatal("retry state was not cleared")
	}
	if got := common.GetContextKeyInt(c, constant.ContextKeySchedulerTTFTMS); got != 0 {
		t.Fatalf("ttft=%d", got)
	}
}
