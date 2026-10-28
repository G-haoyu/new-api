package service

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/capability"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupCapabilityGateTest(t *testing.T) {
	t.Helper()
	previousDB := model.DB
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Model{}, &model.Channel{}, &model.Ability{}, &model.ChannelModelCapability{}))
	model.DB = db
	require.NoError(t, db.Create(&model.Model{
		ModelName:    "gated-model",
		NameRule:     model.NameRuleExact,
		Capabilities: `{"tools":{"supported":true},"streaming":{"supported":true}}`,
	}).Error)
	require.NoError(t, db.Create(&model.Channel{
		Id:     1,
		Name:   "gated-channel",
		Group:  "default",
		Models: "gated-model",
		Status: 1,
	}).Error)
	require.NoError(t, model.InitCapabilityCache())
	require.NoError(t, model.SetChannelModelCapability(1, "gated-model", `{"tools":{"supported":false}}`))
	t.Cleanup(func() {
		model.DB = previousDB
		model.InvalidateCapabilityCache()
		sqlDB, dbErr := db.DB()
		if dbErr == nil {
			require.NoError(t, sqlDB.Close())
		}
	})
}

// 渠道 1 把 tools 收窄成不支持，渠道 2 没有收窄记录、沿用模型级声明（支持 tools）。
func TestCapabilityGateCandidateNeedsOneSatisfyingChannel(t *testing.T) {
	setupCapabilityGateTest(t)
	needs := &capability.Needs{ToolsCount: 1}
	newGate := func(mode CapabilityGateMode) *CapabilityGate {
		return &CapabilityGate{mode: mode, needs: needs, modelVerdicts: map[string]capabilityVerdict{}}
	}

	assert.True(t, newGate(CapabilityGateEnforce).AllowsAnyChannel("gated-model", []int{1, 2}), "渠道 2 满足就够了")
	assert.False(t, newGate(CapabilityGateEnforce).AllowsAnyChannel("gated-model", []int{1}), "唯一的渠道被收窄淘汰，模型不可选")
	assert.False(t, newGate(CapabilityGateEnforce).AllowsAnyChannel("unregistered-model", []int{1, 2}))

	shadow := newGate(CapabilityGateShadow)
	assert.True(t, shadow.AllowsAnyChannel("gated-model", []int{1}), "shadow 只记录不拦截")
	assert.Equal(t, []string{"gated-model"}, shadow.Summary().WouldReject)
}

func TestCapabilityGateChannelFilter(t *testing.T) {
	setupCapabilityGateTest(t)
	needs := &capability.Needs{ToolsCount: 1}
	channelIDs := []int{1, 2}

	shadow := &CapabilityGate{mode: CapabilityGateShadow, needs: needs, modelVerdicts: map[string]capabilityVerdict{}}
	assert.Equal(t, channelIDs, shadow.FilterChannels(&gin.Context{}, "gated-model", channelIDs))
	assert.Equal(t, []int{1}, shadow.channelsRejected)

	enforce := &CapabilityGate{mode: CapabilityGateEnforce, needs: needs, modelVerdicts: map[string]capabilityVerdict{}}
	assert.Equal(t, []int{2}, enforce.FilterChannels(&gin.Context{}, "gated-model", channelIDs))
}

func TestCapabilityGateExplainsModelRejection(t *testing.T) {
	setupCapabilityGateTest(t)
	needs := &capability.Needs{BuiltinTools: []string{"web_search"}}
	enforce := &CapabilityGate{mode: CapabilityGateEnforce, needs: needs, modelVerdicts: map[string]capabilityVerdict{}}
	assert.False(t, enforce.AllowsAnyChannel("gated-model", []int{1, 2}))
	assert.True(t, enforce.AllEvaluatedRejected())
	message := enforce.ExplainModelRejection()
	assert.Contains(t, message, "tools.builtin")
	assert.Contains(t, message, "web_search")
}

func TestCapabilityGateExplainsChannelRejection(t *testing.T) {
	setupCapabilityGateTest(t)
	needs := &capability.Needs{ToolsCount: 1}
	enforce := &CapabilityGate{mode: CapabilityGateEnforce, needs: needs, modelVerdicts: map[string]capabilityVerdict{}}
	assert.Empty(t, enforce.FilterChannels(&gin.Context{}, "gated-model", []int{1}))
	message := enforce.ExplainChannelRejection("gated-model")
	assert.Contains(t, message, "tools.supported")
	assert.Contains(t, message, "requested 1")
}

