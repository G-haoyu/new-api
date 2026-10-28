package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/observability"
	"github.com/QuantumNous/new-api/pkg/autorouter"
	"github.com/QuantumNous/new-api/service/capability"
	"github.com/QuantumNous/new-api/setting/autorouter_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

const (
	testJevAPIKey = "test-jev-key"
	testJevModel  = "test/jev"
)

// fakeJev answers by the current task: "hard" is a very difficult coding task,
// "ok" is unclassifiable, anything else is a simple question. Every answer
// continues the previous task. It expects the key and model that
// useAutoRouterSetting configures.
func fakeJev(t *testing.T, calls *atomic.Int32, delay time.Duration) *httptest.Server {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		time.Sleep(delay)
		var payload struct {
			Model string            `json:"model"`
			State map[string]string `json:"state"`
		}
		require.NoError(t, common.DecodeJson(r.Body, &payload))
		assert.Equal(t, "Bearer "+testJevAPIKey, r.Header.Get("Authorization"))
		assert.Equal(t, testJevModel, payload.Model)
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
	t.Setenv("JEV_ENDPOINT", endpoint)
	t.Setenv("JEV_API_KEY", testJevAPIKey)
	t.Setenv("JEV_MODEL", testJevModel)
	setting.Enabled = true
	setting.JevTimeoutMs = timeoutMs
	setting.TaskModels = map[string][]string{
		"coding":    {"coder"},
		"direct_qa": {"chat"},
	}
	setting.DefaultModels = []string{"general"}
	setting.DifficultyEfforts = []string{"low", "medium", "high", "xhigh"}
	setting.DefaultEffort = "medium"
}

func newAutoRouterContext(tokenID int, body string) *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	common.SetContextKey(c, constant.ContextKeyTokenId, tokenID)
	return c
}

// candidatesFrom 把"这个模型名能不能用"的判定包成 AutoCandidates。
// 这些用例不区分客户端的推理字段，也不关心渠道。
func candidatesFrom(serves func(name string) bool) AutoCandidates {
	return AutoCandidates{
		Serves:   func(name string, _ bool) bool { return serves(name) },
		Channels: func(string) []int { return nil },
	}
}

var allModelsAvailable = candidatesFrom(func(string) bool { return true })

func TestResolveAutoModelCachesDecisionPerInstruction(t *testing.T) {
	var calls atomic.Int32
	useAutoRouterSetting(t, fakeJev(t, &calls, 0).URL, 1000)
	body := `{"model":"auto","messages":[{"role":"user","content":"hard"}]}`

	first, err := ResolveAutoModel(newAutoRouterContext(9101, body), allModelsAvailable)
	require.NoError(t, err)
	assert.Equal(t, "coder-xhigh", first.ResolvedModel)
	assert.Equal(t, autorouter.Route{TaskType: "coding", Model: "coder", Effort: "xhigh"}, first.Route)
	assert.Equal(t, autorouter.SourceJev, first.Source)
	require.NotNil(t, first.Jev)
	assert.NotEmpty(t, first.Jev.Response)

	c := newAutoRouterContext(9101, body)
	second, err := ResolveAutoModel(c, allModelsAvailable)
	require.NoError(t, err)
	assert.Equal(t, "coder-xhigh", second.ResolvedModel)
	assert.Equal(t, autorouter.SourceCache, second.Source)
	assert.Nil(t, second.Jev, "a cache hit must not carry jev output")
	assert.Equal(t, int32(1), calls.Load())
	assert.Equal(t, "coding", common.GetContextKeyString(c, constant.ContextKeySchedulerWorkload))

	_, err = ResolveAutoModel(newAutoRouterContext(9102, body), allModelsAvailable)
	require.NoError(t, err)
	assert.Equal(t, int32(2), calls.Load(), "another token must not share the decision")
}

