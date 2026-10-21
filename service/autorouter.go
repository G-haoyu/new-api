package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/pkg/autorouter"
	"github.com/QuantumNous/new-api/pkg/cachex"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/setting/autorouter_setting"
	"github.com/gin-gonic/gin"
	"github.com/samber/hot"
)

const (
	autoRouterCacheNamespace = "new-api:autorouter:v1"
	ginKeyAutoRouterResult   = "autorouter_result"
)

// autoRouterEntry is cached per instruction so every call of one agent loop
// resolves to the same tier without calling jev again.
type autoRouterEntry struct {
	Tier     string               `json:"tier"`
	Decision *autorouter.Decision `json:"decision,omitempty"`
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
	for _, models := range setting.Tiers {
		for _, m := range models {
			if slices.Contains(visible, m) {
				return true
			}
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

// ResolveAutoModel picks the model for an "auto" request. available reports
// whether a model is usable by this request (token whitelist, group abilities,
// request path). A jev failure never fails the request: the tier falls back to
// the previous instruction's tier or the default.
func ResolveAutoModel(c *gin.Context, available func(model string) bool) (*autorouter.Result, error) {
	setting := autorouter_setting.GetAutoRouterSetting()
	policy := setting.Policy()
	result := &autorouter.Result{Tier: policy.DefaultTier, Source: autorouter.SourceDefault}

	var instruction autorouter.Instruction
	if storage, err := common.GetBodyStorage(c); err == nil {
		if body, err := storage.Bytes(); err == nil {
			instruction = autorouter.ExtractInstruction(body)
		}
	}

	if !instruction.Auxiliary && instruction.Text != "" {
		tokenID := common.GetContextKeyInt(c, constant.ContextKeyTokenId)
		cache := getAutoRouterCache()
		ttl := time.Duration(setting.CacheTTLSeconds) * time.Second
		state := autorouter.BuildState(instruction.Text, instruction.Previous, setting.MaxRunes, setting.PreviousMaxRunes)
		decisionKey := autoRouterCacheKey("d", tokenID, autorouter.QuestionsVersion, state["current_task"], state["previous_task"])
		tierKey := autoRouterCacheKey("t", tokenID, instruction.Text)

		if entry, found, _ := cache.Get(decisionKey); found {
			result.Tier, result.Decision, result.Source = entry.Tier, entry.Decision, autorouter.SourceCache
		} else {
			previousTier := ""
			if instruction.Previous != "" {
				if previous, found, _ := cache.Get(autoRouterCacheKey("t", tokenID, instruction.Previous)); found {
					previousTier = previous.Tier
				}
			}

			client := autorouter.JevClient{Endpoint: setting.JevEndpoint, HTTPClient: autoRouterClient}
			ctx, cancel := context.WithTimeout(c.Request.Context(), time.Duration(setting.JevTimeoutMs)*time.Millisecond)
			started := time.Now()
			response, err := client.Classify(ctx, state)
			cancel()
			result.Jev = &autorouter.JevCall{Endpoint: setting.JevEndpoint, LatencyMs: time.Since(started).Milliseconds()}

			inherited := true
			if err != nil {
				result.Jev.Error = err.Error()
				if previousTier != "" {
					result.Tier = previousTier
				}
			} else {
				result.Jev.Response = response.ResponseBody
				decision := response.Decision(instruction.Previous != "")
				result.Decision = &decision
				result.Tier, inherited = autorouter.SelectTier(decision, previousTier, policy)
			}
			switch {
			case !inherited:
				result.Source = autorouter.SourceJev
			case previousTier != "":
				result.Source = autorouter.SourceInherit
			}
			// A fallback is cached too, so a jev outage costs one timeout per
			// instruction rather than one per agent-loop call, and the loop
			// keeps one model.
			entry := autoRouterEntry{Tier: result.Tier, Decision: result.Decision}
			_ = cache.SetWithTTL(decisionKey, entry, ttl)
			_ = cache.SetWithTTL(tierKey, entry, ttl)
		}
	}

	model, usedTier, ok := autorouter.PickModel(result.Tier, policy.Tiers, available)
	if !ok {
		return result, errors.New("auto router found no available model for this token and group")
	}
	result.ResolvedModel, result.Tier = model, usedTier
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
