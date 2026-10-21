package service

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/pkg/autorouter"
	"github.com/QuantumNous/new-api/setting/autorouter_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeJev answers by the current task: "hard" is a difficult coding task,
// "ok" is unclassifiable, anything else is a simple question.
func fakeJev(t *testing.T, calls *atomic.Int32, delay time.Duration) *httptest.Server {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		time.Sleep(delay)
		var payload struct {
			State map[string]string `json:"state"`
		}
		require.NoError(t, common.DecodeJson(r.Body, &payload))
		taskType, difficulty := "direct_qa", "0"
		switch payload.State["current_task"] {
		case "hard":
			taskType, difficulty = "coding", "3"
		case "ok":
			taskType = autorouter.TaskTypeUnknown
		}
		_, _ = w.Write([]byte(`{"answers":{
			"task_type":{"type":"choice","choice":"` + taskType + `","probabilities":{"` + taskType + `":0.9}},
			"difficulty":{"type":"score","probabilities":{"` + difficulty + `":0.8}},
			"task_change":{"type":"noul","noul":0.1},
			"larger_model_benefit":{"type":"noul","noul":0.1}}}`))
	}))
	t.Cleanup(server.Close)
	return server
}

func useAutoRouterSetting(t *testing.T, endpoint string, timeoutMs int) {
	setting := autorouter_setting.GetAutoRouterSetting()
	saved := *setting
	t.Cleanup(func() { *setting = saved })
	setting.Enabled = true
	setting.JevEndpoint = endpoint
	setting.JevTimeoutMs = timeoutMs
	setting.Tiers = map[string][]string{
		autorouter.TierLow:  {"small"},
		autorouter.TierMid:  {"medium"},
		autorouter.TierHigh: {"large"},
	}
}

func newAutoRouterContext(tokenID int, body string) *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	common.SetContextKey(c, constant.ContextKeyTokenId, tokenID)
	return c
}

func allModelsAvailable(string) bool { return true }

func TestResolveAutoModelCachesDecisionPerInstruction(t *testing.T) {
	var calls atomic.Int32
	useAutoRouterSetting(t, fakeJev(t, &calls, 0).URL, 1000)
	body := `{"model":"auto","messages":[{"role":"user","content":"hard"}]}`

	first, err := ResolveAutoModel(newAutoRouterContext(9101, body), allModelsAvailable)
	require.NoError(t, err)
	assert.Equal(t, "large", first.ResolvedModel)
	assert.Equal(t, autorouter.SourceJev, first.Source)
	require.NotNil(t, first.Jev)
	assert.NotEmpty(t, first.Jev.Response)

	c := newAutoRouterContext(9101, body)
	second, err := ResolveAutoModel(c, allModelsAvailable)
	require.NoError(t, err)
	assert.Equal(t, "large", second.ResolvedModel)
	assert.Equal(t, autorouter.SourceCache, second.Source)
	assert.Nil(t, second.Jev, "a cache hit must not carry jev output")
	assert.Equal(t, int32(1), calls.Load())
	assert.Equal(t, "coding", common.GetContextKeyString(c, constant.ContextKeySchedulerWorkload))

	_, err = ResolveAutoModel(newAutoRouterContext(9102, body), allModelsAvailable)
	require.NoError(t, err)
	assert.Equal(t, int32(2), calls.Load(), "another token must not share the decision")
}

func TestResolveAutoModelUnknownInheritsPreviousTier(t *testing.T) {
	var calls atomic.Int32
	useAutoRouterSetting(t, fakeJev(t, &calls, 0).URL, 1000)

	_, err := ResolveAutoModel(newAutoRouterContext(9201, `{"messages":[{"role":"user","content":"hard"}]}`), allModelsAvailable)
	require.NoError(t, err)

	result, err := ResolveAutoModel(newAutoRouterContext(9201, `{"messages":[
		{"role":"user","content":"hard"},{"role":"assistant","content":"done"},{"role":"user","content":"ok"}]}`), allModelsAvailable)
	require.NoError(t, err)
	assert.Equal(t, "large", result.ResolvedModel)
	assert.Equal(t, autorouter.SourceInherit, result.Source)
	require.NotNil(t, result.Decision)
	assert.Equal(t, autorouter.TaskTypeUnknown, result.Decision.TaskType)
}

func TestResolveAutoModelJevTimeoutFallsBackToDefault(t *testing.T) {
	var calls atomic.Int32
	useAutoRouterSetting(t, fakeJev(t, &calls, 200*time.Millisecond).URL, 50)

	result, err := ResolveAutoModel(newAutoRouterContext(9301, `{"messages":[{"role":"user","content":"hard"}]}`), allModelsAvailable)
	require.NoError(t, err, "a jev failure must not fail the request")
	assert.Equal(t, "medium", result.ResolvedModel)
	assert.Equal(t, autorouter.SourceDefault, result.Source)
	require.NotNil(t, result.Jev)
	assert.NotEmpty(t, result.Jev.Error)
	assert.Nil(t, result.Decision)
}

func TestResolveAutoModelOnlyPicksAvailableModels(t *testing.T) {
	var calls atomic.Int32
	useAutoRouterSetting(t, fakeJev(t, &calls, 0).URL, 1000)
	body := `{"messages":[{"role":"user","content":"hard"}]}`

	result, err := ResolveAutoModel(newAutoRouterContext(9401, body), func(model string) bool { return model == "small" })
	require.NoError(t, err)
	assert.Equal(t, "small", result.ResolvedModel)
	assert.Equal(t, autorouter.TierLow, result.Tier)

	_, err = ResolveAutoModel(newAutoRouterContext(9401, body), func(string) bool { return false })
	require.Error(t, err)
}

func TestResolveAutoModelAuxiliarySkipsJev(t *testing.T) {
	var calls atomic.Int32
	useAutoRouterSetting(t, fakeJev(t, &calls, 0).URL, 1000)

	result, err := ResolveAutoModel(newAutoRouterContext(9501, `{"messages":[{"role":"user","content":"[SUGGESTION MODE: next prompt]"}]}`), allModelsAvailable)
	require.NoError(t, err)
	assert.Equal(t, "medium", result.ResolvedModel)
	assert.Equal(t, autorouter.SourceDefault, result.Source)
	assert.Equal(t, int32(0), calls.Load())
}

func TestAutoRouterListedOnlyWhenResolvable(t *testing.T) {
	useAutoRouterSetting(t, "", 1000)

	assert.True(t, AutoRouterListed([]string{"other", "medium"}))
	assert.False(t, AutoRouterListed([]string{"other"}))
	assert.False(t, AutoRouterListed([]string{"auto", "medium"}), "a real auto model is listed already")

	autorouter_setting.GetAutoRouterSetting().Enabled = false
	assert.False(t, AutoRouterListed([]string{"medium"}))
}
