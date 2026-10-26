package service

import (
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/capability"
	"github.com/QuantumNous/new-api/setting/reasoning"

	"github.com/gin-gonic/gin"
)

// CapabilityGateMode 决定能力门控如何参与路由。
type CapabilityGateMode string

const (
	CapabilityGateOff     CapabilityGateMode = "off"
	CapabilityGateShadow  CapabilityGateMode = "shadow"
	CapabilityGateEnforce CapabilityGateMode = "enforce"
)

// CurrentCapabilityGateMode 读取 AUTO_CAPABILITY_GATE_MODE，默认 shadow。
func CurrentCapabilityGateMode() CapabilityGateMode {
	switch CapabilityGateMode(strings.ToLower(strings.TrimSpace(os.Getenv("AUTO_CAPABILITY_GATE_MODE")))) {
	case CapabilityGateOff:
		return CapabilityGateOff
	case CapabilityGateEnforce:
		return CapabilityGateEnforce
	default:
		return CapabilityGateShadow
	}
}

// CapabilityGate 是请求级能力闸门。
//
// 模型级判定服务 auto 选模；渠道级判定在选定模型后对候选渠道做合取，
// 结果同时注册为 FilterCapability 约束，重试沿用同一条过滤链。
// 解析失败、请求体不可用或未声明任何能力时返回 nil，调用方按"不拦截"处理：
// 宁可漏掉一次约束，也不能让门控抖动把流量打挂。
type CapabilityGate struct {
	mode  CapabilityGateMode
	needs *capability.Needs

	modelVerdicts map[string]capabilityVerdict
	evaluated     int
	wouldReject   []string
	rejected      []string

	channelModel      string
	channelsEvaluated int
	channelsRejected  []int
}

type capabilityVerdict struct {
	declared  bool
	satisfied bool
}

// CapabilityGateSummary 是写进日志的门控摘要。
type CapabilityGateSummary struct {
	Mode              CapabilityGateMode `json:"mode"`
	Evaluated         int                `json:"evaluated"`
	WouldReject       []string           `json:"would_reject,omitempty"`
	Rejected          []string           `json:"rejected,omitempty"`
	ChannelModel      string             `json:"channel_model,omitempty"`
	ChannelsEvaluated int                `json:"channels_evaluated,omitempty"`
	ChannelsRejected  []int              `json:"channels_rejected,omitempty"`
}

// BuildCapabilityGate 解析一次请求体并构造闸门。非四协议入口返回 nil。
func BuildCapabilityGate(c *gin.Context) *CapabilityGate {
	mode := CurrentCapabilityGateMode()
	if mode == CapabilityGateOff {
		return nil
	}
	if !model.HasCapabilityDeclarations() {
		return nil
	}
	format, ok := capabilityGateFormat(c.Request.URL.Path)
	if !ok {
		return nil
	}
	body, ok := capabilityGateBody(c, capabilityGateMaxBodyBytes())
	if !ok {
		return nil
	}
	needs, err := capability.Extract(format, body)
	if err != nil {
		logger.LogWarn(c.Request.Context(), common.LogText("capability gate skipped: request body could not be parsed: %v", err))
		return nil
	}
	gate := &CapabilityGate{
		mode:          mode,
		needs:         needs,
		modelVerdicts: make(map[string]capabilityVerdict),
	}
	common.SetContextKey(c, constant.ContextKeyCapabilityGate, gate)
	return gate
}

// CapabilityGateFromContext 返回本请求构建的闸门。
func CapabilityGateFromContext(c *gin.Context) *CapabilityGate {
	gate, ok := common.GetContextKeyType[*CapabilityGate](c, constant.ContextKeyCapabilityGate)
	if !ok {
		return nil
	}
	return gate
}

// Allows 返回 auto 候选模型是否通过模型级能力判定。
// 没有模型级声明视为不合格；shadow 模式只记录，不拦截。
func (g *CapabilityGate) Allows(modelName string) bool {
	if g == nil {
		return true
	}
	baseModel := reasoning.BaseModelName(modelName)
	if baseModel == "" {
		baseModel = modelName
	}
	verdict, seen := g.modelVerdicts[baseModel]
	if !seen {
		spec, declared := model.ResolveModelCapability(baseModel)
		satisfied := declared && capability.Satisfied(capability.Evaluate(spec, g.needs))
		verdict = capabilityVerdict{declared: declared, satisfied: satisfied}
		g.modelVerdicts[baseModel] = verdict
		g.evaluated++
		if !satisfied {
			g.wouldReject = append(g.wouldReject, baseModel)
		}
	}
	if verdict.satisfied {
		return true
	}
	if g.mode != CapabilityGateEnforce {
		return true
	}
	g.rejected = append(g.rejected, baseModel)
	return false
}

