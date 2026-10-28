package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/observability"
	"github.com/QuantumNous/new-api/pkg/autorouter"
	"github.com/QuantumNous/new-api/pkg/cachex"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/setting/autorouter_setting"
	"github.com/gin-gonic/gin"
	"github.com/samber/hot"
)

const (
	autoRouterCacheNamespace = "new-api:autorouter:v3"
	ginKeyAutoRouterResult   = "autorouter_result"
	// autoRouterFailureTTL 是 jev 调用失败后不再重试的时长。agent 循环里同一条指令会
	// 连发很多请求，jev 故障时不记一下，每个请求都要白等一次超时；
	// 时间取得短，是为了 jev 恢复后能很快重新分类，而不是把兜底结果钉住整个缓存周期。
	autoRouterFailureTTL = 60 * time.Second
)

// autoRouterEntry 按键的用途存不同的字段：
//   - d: 键只存 jev 的判断（Decision），或"刚失败过"的标记（Failed）。
//     同一条指令的后续请求不再调 jev，但路由每次按各自的可用集合重新算，
//     所以一次带图请求因为能力不满足换了模型，不会影响之后的纯文本请求。
//   - r: 和 s: 键存最近一次算出的路由（Route），供下一条指令"沿用上一轮模型"，
//     以及没有可读文字的子 agent 继承会话路由。
type autoRouterEntry struct {
	Route    autorouter.Route     `json:"route"`
	Decision *autorouter.Decision `json:"decision,omitempty"`
	Failed   bool                 `json:"failed,omitempty"`
}

var (
	autoRouterCacheOnce sync.Once
	autoRouterCache     *cachex.HybridCache[autoRouterEntry]
	autoRouterClient    = &http.Client{}
)

func getAutoRouterCache() *cachex.HybridCache[autoRouterEntry] {
	autoRouterCacheOnce.Do(func() {
		autoRouterCache = cachex.NewHybridCache[autoRouterEntry](cachex.HybridCacheConfig[autoRouterEntry]{
			Namespace: cachex.Namespace(autoRouterCacheNamespace),
			Redis:     common.RDB,
			RedisEnabled: func() bool {
				return common.RedisEnabled && common.RDB != nil
			},
			RedisCodec: cachex.JSONCodec[autoRouterEntry]{},
			Memory: func() *hot.HotCache[string, autoRouterEntry] {
				return hot.NewHotCache[string, autoRouterEntry](hot.LRU, 100_000).
					WithTTL(2 * time.Hour).
					WithJanitor().
					Build()
			},
		})
	})
	return autoRouterCache
}

// IsAutoRouterRequest reports whether the request asks the gateway to pick
// the model. Only conversational endpoints carry a human instruction to
// classify; elsewhere "auto" stays an ordinary, unknown model name.
func IsAutoRouterRequest(c *gin.Context, modelName string) bool {
	if modelName != autorouter.AutoModel || !autorouter_setting.GetAutoRouterSetting().Enabled {
		return false
	}
	path := c.Request.URL.Path
	mode := relayconstant.Path2RelayMode(path)
	return mode == relayconstant.RelayModeChatCompletions || mode == relayconstant.RelayModeResponses ||
		strings.HasPrefix(path, "/v1/messages")
}

// AutoRouterListed reports whether "auto" belongs in a model list containing
// visible: only when it can resolve to one of them.
func AutoRouterListed(visible []string) bool {
	setting := autorouter_setting.GetAutoRouterSetting()
	if !setting.Enabled || slices.Contains(visible, autorouter.AutoModel) {
		return false
	}
	isVisible := func(model string) bool { return slices.Contains(visible, model) }
	if slices.ContainsFunc(setting.DefaultModels, func(model string) bool { return autorouter.ModelAvailable(model, isVisible) }) {
		return true
	}
	for _, models := range setting.TaskModels {
		if slices.ContainsFunc(models, func(model string) bool { return autorouter.ModelAvailable(model, isVisible) }) {
			return true
		}
	}
	return false
}