func TestResolveAutoModelFollowsPreviousRoute(t *testing.T) {
	var calls atomic.Int32
	useAutoRouterSetting(t, fakeJev(t, &calls, 0).URL, 1000)

	_, err := ResolveAutoModel(newAutoRouterContext(9201, `{"messages":[{"role":"user","content":"hard"}]}`), allModelsAvailable)
	require.NoError(t, err)

	unknown, err := ResolveAutoModel(newAutoRouterContext(9201, `{"messages":[
		{"role":"user","content":"hard"},{"role":"assistant","content":"done"},{"role":"user","content":"ok"}]}`), allModelsAvailable)
	require.NoError(t, err)
	assert.Equal(t, "coder-xhigh", unknown.ResolvedModel)
	assert.Equal(t, autorouter.SourceInherit, unknown.Source)
	assert.False(t, unknown.EffortOverride, "不可信的判断沿用上一轮路由，不碰客户端的推理字段")
	require.NotNil(t, unknown.Decision)
	assert.Equal(t, autorouter.TaskTypeUnknown, unknown.Decision.TaskType)

	continued, err := ResolveAutoModel(newAutoRouterContext(9201, `{"messages":[
		{"role":"user","content":"hard"},{"role":"assistant","content":"done"},{"role":"user","content":"rename it"}]}`), allModelsAvailable)
	require.NoError(t, err)
	assert.Equal(t, "coder-xhigh", continued.ResolvedModel, "a continuation keeps the model and never lowers the effort")
	assert.Equal(t, autorouter.SourceJev, continued.Source)
	assert.Equal(t, "direct_qa", continued.Decision.TaskType)
	assert.True(t, continued.EffortOverride)
}

func TestResolveAutoModelEncryptedSubAgentInheritsSessionRoute(t *testing.T) {
	var calls atomic.Int32
	useAutoRouterSetting(t, fakeJev(t, &calls, 0).URL, 1000)
	subAgent := func(session string) string {
		return `{"client_metadata":{"session_id":"` + session + `"},"input":[{"type":"agent_message","content":[
			{"type":"input_text","text":"Message Type: NEW_TASK\nTask name: /root/scan\nSender: /root\nPayload:"},
			{"type":"encrypted_content","encrypted_content":"gAAA"}]}]}`
	}

	_, err := ResolveAutoModel(newAutoRouterContext(9251, `{"client_metadata":{"session_id":"sess-a"},"input":[
		{"type":"message","role":"user","content":[{"type":"input_text","text":"hard"}]}]}`), allModelsAvailable)
	require.NoError(t, err)

	result, err := ResolveAutoModel(newAutoRouterContext(9251, subAgent("sess-a")), allModelsAvailable)
	require.NoError(t, err)
	assert.Equal(t, "coder-xhigh", result.ResolvedModel)
	assert.Equal(t, autorouter.SourceInherit, result.Source)
	assert.True(t, result.EffortOverride, "子 agent 继承的是一次可信判断的路由，也继承覆盖推理字段的资格")
	assert.Nil(t, result.Jev)
	assert.Equal(t, int32(1), calls.Load())

	result, err = ResolveAutoModel(newAutoRouterContext(9251, subAgent("sess-b")), allModelsAvailable)
	require.NoError(t, err)
	assert.Equal(t, "general-medium", result.ResolvedModel, "an unknown session falls back to the default route")
	assert.Equal(t, autorouter.SourceDefault, result.Source)
	assert.False(t, result.EffortOverride)
}

func TestResolveAutoModelJevTimeoutFallsBackToDefault(t *testing.T) {
	var calls atomic.Int32
	useAutoRouterSetting(t, fakeJev(t, &calls, 200*time.Millisecond).URL, 50)

	result, err := ResolveAutoModel(newAutoRouterContext(9301, `{"messages":[{"role":"user","content":"hard"}]}`), allModelsAvailable)
	require.NoError(t, err, "a jev failure must not fail the request")
	assert.Equal(t, "general-medium", result.ResolvedModel)
	assert.Equal(t, autorouter.SourceDefault, result.Source)
	require.NotNil(t, result.Jev)
	assert.NotEmpty(t, result.Jev.Error)
	assert.Nil(t, result.Decision)
}