func TestCapabilityGateExplainsMissingDeclaration(t *testing.T) {
	setupCapabilityGateTest(t)
	needs := &capability.Needs{ToolsCount: 1}
	enforce := &CapabilityGate{mode: CapabilityGateEnforce, needs: needs, modelVerdicts: map[string]capabilityVerdict{}}
	assert.Empty(t, enforce.FilterChannels(&gin.Context{}, "unknown-model", []int{1}))
	message := enforce.ExplainChannelRejection("unknown-model")
	assert.Contains(t, message, "no model-level capability declaration")
}

func TestCapabilityGateRejectsUndeclaredModelChannels(t *testing.T) {
	setupCapabilityGateTest(t)
	needs := &capability.Needs{ToolsCount: 1}
	enforce := &CapabilityGate{mode: CapabilityGateEnforce, needs: needs, modelVerdicts: map[string]capabilityVerdict{}}
	assert.Empty(t, enforce.FilterChannels(&gin.Context{}, "unknown-model", []int{1, 2}))
}

func TestNormalizeGateViolationsUsesRecordOnlySettings(t *testing.T) {
	setting := operation_setting.GetCapabilityGateSetting()
	previous := setting.RecordOnlyDimensions
	t.Cleanup(func() { setting.RecordOnlyDimensions = previous })

	setting.RecordOnlyDimensions = "params,tools.parallel"
	normalized := normalizeGateViolations([]capability.Violation{
		{Dimension: "tools.supported", Severity: capability.SeverityHard},
		{Dimension: "tools.parallel", Severity: capability.SeveritySoft},
		{Dimension: "params.top_p", Severity: capability.SeveritySoft},
	})
	require.Len(t, normalized, 3)
	assert.Equal(t, capability.SeverityHard, normalized[0].Severity)
	assert.Equal(t, capability.SeveritySoft, normalized[1].Severity)
	assert.Equal(t, capability.SeveritySoft, normalized[2].Severity)

	// 把整个 tools 组切成仅记录后，工具类越界不再拦截请求。
	setting.RecordOnlyDimensions = "params,tools"
	relaxed := normalizeGateViolations([]capability.Violation{{Dimension: "tools.supported", Severity: capability.SeverityHard}})
	require.Len(t, relaxed, 1)
	assert.Equal(t, capability.SeveritySoft, relaxed[0].Severity)
	assert.True(t, capability.Satisfied(relaxed))

	// 反过来，清空清单会让采样参数也变成拦截项。
	setting.RecordOnlyDimensions = ""
	strict := normalizeGateViolations([]capability.Violation{{Dimension: "params.top_p", Severity: capability.SeveritySoft}})
	require.Len(t, strict, 1)
	assert.Equal(t, capability.SeverityHard, strict[0].Severity)
	assert.False(t, capability.Satisfied(strict))
}

func TestCapabilityGateEventActionDistinguishesFilteredFromRejected(t *testing.T) {
	assert.Equal(t, model.CapabilityGateEventPassed, capabilityGateEventAction(CapabilityGateEnforce, 2, 0, 0))
	assert.Equal(t, model.CapabilityGateEventSoft, capabilityGateEventAction(CapabilityGateShadow, 2, 0, 1))

	// enforce：还有渠道活着就是 filtered，全部淘汰才是 rejected。
	assert.Equal(t, model.CapabilityGateEventFiltered, capabilityGateEventAction(CapabilityGateEnforce, 2, 1, 0))
	assert.Equal(t, model.CapabilityGateEventRejected, capabilityGateEventAction(CapabilityGateEnforce, 2, 2, 0))

	// shadow：只是预演，不会真的拦请求。
	assert.Equal(t, model.CapabilityGateEventWouldFilter, capabilityGateEventAction(CapabilityGateShadow, 2, 1, 0))
	assert.Equal(t, model.CapabilityGateEventWouldReject, capabilityGateEventAction(CapabilityGateShadow, 2, 2, 0))
}