// FilterChannels 在选定模型后对候选渠道做一次能力合取。
// shadow 模式只记录应被剔除的渠道，enforce 模式返回过滤后的集合。
func (g *CapabilityGate) FilterChannels(c *gin.Context, modelName string, channelIDs []int) []int {
	if g == nil || len(channelIDs) == 0 {
		return channelIDs
	}
	baseModel := reasoning.BaseModelName(modelName)
	if baseModel == "" {
		baseModel = modelName
	}
	_, declared := model.ResolveModelCapability(baseModel)

	kept := make([]int, 0, len(channelIDs))
	rejected := make([]int, 0)
	for _, channelID := range channelIDs {
		if !declared {
			rejected = append(rejected, channelID)
			continue
		}
		effective, ok := model.EffectiveChannelCapability(channelID, baseModel)
		if !ok || !capability.Satisfied(capability.Evaluate(effective, g.needs)) {
			rejected = append(rejected, channelID)
			continue
		}
		kept = append(kept, channelID)
	}
	g.channelModel = baseModel
	g.channelsEvaluated = len(channelIDs)
	g.channelsRejected = rejected
	if g.mode != CapabilityGateEnforce {
		return channelIDs
	}
	return kept
}

// Summary 返回可写进日志的摘要；没有结论时返回 nil。
func (g *CapabilityGate) Summary() *CapabilityGateSummary {
	if g == nil || (g.evaluated == 0 && g.channelsEvaluated == 0) {
		return nil
	}
	return &CapabilityGateSummary{
		Mode:              g.mode,
		Evaluated:         g.evaluated,
		WouldReject:       dedupeSorted(g.wouldReject),
		Rejected:          dedupeSorted(g.rejected),
		ChannelModel:      g.channelModel,
		ChannelsEvaluated: g.channelsEvaluated,
		ChannelsRejected:  append([]int{}, g.channelsRejected...),
	}
}

// LogSummary 把本次门控结论写进服务端日志，供 shadow 评估。
func (g *CapabilityGate) LogSummary(c *gin.Context) {
	summary := g.Summary()
	if summary == nil {
		return
	}
	if len(summary.WouldReject) == 0 && len(summary.ChannelsRejected) == 0 && len(summary.Rejected) == 0 {
		return
	}
	logger.LogWarn(c.Request.Context(), common.LogText(
		"capability gate: mode=%s evaluated=%d would_reject=%v rejected=%v channel_model=%s channels_evaluated=%d channels_rejected=%v",
		summary.Mode, summary.Evaluated, summary.WouldReject, summary.Rejected, summary.ChannelModel,
		summary.ChannelsEvaluated, summary.ChannelsRejected))
}

func capabilityGateFormat(path string) (capability.Format, bool) {
	switch {
	case strings.HasPrefix(path, "/v1/messages"):
		return capability.FormatAnthropic, true
	case strings.HasPrefix(path, "/v1/responses"):
		return capability.FormatOpenAIResponses, true
	case strings.HasPrefix(path, "/v1beta/models/"), strings.HasPrefix(path, "/v1/models/"):
		return capability.FormatGemini, true
	case strings.HasPrefix(path, "/v1/chat/completions"),
		strings.HasPrefix(path, "/pg/chat/completions"),
		strings.HasPrefix(path, "/v1/completions"):
		return capability.FormatOpenAIChat, true
	default:
		return "", false
	}
}

func capabilityGateBody(c *gin.Context, maxBytes int64) ([]byte, bool) {
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return nil, false
	}
	body, err := storage.Bytes()
	if err != nil || len(body) == 0 {
		return nil, false
	}
	if maxBytes > 0 && int64(len(body)) > maxBytes {
		logger.LogWarn(c.Request.Context(), common.LogText(
			"capability gate skipped: body %d bytes exceeds AUTO_CAPABILITY_GATE_MAX_BODY_BYTES %d",
			len(body), maxBytes))
		return nil, false
	}
	return body, true
}

func capabilityGateMaxBodyBytes() int64 {
	value := strings.TrimSpace(os.Getenv("AUTO_CAPABILITY_GATE_MAX_BODY_BYTES"))
	if value == "" {
		return 8 << 20
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 {
		return 8 << 20
	}
	return parsed
}

func dedupeSorted(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