// autoRouterCacheKey scopes keys by token so short instructions such as "ok"
// from different users never share a decision, and instruction text never
// crosses tenants.
func autoRouterCacheKey(kind string, tokenID int, parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(append([]string{strconv.Itoa(tokenID)}, parts...), "\x00")))
	return kind + ":" + hex.EncodeToString(sum[:])
}

// ResolveAutoModel picks the model and reasoning effort for an "auto" request.
// available reports whether a model name is usable by this request (token
// whitelist, group abilities, request path). A jev failure never fails the
// request: the route falls back to the previous instruction's route or the
// defaults.
func ResolveAutoModel(c *gin.Context, available func(model string) bool) (*autorouter.Result, error) {
	setting := autorouter_setting.GetAutoRouterSetting()
	policy := setting.Policy()
	result := &autorouter.Result{Source: autorouter.SourceDefault}

	otelRuntime := observability.FromContext(c.Request.Context())
	routeCtx, routeSpan := otelRuntime.StartAutoRouter(c.Request.Context())
	routeInput := map[string]any{}
	route := observability.Observation{Input: routeInput}

	var instruction autorouter.Instruction
	if storage, err := common.GetBodyStorage(c); err == nil {
		if body, err := storage.Bytes(); err == nil {
			instruction = autorouter.ExtractInstruction(body)
		}
	}
	if instruction.Auxiliary {
		routeInput["auxiliary"] = true
	}

	ok := false
	tokenID := common.GetContextKeyInt(c, constant.ContextKeyTokenId)
	cache := getAutoRouterCache()
	ttl := time.Duration(setting.CacheTTLSeconds) * time.Second
	switch {
	case !policy.AnyModelAvailable(available):
		// 一个可用模型都没有时，分类得出什么都选不出路由，不必先花一次 jev 调用。
	case instruction.Auxiliary:
	case instruction.Text == "" && instruction.Session != "":
		if entry, found, _ := cache.Get(autoRouterCacheKey("s", tokenID, instruction.Session)); found && autorouter.ModelAvailable(entry.Route.Model, available) {
			result.Route, result.Source = entry.Route, autorouter.SourceInherit
			ok = true
		}
	case instruction.Text != "":
		state := autorouter.BuildState(instruction.Text, instruction.Previous, setting.MaxRunes, setting.PreviousMaxRunes)
		if otelRuntime.RecordsContent(nil) {
			for key, value := range state {
				routeInput[key] = value
			}
		}
		decisionKey := autoRouterCacheKey("d", tokenID, autorouter.QuestionsVersion, state["current_task"], state["previous_task"])
		routeKey := autoRouterCacheKey("r", tokenID, instruction.Text)

		var previous *autorouter.Route
		if instruction.Previous != "" {
			if entry, found, _ := cache.Get(autoRouterCacheKey("r", tokenID, instruction.Previous)); found {
				previous = &entry.Route
			}
		}
		routeInput["previous_route"] = previous

		var decision *autorouter.Decision
		fromCache := false
		if entry, found, _ := cache.Get(decisionKey); found && (entry.Decision != nil || entry.Failed) {
			decision, fromCache = entry.Decision, entry.Decision != nil
			if entry.Failed {
				route.Warning = "jev failed recently, not retried"
			}
		} else {
			client := autorouter.JevClient{
				Endpoint:   os.Getenv("JEV_ENDPOINT"),
				Model:      os.Getenv("JEV_MODEL"),
				APIKey:     os.Getenv("JEV_API_KEY"),
				HTTPClient: autoRouterClient,
			}
			jevCtx, jevSpan := otelRuntime.StartJevClassify(routeCtx)
			ctx, cancel := context.WithTimeout(jevCtx, time.Duration(setting.JevTimeoutMs)*time.Millisecond)
			started := time.Now()
			response, err := client.Classify(ctx, state)
			cancel()
			result.Jev = &autorouter.JevCall{Endpoint: client.Endpoint, LatencyMs: time.Since(started).Milliseconds()}
			jev := observability.Observation{Err: err}
			jevInput := map[string]any{"questions_version": autorouter.QuestionsVersion}
			if otelRuntime.RecordsContent(err) {
				jevInput["state"] = state
			}
			jev.Input = jevInput

			if err != nil {
				result.Jev.Error = err.Error()
				route.Warning = "jev failed: " + err.Error()
				_ = cache.SetWithTTL(decisionKey, autoRouterEntry{Failed: true}, autoRouterFailureTTL)
			} else {
				result.Jev.Response = response.ResponseBody
				decided := response.Decision(instruction.Previous != "")
				decision = &decided
				jev.Model = response.Model
				jevOutput := map[string]any{"answers": response.Answers}
				if response.LatencyMs != nil {
					jevOutput["server_latency_ms"] = *response.LatencyMs
				}
				jev.Output = jevOutput
				_ = cache.SetWithTTL(decisionKey, autoRouterEntry{Decision: decision}, ttl)
			}
			otelRuntime.FinishObservation(jevSpan, jev)
		}

		// 路由每次按本次请求自己的可用集合重新算，缓存里只有 jev 的判断。
		var inherited bool
		result.Decision = decision
		result.Route, inherited, ok = autorouter.SelectRoute(decision, previous, policy, available)
		if inherited && decision != nil {
			route.Warning = "jev decision untrusted"
		}
		switch {
		case !inherited && fromCache:
			result.Source = autorouter.SourceCache
		case !inherited:
			result.Source = autorouter.SourceJev
		case previous != nil && result.Route == *previous:
			result.Source = autorouter.SourceInherit
		}
		// 兜底路由也记下来，下一条指令才能沿用同一个模型，agent 循环不会来回换模型。
		if ok {
			_ = cache.SetWithTTL(routeKey, autoRouterEntry{Route: result.Route}, ttl)
		}
		// The session remembers its latest human instruction's route for the
		// sub-agents whose task arrives without readable text.
		if ok && instruction.Session != "" && !instruction.Agent {
			_ = cache.SetWithTTL(autoRouterCacheKey("s", tokenID, instruction.Session), autoRouterEntry{Route: result.Route}, ttl)
		}
	}
	if !ok {
		result.Route, _, ok = autorouter.SelectRoute(nil, nil, policy, available)
	}

	if ok {
		result.ResolvedModel, ok = autorouter.ResolveVariant(result.Model, result.Effort, available)
	}
	if !ok {
		route.Err = errors.New("auto router found no available model for this token and group")
	}
	route.Output = result
	route.Metadata = map[string]string{
		"route_model":    result.Model,
		"route_effort":   result.Effort,
		"source":         result.Source,
		"resolved_model": result.ResolvedModel,
	}
	route.TraceMetadata = map[string]string{
		"auto_router_model":  result.Model,
		"auto_router_effort": result.Effort,
		"auto_router_source": result.Source,
	}
	otelRuntime.FinishObservation(routeSpan, route)
	if !ok {
		return result, route.Err
	}
	c.Set(ginKeyAutoRouterResult, result)
	if result.Decision != nil && result.Decision.TaskType != autorouter.TaskTypeUnknown &&
		common.GetContextKeyString(c, constant.ContextKeySchedulerWorkload) == "" {
		common.SetContextKey(c, constant.ContextKeySchedulerWorkload, result.Decision.TaskType)
	}
	return result, nil
}

// GetAutoRouterResult returns the routing outcome stored by ResolveAutoModel.
func GetAutoRouterResult(c *gin.Context) *autorouter.Result {
	value, ok := c.Get(ginKeyAutoRouterResult)
	if !ok {
		return nil
	}
	result, _ := value.(*autorouter.Result)
	return result
}