func receiveWithin[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(5 * time.Second):
		require.FailNow(t, "timed out waiting for the capability gate event writer")
		var zero T
		return zero
	}
}

// newRecordingCapabilityGateEventWriter 返回一个把每个交付批次送进 channel 的 writer。
// 调用方负责 shutdown。
func newRecordingCapabilityGateEventWriter(queueSize, batchSize int, flushInterval time.Duration) (*capabilityGateEventWriter, <-chan []model.CapabilityGateEvent) {
	batches := make(chan []model.CapabilityGateEvent, 16)
	writer := newCapabilityGateEventWriter(queueSize, batchSize, flushInterval, func(batch []*model.CapabilityGateEvent) error {
		delivered := make([]model.CapabilityGateEvent, 0, len(batch))
		for _, event := range batch {
			delivered = append(delivered, *event)
		}
		batches <- delivered
		return nil
	})
	return writer, batches
}

// captureCapabilityGateEvents 把进程级 writer 换成录制用的，批大小为 1，每个事件单独交付。
func captureCapabilityGateEvents(t *testing.T) <-chan []model.CapabilityGateEvent {
	t.Helper()
	writer, batches := newRecordingCapabilityGateEventWriter(16, 1, time.Hour)
	capabilityGateEvents.Store(writer)
	t.Cleanup(func() {
		capabilityGateEvents.Store(nil)
		writer.shutdown(time.Second)
	})
	return batches
}

func TestCapabilityGateEventWriterFlushesOnBatchSizeAndInterval(t *testing.T) {
	cases := []struct {
		name          string
		batchSize     int
		flushInterval time.Duration
		enqueued      []string
	}{
		{name: "batch size reached", batchSize: 3, flushInterval: time.Hour, enqueued: []string{"a", "b", "c"}},
		{name: "flush interval elapsed", batchSize: 100, flushInterval: 20 * time.Millisecond, enqueued: []string{"a"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			writer, batches := newRecordingCapabilityGateEventWriter(16, tc.batchSize, tc.flushInterval)
			t.Cleanup(func() { writer.shutdown(time.Second) })
			want := make([]model.CapabilityGateEvent, 0, len(tc.enqueued))
			for _, name := range tc.enqueued {
				writer.enqueue(&model.CapabilityGateEvent{Model: name})
				want = append(want, model.CapabilityGateEvent{Model: name})
			}
			assert.Equal(t, want, receiveWithin(t, batches))
		})
	}
}

func TestCapabilityGateEventWriterShutdownDrainsQueuedEvents(t *testing.T) {
	writer, batches := newRecordingCapabilityGateEventWriter(16, 100, time.Hour)
	writer.enqueue(&model.CapabilityGateEvent{Model: "a"})
	writer.enqueue(&model.CapabilityGateEvent{Model: "b"})

	writer.shutdown(5 * time.Second)

	assert.Equal(t, []model.CapabilityGateEvent{{Model: "a"}, {Model: "b"}}, receiveWithin(t, batches))
}

func TestCapabilityGateEventWriterDropsInsteadOfBlockingWhenQueueIsFull(t *testing.T) {
	started := make(chan struct{}, 4)
	release := make(chan struct{})
	var inserted atomic.Int64
	writer := newCapabilityGateEventWriter(1, 1, time.Hour, func(batch []*model.CapabilityGateEvent) error {
		started <- struct{}{}
		<-release
		inserted.Add(int64(len(batch)))
		return nil
	})

	writer.enqueue(&model.CapabilityGateEvent{Model: "a"})
	// 消费者取走 a 并卡在写入里，容量为 1 的队列此刻是空的。
	receiveWithin(t, started)
	writer.enqueue(&model.CapabilityGateEvent{Model: "b"})
	writer.enqueue(&model.CapabilityGateEvent{Model: "c"})
	assert.Equal(t, int64(1), writer.dropped.Load())

	close(release)
	writer.shutdown(5 * time.Second)
	assert.Equal(t, int64(2), inserted.Load())
}