func TestResolveAutoModelOnlyPicksAvailableModels(t *testing.T) {
	var calls atomic.Int32
	useAutoRouterSetting(t, fakeJev(t, &calls, 0).URL, 1000)
	body := `{"messages":[{"role":"user","content":"hard"}]}`

	result, err := ResolveAutoModel(newAutoRouterContext(9401, body), candidatesFrom(func(model string) bool { return model == "coder-high" }))
	require.NoError(t, err)
	assert.Equal(t, "coder-high", result.ResolvedModel, "a missing effort variant falls to the nearest one")

	result, err = ResolveAutoModel(newAutoRouterContext(9401, body), candidatesFrom(func(model string) bool { return model == "general" }))
	require.NoError(t, err)
	assert.Equal(t, int32(1), calls.Load(), "the cached decision is reused, only the route is recomputed")
	assert.Equal(t, "general", result.ResolvedModel, "the bare model runs at the provider default effort")
	assert.Equal(t, "xhigh", result.Effort)

	_, err = ResolveAutoModel(newAutoRouterContext(9401, body), candidatesFrom(func(string) bool { return false }))
	require.Error(t, err)
}

func TestResolveAutoModelRecomputesRouteFromCachedDecision(t *testing.T) {
	var calls atomic.Int32
	useAutoRouterSetting(t, fakeJev(t, &calls, 0).URL, 1000)
	body := `{"messages":[{"role":"user","content":"hard"}]}`

	// 第一次只有兜底模型可用（比如这次请求带图，别的模型能力不满足）。
	first, err := ResolveAutoModel(newAutoRouterContext(9411, body), candidatesFrom(func(model string) bool { return model == "general" }))
	require.NoError(t, err)
	assert.Equal(t, "general", first.ResolvedModel)

	// 同一条指令的下一次请求所有模型都可用：不能被上一次的选择钉住，也不必再调 jev。
	second, err := ResolveAutoModel(newAutoRouterContext(9411, body), allModelsAvailable)
	require.NoError(t, err)
	assert.Equal(t, "coder-xhigh", second.ResolvedModel)
	assert.Equal(t, autorouter.SourceCache, second.Source)
	assert.Equal(t, int32(1), calls.Load())
}

func TestResolveAutoModelSkipsJevWhenNothingIsRoutable(t *testing.T) {
	var calls atomic.Int32
	useAutoRouterSetting(t, fakeJev(t, &calls, 0).URL, 1000)

	_, err := ResolveAutoModel(newAutoRouterContext(9412, `{"messages":[{"role":"user","content":"hard"}]}`), candidatesFrom(func(string) bool { return false }))
	require.Error(t, err)
	assert.Equal(t, int32(0), calls.Load(), "没有任何可用模型时不该花一次 jev 调用")
}

