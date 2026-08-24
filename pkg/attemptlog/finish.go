package attemptlog

import (
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// FinishInput carries the attempt's terminal state.
type FinishInput struct {
	// Err is the gateway's error for this attempt, nil on success.
	Err error
	// InternalErrCode is the gateway's own error code, e.g. "empty_response".
	InternalErrCode string
	// ErrMessage is the unmasked error text, used only to derive a cluster hash
	// and to probe for cause substrings. It is never persisted verbatim.
	ErrMessage string
	// HTTPStatus overrides the status seen by NoteUpstream when non-zero, for
	// paths that resolve a status without going through the shared request layer.
	HTTPStatus int
	// StreamEndReason is StreamStatus.EndReason, empty for non-stream attempts.
	StreamEndReason string
	// FirstTokenTime is when the first content chunk was produced, or nil when
	// none ever was. Callers must resolve the relay layer's sentinel value
	// before passing it here.
	FirstTokenTime *time.Time
	// StreamChunks is the number of upstream chunks received.
	StreamChunks int
	// ClientGone reports whether the downstream request context was cancelled.
	ClientGone bool
}

// Finish closes the attempt, classifies its outcome, and enqueues the record.
// It is safe to call on a nil Attempt, and it detaches the in-flight counter
// exactly once even if called more than once.
func (a *Attempt) Finish(c *gin.Context, in FinishInput) {
	if a == nil {
		return
	}

	a.mu.Lock()
	if a.finished {
		a.mu.Unlock()
		return
	}
	a.finished = true

	endTime := time.Now()
	removeInflight(a.target.ChannelId, a.features.InputTokensEst)

	httpStatus := a.httpStatus
	if in.HTTPStatus != 0 {
		httpStatus = in.HTTPStatus
	}

	sentFirstToken := in.FirstTokenTime != nil
	classifyIn := ClassifyInput{
		Err:             in.Err,
		InternalErrCode: in.InternalErrCode,
		HTTPStatus:      httpStatus,
		ErrMessage:      in.ErrMessage,
		StreamEndReason: in.StreamEndReason,
		SentFirstToken:  sentFirstToken,
		ClientGone:      in.ClientGone,
		IsStream:        a.features.IsStream,
	}

	outcome := Classify(classifyIn)
	record := a.buildRecord(c, in, classifyIn, outcome, httpStatus, endTime)
	a.mu.Unlock()

	if c != nil {
		common.SetContextKey(c, constant.ContextKeyRelayAttempt, nil)
	}
	enqueue(record)
}

// buildRecord assembles the row. Caller must hold a.mu.
func (a *Attempt) buildRecord(
	c *gin.Context,
	in FinishInput,
	classifyIn ClassifyInput,
	outcome string,
	httpStatus int,
	endTime time.Time,
) *model.RelayAttempt {
	totalMs := endTime.Sub(a.startTime).Milliseconds()

	record := &model.RelayAttempt{
		CreatedAt: endTime.Unix(),

		AttemptId:    a.attemptId,
		RequestId:    a.features.RequestId,
		AttemptIndex: a.attemptIndex,

		ChannelId:         a.target.ChannelId,
		ChannelType:       a.target.ChannelType,
		ModelName:         a.features.ModelName,
		UpstreamModelName: a.target.UpstreamModelName,
		UsingGroup:        a.target.UsingGroup,

		InputTokensEst: a.features.InputTokensEst,
		CharsLatin:     a.features.Chars.Latin,
		CharsHan:       a.features.Chars.Han,
		CharsOther:     a.features.Chars.Other,
		MaxTokensReq:   a.features.MaxTokensReq,
		IsStream:       a.features.IsStream,
		HasTools:       a.features.HasTools,
		ToolsCount:     a.features.ToolsCount,
		Temperature:    a.features.Temperature,
		TenantId:       a.features.TenantId,
		TokenId:        a.features.TokenId,
		RelayFormat:    a.features.RelayFormat,
		RequestPath:    a.features.RequestPath,

		InflightRequests:  a.inflightRequests,
		InflightTokensEst: a.inflightTokensEst,

		TsStart: a.startTime.UnixMilli(),
		TsEnd:   endTime.UnixMilli(),
		TotalMs: totalMs,

		Ok:              outcome == OutcomeOK,
		OutcomeCode:     outcome,
		UpstreamErrHash: ErrorHash(in.ErrMessage),
		TerminatedBy:    TerminatedBy(classifyIn, outcome),
		InternalErrCode: in.InternalErrCode,
		StreamEndReason: in.StreamEndReason,
		RetryAfterHint:  a.retryAfterHint,
	}

	if httpStatus != 0 {
		record.HttpStatus = &httpStatus
	}

	if in.FirstTokenTime != nil {
		tsFirst := in.FirstTokenTime.UnixMilli()
		record.TsFirstToken = &tsFirst
		if ttft := in.FirstTokenTime.Sub(a.startTime).Milliseconds(); ttft >= 0 {
			record.TtftMs = &ttft
		}
	}

	if a.upstreamKnown {
		upstreamMs := endTime.Sub(a.upstreamStart).Milliseconds()
		record.UpstreamMs = &upstreamMs
		overhead := totalMs - upstreamMs
		if overhead < 0 {
			overhead = 0
		}
		record.GatewayOverheadMs = &overhead
	}

	if in.StreamChunks > 0 {
		chunks := in.StreamChunks
		record.StreamChunks = &chunks
	}

	a.applyPricing(record)
	a.applyUsage(record, in, endTime)

	if c != nil {
		if reason := common.GetContextKeyString(c, constant.ContextKeyFinishReason); reason != "" {
			record.FinishReason = reason
		}
	}

	return record
}

// applyPricing records the ratios this gateway actually bills on. price_in and
// price_out stay nil deliberately: billing here is a per-model-name ratio, and a
// per-token unit price cannot be derived from it without inventing a base rate.
func (a *Attempt) applyPricing(record *model.RelayAttempt) {
	if a.pricing == nil {
		return
	}
	p := a.pricing
	if p.UsePrice {
		record.ModelPrice = &p.ModelPrice
	} else {
		record.ModelRatio = &p.ModelRatio
		record.CompletionRatio = &p.CompletionRatio
	}
	record.GroupRatio = &p.GroupRatio
	if p.CacheRatio > 0 {
		record.CacheRatio = &p.CacheRatio
	}
}

// applyUsage records settled usage. Nothing is written when billing never
// reported usage for this attempt, which is the normal case for a failed
// attempt: nil there means "never settled", not "zero tokens".
func (a *Attempt) applyUsage(record *model.RelayAttempt, in FinishInput, endTime time.Time) {
	if !a.usageKnown {
		return
	}

	record.InputTokensActual = &a.inputTokens
	record.OutputTokensActual = &a.outputTokens
	record.CachedTokens = &a.cachedTokens
	record.CostActual = &a.costActual
	if a.reasoningTokens > 0 {
		record.ReasoningTokens = &a.reasoningTokens
	}

	// Output rate is measured over the generation window, i.e. after the first
	// token. Using total elapsed time instead would fold queueing and TTFT into
	// the rate and understate a fast-but-slow-to-start channel.
	if in.FirstTokenTime == nil || a.outputTokens <= 0 {
		return
	}
	generationMs := endTime.Sub(*in.FirstTokenTime).Milliseconds()
	if generationMs <= 0 {
		return
	}
	tps := float64(a.outputTokens) / (float64(generationMs) / 1000)
	record.TpsActual = &tps
}
