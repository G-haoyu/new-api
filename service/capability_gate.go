package service

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/capability"
	"github.com/QuantumNous/new-api/setting/operation_setting"
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

// CurrentCapabilityGateMode 读取系统设置里的门控模式（默认 shadow）。
// 模式只存在数据库里，没有环境变量开关。
func CurrentCapabilityGateMode() CapabilityGateMode {
	switch operation_setting.GetCapabilityGateSetting().Mode {
	case operation_setting.CapabilityGateModeOff:
		return CapabilityGateOff
	case operation_setting.CapabilityGateModeEnforce:
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

	modelVerdicts   map[string]capabilityVerdict
	modelRejections map[string]capabilityRejection
	evaluated       int
	wouldReject     []string
	rejected        []string

	channelModel      string
	channelsEvaluated int
	channelsRejected  []int
	channelRejections map[int]capabilityRejection
}

type capabilityRejection struct {
	undeclared bool
	violations []capability.Violation
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
		if g.modelVerdicts == nil {
			g.modelVerdicts = make(map[string]capabilityVerdict)
		}
		if g.modelRejections == nil {
			g.modelRejections = make(map[string]capabilityRejection)
		}
		spec, declared := model.ResolveModelCapability(baseModel)
		var violations []capability.Violation
		if declared {
			violations = normalizeGateViolations(capability.Evaluate(spec, g.needs))
		}
		satisfied := declared && capability.Satisfied(violations)
		verdict = capabilityVerdict{declared: declared, satisfied: satisfied}
		g.modelVerdicts[baseModel] = verdict
		g.evaluated++
		if !satisfied {
			g.modelRejections[baseModel] = capabilityRejection{undeclared: !declared, violations: violations}
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
// 只有 soft 违规（采样参数越界）的渠道仍然保留，但会写进门控事件供排查。
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
	rejections := make(map[int]capabilityRejection, len(channelIDs))
	soft := make(map[string]capability.Violation)
	for _, channelID := range channelIDs {
		if !declared {
			rejected = append(rejected, channelID)
			rejections[channelID] = capabilityRejection{undeclared: true}
			continue
		}
		effective, ok := model.EffectiveChannelCapability(channelID, baseModel)
		if !ok {
			rejected = append(rejected, channelID)
			rejections[channelID] = capabilityRejection{undeclared: true}
			continue
		}
		violations := normalizeGateViolations(capability.Evaluate(effective, g.needs))
		if !capability.Satisfied(violations) {
			rejected = append(rejected, channelID)
			rejections[channelID] = capabilityRejection{violations: violations}
			continue
		}
		for _, violation := range violations {
			soft[violation.Dimension] = violation
		}
		kept = append(kept, channelID)
	}
	g.channelModel = baseModel
	g.channelsEvaluated = len(channelIDs)
	g.channelsRejected = rejected
	g.channelRejections = rejections
	recordCapabilityGateEvent(c, g, baseModel, len(channelIDs), rejected, rejections, soft)
	if g.mode != CapabilityGateEnforce {
		return channelIDs
	}
	return kept
}

// capabilityGateEventAction 把一次判定归类：
// 只有"剩余可用渠道为 0"才是真正的拒绝；淘汰了部分渠道但仍有可用渠道时，
// 请求是被过滤后成功的，不能标成 rejected。
func capabilityGateEventAction(mode CapabilityGateMode, evaluated, rejected, soft int) string {
	if rejected == 0 {
		if soft > 0 {
			return model.CapabilityGateEventSoft
		}
		return model.CapabilityGateEventPassed
	}
	kept := evaluated - rejected
	if mode == CapabilityGateEnforce {
		if kept == 0 {
			return model.CapabilityGateEventRejected
		}
		return model.CapabilityGateEventFiltered
	}
	if kept == 0 {
		return model.CapabilityGateEventWouldReject
	}
	return model.CapabilityGateEventWouldFilter
}

// normalizeGateViolations 按系统设置里的"仅记录维度"重算严重度。
// 命中清单的维度（可以是组，也可以是单个维度）一律 soft，其余 hard。
func normalizeGateViolations(violations []capability.Violation) []capability.Violation {
	if len(violations) == 0 {
		return nil
	}
	recordOnly := operation_setting.CapabilityGateRecordOnlyDimensions()
	out := make([]capability.Violation, 0, len(violations))
	for _, violation := range violations {
		normalized := violation
		normalized.Severity = capability.SeverityHard
		for _, prefix := range recordOnly {
			if violation.Dimension == prefix || strings.HasPrefix(violation.Dimension, prefix+".") {
				normalized.Severity = capability.SeveritySoft
				break
			}
		}
		out = append(out, normalized)
	}
	return out
}

// recordCapabilityGateEvent 把一次渠道级判定交给异步 writer 落库。
// 设置里允许记录通过时，每个请求都会留痕（含多渠道命中里部分渠道不满足的情况）；
// 这里只做内存里的组装和非阻塞投递，落库失败或队列满都不影响本次请求的选路。
func recordCapabilityGateEvent(c *gin.Context, g *CapabilityGate, modelName string, evaluated int, rejected []int, rejections map[int]capabilityRejection, soft map[string]capability.Violation) {
	writer := capabilityGateEvents.Load()
	if writer == nil {
		return
	}
	action := capabilityGateEventAction(g.mode, evaluated, len(rejected), len(soft))
	if action == model.CapabilityGateEventPassed && !operation_setting.GetCapabilityGateSetting().RecordPassed {
		return
	}

	dimensions := make(map[string]capability.Violation, len(soft)+len(rejections))
	for dimension, violation := range soft {
		dimensions[dimension] = violation
	}
	undeclared := false
	for _, rejection := range rejections {
		if rejection.undeclared {
			undeclared = true
			continue
		}
		for _, violation := range rejection.violations {
			if _, exists := dimensions[violation.Dimension]; !exists {
				dimensions[violation.Dimension] = violation
			}
		}
	}
	if undeclared {
		dimensions["model.declaration"] = capability.Violation{
			Dimension: "model.declaration",
			Required:  true,
			Supported: false,
			Severity:  capability.SeverityHard,
		}
	}
	names := make([]string, 0, len(dimensions))
	for dimension := range dimensions {
		names = append(names, dimension)
	}
	sort.Strings(names)
	list := make([]capability.Violation, 0, len(names))
	for _, name := range names {
		list = append(list, dimensions[name])
	}
	encoded, err := common.Marshal(list)
	if err != nil {
		common.SysError("failed to encode capability gate event: " + err.Error())
		return
	}
	channelText := ""
	if len(rejected) > 0 {
		ids := make([]string, 0, len(rejected))
		for _, channelID := range rejected {
			ids = append(ids, strconv.Itoa(channelID))
		}
		channelText = strings.Join(ids, ",")
		// 列宽有限，超出时在逗号处截断；准确数量由 RejectedCount 记录。
		if len(channelText) > model.CapabilityGateEventRejectedChannelsMax {
			channelText = channelText[:strings.LastIndex(channelText[:model.CapabilityGateEventRejectedChannelsMax+1], ",")]
		}
	}
	writer.enqueue(&model.CapabilityGateEvent{
		Mode:              string(g.mode),
		Model:             modelName,
		Action:            action,
		ChannelsEvaluated: evaluated,
		RejectedChannels:  channelText,
		Dimensions:        string(encoded),
		CreatedAt:         common.GetTimestamp(),
		RejectedCount:     len(rejected),
		RequestId:         c.GetString(common.RequestIdKey),
		UserId:            common.GetContextKeyInt(c, constant.ContextKeyUserId),
		TokenId:           common.GetContextKeyInt(c, constant.ContextKeyTokenId),
		UsingGroup:        common.GetContextKeyString(c, constant.ContextKeyUsingGroup),
	})
}

// ExplainChannelRejection 把本次请求的渠道级淘汰原因整理成面向调用方的错误。
// 只描述 hard 违规维度；未声明模型会明确提示缺少模型级声明。
func (g *CapabilityGate) ExplainChannelRejection(modelName string) string {
	if g == nil || len(g.channelRejections) == 0 {
		return fmt.Sprintf("no available channel for model %q satisfies the request capabilities", modelName)
	}
	undeclared := 0
	dimensions := make(map[string]string)
	for _, rejection := range g.channelRejections {
		if rejection.undeclared {
			undeclared++
			continue
		}
		for _, violation := range rejection.violations {
			if violation.Severity != capability.SeverityHard {
				continue
			}
			if _, exists := dimensions[violation.Dimension]; exists {
				continue
			}
			dimensions[violation.Dimension] = describeCapabilityViolation(violation)
		}
	}
	if len(dimensions) == 0 {
		if undeclared > 0 {
			return fmt.Sprintf("model %q has no model-level capability declaration, so no available channel can be validated", modelName)
		}
		return fmt.Sprintf("no available channel for model %q satisfies the request capabilities", modelName)
	}
	names := make([]string, 0, len(dimensions))
	for dimension := range dimensions {
		names = append(names, dimension)
	}
	sort.Strings(names)
	if len(names) > 8 {
		names = append(names[:8], fmt.Sprintf("and %d more", len(names)-8))
	}
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, dimensions[name])
	}
	return fmt.Sprintf("no available channel for model %q satisfies the request: %s", modelName, strings.Join(parts, "; "))
}