// 候选的用户价格从 AutoCandidates.Price 进来，可信的判断在榜单分接近的模型里选更便宜的。
func TestResolveAutoModelPicksByQualityAndPrice(t *testing.T) {
	var calls atomic.Int32
	useAutoRouterSetting(t, fakeJev(t, &calls, 0).URL, 1000)
	setting := autorouter_setting.GetAutoRouterSetting()
	setting.TaskModels = map[string][]string{"coding": {"opus-m", "sonnet-m"}}
	setting.TaskScores = map[string]map[string]float64{"coding": {"opus-m": 0.97, "sonnet-m": 0.96}}
	setting.CostLambda, setting.ScoreTieDelta = 0.05, 0.02
	prices := map[string]float64{"opus-m": 8, "sonnet-m": 4}
	candidates := func(withPrice func(string) (float64, bool)) AutoCandidates {
		candidates := candidatesFrom(func(string) bool { return true })
		candidates.Price = withPrice
		return candidates
	}
	body := `{"messages":[{"role":"user","content":"hard"}]}`

	t.Run("the cheaper model wins when the scores are close", func(t *testing.T) {
		result, err := ResolveAutoModel(newAutoRouterContext(9801, body), candidates(func(model string) (float64, bool) {
			price, ok := prices[model]
			return price, ok
		}))
		require.NoError(t, err)
		assert.Equal(t, "sonnet-m-xhigh", result.ResolvedModel)
		require.NotNil(t, result.Pick)
		assert.Equal(t, "sonnet-m", result.Pick.Chosen)
		assert.Len(t, result.Pick.Band, 2)
	})

	t.Run("a model without a comparable price does not take part", func(t *testing.T) {
		result, err := ResolveAutoModel(newAutoRouterContext(9802, body), candidates(func(model string) (float64, bool) {
			return prices[model], model == "opus-m"
		}))
		require.NoError(t, err)
		assert.Equal(t, "opus-m-xhigh", result.ResolvedModel)
	})

	t.Run("without a price source the first available model in list order wins", func(t *testing.T) {
		result, err := ResolveAutoModel(newAutoRouterContext(9803, body), candidates(nil))
		require.NoError(t, err)
		assert.Equal(t, "opus-m-xhigh", result.ResolvedModel)
		assert.Nil(t, result.Pick)
	})
}

func TestResolveAutoModelRemembersJevFailureBriefly(t *testing.T) {
	var calls atomic.Int32
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(failing.Close)
	useAutoRouterSetting(t, failing.URL, 1000)
	body := `{"messages":[{"role":"user","content":"hard"}]}`

	first, err := ResolveAutoModel(newAutoRouterContext(9413, body), allModelsAvailable)
	require.NoError(t, err, "a jev failure must not fail the request")
	assert.Equal(t, "general-medium", first.ResolvedModel)
	require.NotNil(t, first.Jev)
	assert.NotEmpty(t, first.Jev.Error)

	second, err := ResolveAutoModel(newAutoRouterContext(9413, body), allModelsAvailable)
	require.NoError(t, err)
	assert.Equal(t, "general-medium", second.ResolvedModel)
	assert.Nil(t, second.Jev, "刚失败过的指令不再调 jev")
	assert.Equal(t, int32(1), calls.Load())
}