func TestCapabilityGateEventWriterKeepsRunningAfterFailedInserts(t *testing.T) {
	var calls atomic.Int32
	delivered := make(chan string, 1)
	writer := newCapabilityGateEventWriter(8, 1, time.Hour, func(batch []*model.CapabilityGateEvent) error {
		switch calls.Add(1) {
		case 1:
			panic("insert panicked")
		case 2:
			return errors.New("database is down")
		}
		delivered <- batch[0].Model
		return nil
	})
	t.Cleanup(func() { writer.shutdown(time.Second) })

	for _, name := range []string{"panics", "errors", "succeeds"} {
		writer.enqueue(&model.CapabilityGateEvent{Model: name})
	}

	assert.Equal(t, "succeeds", receiveWithin(t, delivered))
}

func TestCapabilityGateEventWriterReadsTuningFromEnvironment(t *testing.T) {
	const (
		bufferKey = "CAPABILITY_GATE_EVENT_BUFFER_SIZE"
		batchKey  = "CAPABILITY_GATE_EVENT_BATCH_SIZE"
		flushKey  = "CAPABILITY_GATE_EVENT_FLUSH_MS"
	)
	cases := []struct {
		name                  string
		buffer, batch, flush  string
		wantBuffer, wantBatch int
		wantFlushInterval     time.Duration
	}{
		{name: "defaults", wantBuffer: 4096, wantBatch: 200, wantFlushInterval: 2 * time.Second},
		{name: "overrides", buffer: "64", batch: "8", flush: "500", wantBuffer: 64, wantBatch: 8, wantFlushInterval: 500 * time.Millisecond},
		{name: "raised to the minimum", buffer: "0", batch: "-3", flush: "1", wantBuffer: 1, wantBatch: 1, wantFlushInterval: 100 * time.Millisecond},
		{name: "cut to the maximum", buffer: "99999999", batch: "99999999", flush: "99999999", wantBuffer: 1000000, wantBatch: 10000, wantFlushInterval: time.Hour},
		{name: "batch never exceeds the queue", buffer: "16", batch: "500", wantBuffer: 16, wantBatch: 16, wantFlushInterval: 2 * time.Second},
		{name: "unparsable values fall back to the defaults", buffer: "abc", batch: "1.5", flush: "soon", wantBuffer: 4096, wantBatch: 200, wantFlushInterval: 2 * time.Second},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// 空串等同于未设置。
			t.Setenv(bufferKey, tc.buffer)
			t.Setenv(batchKey, tc.batch)
			t.Setenv(flushKey, tc.flush)

			StartCapabilityGateEventWriter()
			t.Cleanup(StopCapabilityGateEventWriter)

			writer := capabilityGateEvents.Load()
			require.NotNil(t, writer)
			assert.Equal(t, tc.wantBuffer, cap(writer.queue))
			assert.Equal(t, tc.wantBatch, writer.batchSize)
			assert.Equal(t, tc.wantFlushInterval, writer.flushInterval)
		})
	}
}

func TestCapabilityGateEventWriterStartAndStopAreIdempotent(t *testing.T) {
	require.Nil(t, capabilityGateEvents.Load())

	StartCapabilityGateEventWriter()
	first := capabilityGateEvents.Load()
	require.NotNil(t, first)
	StartCapabilityGateEventWriter()
	assert.Same(t, first, capabilityGateEvents.Load())

	StopCapabilityGateEventWriter()
	assert.Nil(t, capabilityGateEvents.Load())
	assert.NotPanics(t, StopCapabilityGateEventWriter)
}