func describeCapabilityViolation(violation capability.Violation) string {
	if violation.Required == nil {
		return violation.Dimension
	}
	if violation.Supported == nil {
		return fmt.Sprintf("%s (requested %s)", violation.Dimension, formatCapabilityValue(violation.Required))
	}
	return fmt.Sprintf("%s (requested %s, supported %s)",
		violation.Dimension, formatCapabilityValue(violation.Required), formatCapabilityValue(violation.Supported))
}

func formatCapabilityValue(value any) string {
	switch typed := value.(type) {
	case []string:
		if len(typed) == 0 {
			return "not declared"
		}
		return strings.Join(typed, ", ")
	case *capability.IntRange:
		if typed == nil {
			return "not declared"
		}
		return fmt.Sprintf("[%d,%d]", typed.Min, typed.Max)
	case *capability.FloatRange:
		if typed == nil {
			return "not declared"
		}
		return fmt.Sprintf("[%g,%g]", typed.Min, typed.Max)
	case *capability.Limit:
		if typed == nil {
			return "not declared"
		}
		return fmt.Sprintf("max %d", typed.Max)
	default:
		return fmt.Sprint(value)
	}
}

// AllEvaluatedRejected 报告 auto 候选里所有已评估模型都被模型级能力淘汰。
func (g *CapabilityGate) AllEvaluatedRejected() bool {
	return g != nil && g.mode == CapabilityGateEnforce && g.evaluated > 0 && len(g.wouldReject) >= g.evaluated
}