func TestResolveAutoModelConvergesEffortForDeclaredReasoningModels(t *testing.T) {
	setupReasoningCapabilities(t)
	var calls atomic.Int32
	useAutoRouterSetting(t, fakeJev(t, &calls, 0).URL, 1000)
	setting := autorouter_setting.GetAutoRouterSetting()
	setting.TaskModels = map[string][]string{"coding": {"reasoner"}, "direct_qa": {"no-think"}}
	setting.DefaultModels = []string{"reasoner"}
	withChannels := func(channels ...int) AutoCandidates {
		return AutoCandidates{Serves: func(string, bool) bool { return true }, Channels: func(string) []int { return channels }}
	}

	t.Run("a trusted decision converges the effort and the final filter judges it", func(t *testing.T) {
		// 客户端自己要 xhigh；请求带 tools，所以只有渠道 11、12 能服务（13 不支持 tools）。
		gate := &CapabilityGate{mode: CapabilityGateEnforce, needs: &capability.Needs{ToolsCount: 1, ReasoningEffort: "xhigh"}, modelVerdicts: map[string]capabilityVerdict{}}
		c := newAutoRouterContext(9701, `{"messages":[{"role":"user","content":"hard"}]}`)
		common.SetContextKey(c, constant.ContextKeyCapabilityGate, gate)

		result, err := ResolveAutoModel(c, withChannels(11, 12, 13))
		require.NoError(t, err)
		assert.Equal(t, "reasoner", result.ResolvedModel, "声明了推理的模型用裸模型名，档位不拼后缀")
		assert.True(t, result.EffortInBody)
		assert.True(t, result.EffortOverride)
		assert.Equal(t, "xhigh", result.Effort, "难度给出的期望档位")
		assert.Equal(t, "high", result.AppliedEffort, "渠道 11、12 声明过的档位最高到 high")
		assert.Equal(t, autorouter.EffortSet, result.EffortAction)
		assert.Equal(t, []int{12}, gate.FilterChannels(&gin.Context{}, "reasoner", []int{11, 12, 13}),
			"最终的渠道过滤按写进请求体的 high 判定：渠道 11 没声明 high")
	})

	t.Run("a model that does not support reasoning has the client's fields stripped", func(t *testing.T) {
		result, err := ResolveAutoModel(newAutoRouterContext(9702, `{"messages":[{"role":"user","content":"what is 2+2"}]}`), withChannels(15))
		require.NoError(t, err)
		assert.Equal(t, "no-think", result.ResolvedModel)
		assert.Equal(t, autorouter.EffortStrip, result.EffortAction)
		assert.Empty(t, result.AppliedEffort)
	})

	t.Run("an untrusted fallback leaves the client's reasoning fields alone", func(t *testing.T) {
		result, err := ResolveAutoModel(newAutoRouterContext(9703, `{"messages":[{"role":"user","content":"ok"}]}`), withChannels(11, 12))
		require.NoError(t, err)
		assert.Equal(t, "reasoner", result.ResolvedModel)
		assert.True(t, result.EffortInBody)
		assert.False(t, result.EffortOverride)
		assert.Equal(t, autorouter.EffortKeep, result.EffortAction)
		assert.Empty(t, result.AppliedEffort)
	})

	t.Run("a model without a reasoning declaration keeps the model-name suffix", func(t *testing.T) {
		setting.TaskModels = map[string][]string{"coding": {"gated-model"}}
		result, err := ResolveAutoModel(newAutoRouterContext(9704, `{"messages":[{"role":"user","content":"hard"}]}`), withChannels(1))
		require.NoError(t, err)
		assert.Equal(t, "gated-model-xhigh", result.ResolvedModel)
		assert.False(t, result.EffortInBody)
		assert.Empty(t, result.EffortAction)
	})
}

func TestResolveAutoModelAuxiliarySkipsJev(t *testing.T) {
	var calls atomic.Int32
	useAutoRouterSetting(t, fakeJev(t, &calls, 0).URL, 1000)

	result, err := ResolveAutoModel(newAutoRouterContext(9501, `{"messages":[{"role":"user","content":"[SUGGESTION MODE: next prompt]"}]}`), allModelsAvailable)
	require.NoError(t, err)
	assert.Equal(t, "general-medium", result.ResolvedModel)
	assert.Equal(t, autorouter.SourceDefault, result.Source)
	assert.Equal(t, int32(0), calls.Load())
}