func TestCapabilityGateEventsCarryRequestIdentityAndSkipPassedByDefault(t *testing.T) {
	setupCapabilityGateTest(t)
	batches := captureCapabilityGateEvents(t)
	c := &gin.Context{}
	c.Set(common.RequestIdKey, "req-1")
	common.SetContextKey(c, constant.ContextKeyUserId, 7)
	common.SetContextKey(c, constant.ContextKeyTokenId, 9)
	common.SetContextKey(c, constant.ContextKeyUsingGroup, "vip")

	// 满足全部声明的请求默认不产生事件：紧接着的违规事件是第一条被交付的，说明 passed 没有入队。
	passing := &CapabilityGate{mode: CapabilityGateShadow, needs: &capability.Needs{}, modelVerdicts: map[string]capabilityVerdict{}}
	passing.FilterChannels(c, "gated-model", []int{1})
	violating := &CapabilityGate{mode: CapabilityGateShadow, needs: &capability.Needs{ToolsCount: 1}, modelVerdicts: map[string]capabilityVerdict{}}
	violating.FilterChannels(c, "gated-model", []int{1, 2})

	filtered := receiveWithin(t, batches)[0]
	assert.Equal(t, model.CapabilityGateEventWouldFilter, filtered.Action)
	assert.Equal(t, "shadow", filtered.Mode)
	assert.Equal(t, "gated-model", filtered.Model)
	assert.Equal(t, 2, filtered.ChannelsEvaluated)
	assert.Equal(t, "1", filtered.RejectedChannels)
	assert.Equal(t, 1, filtered.RejectedCount)
	assert.Contains(t, filtered.Dimensions, "tools.supported")
	assert.Equal(t, "req-1", filtered.RequestId)
	assert.Equal(t, 7, filtered.UserId)
	assert.Equal(t, 9, filtered.TokenId)
	assert.Equal(t, "vip", filtered.UsingGroup)
	assert.NotZero(t, filtered.CreatedAt)

	setting := operation_setting.GetCapabilityGateSetting()
	previous := setting.RecordPassed
	setting.RecordPassed = true
	t.Cleanup(func() { setting.RecordPassed = previous })
	passing = &CapabilityGate{mode: CapabilityGateShadow, needs: &capability.Needs{}, modelVerdicts: map[string]capabilityVerdict{}}
	passing.FilterChannels(c, "gated-model", []int{1})

	passed := receiveWithin(t, batches)[0]
	assert.Equal(t, model.CapabilityGateEventPassed, passed.Action)
	assert.Empty(t, passed.RejectedChannels)
	assert.Zero(t, passed.RejectedCount)
}

func TestCapabilityGateCandidateJudgementWritesNoEvent(t *testing.T) {
	setupCapabilityGateTest(t)
	batches := captureCapabilityGateEvents(t)
	gate := &CapabilityGate{mode: CapabilityGateEnforce, needs: &capability.Needs{ToolsCount: 1}, modelVerdicts: map[string]capabilityVerdict{}}

	// 候选阶段会判定很多模型，但都不留痕；事件只给最终选中的模型写一条。
	gate.AllowsAnyChannel("unregistered-model", []int{1})
	gate.AllowsAnyChannel("gated-model", []int{1})
	gate.AllowsAnyChannel("gated-model", []int{1, 2})
	assert.Equal(t, []int{2}, gate.FilterChannels(&gin.Context{}, "gated-model", []int{1, 2}))

	event := receiveWithin(t, batches)[0]
	assert.Equal(t, "gated-model", event.Model)
	assert.Equal(t, model.CapabilityGateEventFiltered, event.Action)
	assert.Equal(t, 2, event.ChannelsEvaluated)
	assert.Equal(t, 1, event.RejectedCount)
}

func TestCapabilityGateEventTruncatesRejectedChannelsOnAWholeChannelId(t *testing.T) {
	setupCapabilityGateTest(t)
	batches := captureCapabilityGateEvents(t)

	channelIDs := make([]int, 0, 200)
	idTexts := make([]string, 0, 200)
	for id := 1000; id < 1200; id++ {
		channelIDs = append(channelIDs, id)
		idTexts = append(idTexts, strconv.Itoa(id))
	}
	// 未声明的模型会淘汰所有渠道，拼出来的 id 列表远超列宽。
	gate := &CapabilityGate{mode: CapabilityGateShadow, needs: &capability.Needs{ToolsCount: 1}, modelVerdicts: map[string]capabilityVerdict{}}
	gate.FilterChannels(&gin.Context{}, "unknown-model", channelIDs)

	event := receiveWithin(t, batches)[0]
	assert.Equal(t, model.CapabilityGateEventWouldReject, event.Action)
	assert.Equal(t, 200, event.RejectedCount)
	assert.LessOrEqual(t, len(event.RejectedChannels), model.CapabilityGateEventRejectedChannelsMax)
	assert.True(t, strings.HasPrefix(strings.Join(idTexts, ","), event.RejectedChannels+","),
		"the truncated list must end on a whole channel id, got %q", event.RejectedChannels)
}