// ExplainModelRejection 把 auto 模型级淘汰原因整理成面向调用方的错误。
func (g *CapabilityGate) ExplainModelRejection() string {
	undeclared := 0
	dimensions := make(map[string]string)
	for _, rejection := range g.modelRejections {
		if rejection.undeclared {
			undeclared++
			continue
		}
		for _, violation := range rejection.violations {
			if violation.Severity != capability.SeverityHard {
				continue
			}
			if _, exists := dimensions[violation.Dimension]; exists {
				continue
			}
			dimensions[violation.Dimension] = describeCapabilityViolation(violation)
		}
	}
	if len(dimensions) == 0 {
		if undeclared > 0 {
			return "no auto candidate model has a model-level capability declaration, so none can be validated"
		}
		return "no auto candidate model satisfies the request capabilities"
	}
	names := make([]string, 0, len(dimensions))
	for dimension := range dimensions {
		names = append(names, dimension)
	}
	sort.Strings(names)
	if len(names) > 8 {
		names = append(names[:8], fmt.Sprintf("and %d more", len(names)-8))
	}
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, dimensions[name])
	}
	return fmt.Sprintf("no auto candidate model satisfies the request: %s", strings.Join(parts, "; "))
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
			"capability gate skipped: body %d bytes exceeds capability_gate_setting.max_body_bytes %d",
			len(body), maxBytes))
		return nil, false
	}
	return body, true
}

func capabilityGateMaxBodyBytes() int64 {
	return operation_setting.GetCapabilityGateSetting().MaxBodyBytes
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