func TestResolveAutoModelTraceSpans(t *testing.T) {
	newTracedContext := func(t *testing.T, tokenID int, body string) (*gin.Context, *tracetest.InMemoryExporter) {
		exporter := tracetest.NewInMemoryExporter()
		provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(exporter)))
		t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
		c := newAutoRouterContext(tokenID, body)
		c.Request = c.Request.WithContext(observability.ContextWithRuntimeForTest(c.Request.Context(), provider))
		return c, exporter
	}
	spansByName := func(exporter *tracetest.InMemoryExporter) map[string]tracetest.SpanStub {
		spans := map[string]tracetest.SpanStub{}
		for _, span := range exporter.GetSpans() {
			spans[span.Name] = span
		}
		return spans
	}
	attr := func(span tracetest.SpanStub, key string) string {
		for _, kv := range span.Attributes {
			if string(kv.Key) == key {
				return kv.Value.AsString()
			}
		}
		return ""
	}
	body := `{"messages":[{"role":"user","content":"hard"}]}`

	t.Run("jev called then cache hit", func(t *testing.T) {
		var calls atomic.Int32
		useAutoRouterSetting(t, fakeJev(t, &calls, 0).URL, 1000)

		c, exporter := newTracedContext(t, 9601, body)
		_, err := ResolveAutoModel(c, allModelsAvailable)
		require.NoError(t, err)
		spans := spansByName(exporter)
		require.Len(t, spans, 2)
		route, jev := spans["newapi.auto_router"], spans["newapi.jev.classify"]
		assert.Equal(t, route.SpanContext.SpanID(), jev.Parent.SpanID())
		assert.Equal(t, "generation", attr(jev, "langfuse.observation.type"))
		assert.Contains(t, attr(jev, "langfuse.observation.input"), `"current_task":"hard"`)
		assert.Contains(t, attr(jev, "langfuse.observation.output"), `"task_type"`)
		assert.Equal(t, autorouter.SourceJev, attr(route, "langfuse.observation.metadata.source"))
		assert.Equal(t, "coder-xhigh", attr(route, "langfuse.observation.metadata.resolved_model"))
		assert.Equal(t, "coder", attr(route, "langfuse.trace.metadata.auto_router_model"))
		assert.Equal(t, "xhigh", attr(route, "langfuse.trace.metadata.auto_router_effort"))
		assert.Empty(t, attr(route, "langfuse.observation.level"))

		c, exporter = newTracedContext(t, 9601, body)
		_, err = ResolveAutoModel(c, allModelsAvailable)
		require.NoError(t, err)
		spans = spansByName(exporter)
		require.Len(t, spans, 1, "a cache hit must not record a jev call")
		assert.Equal(t, autorouter.SourceCache, attr(spans["newapi.auto_router"], "langfuse.observation.metadata.source"))
	})

	t.Run("jev timeout", func(t *testing.T) {
		var calls atomic.Int32
		useAutoRouterSetting(t, fakeJev(t, &calls, 200*time.Millisecond).URL, 50)

		c, exporter := newTracedContext(t, 9602, body)
		_, err := ResolveAutoModel(c, allModelsAvailable)
		require.NoError(t, err)
		spans := spansByName(exporter)
		require.Len(t, spans, 2)
		assert.Equal(t, codes.Error, spans["newapi.jev.classify"].Status.Code)
		route := spans["newapi.auto_router"]
		assert.Equal(t, "WARNING", attr(route, "langfuse.observation.level"))
		assert.Contains(t, attr(route, "langfuse.observation.status_message"), "jev failed")
	})

	t.Run("no available model", func(t *testing.T) {
		useAutoRouterSetting(t, "", 1000)

		c, exporter := newTracedContext(t, 9603, `{"messages":[{"role":"user","content":"[SUGGESTION MODE: next prompt]"}]}`)
		_, err := ResolveAutoModel(c, candidatesFrom(func(string) bool { return false }))
		require.Error(t, err)
		spans := spansByName(exporter)
		require.Len(t, spans, 1)
		route := spans["newapi.auto_router"]
		assert.Equal(t, codes.Error, route.Status.Code)
		assert.JSONEq(t, `{"auxiliary":true}`, attr(route, "langfuse.observation.input"))
	})
}

func TestAutoRouterListedOnlyWhenResolvable(t *testing.T) {
	useAutoRouterSetting(t, "", 1000)

	assert.True(t, AutoRouterListed([]string{"other", "coder-high"}), "an effort variant makes its model resolvable")
	assert.True(t, AutoRouterListed([]string{"general"}))
	assert.False(t, AutoRouterListed([]string{"other", "coder-turbo"}))
	assert.False(t, AutoRouterListed([]string{"auto", "chat"}), "a real auto model is listed already")

	autorouter_setting.GetAutoRouterSetting().Enabled = false
	assert.False(t, AutoRouterListed([]string{"chat"}))
}
