package service

import (
	"fmt"
	"strings"
	"testing"

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
	require.NoError(t, db.AutoMigrate(&model.Model{}, &model.Channel{}, &model.Ability{}, &model.ChannelModelCapability{}, &model.CapabilityGateEvent{}))
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

func TestCapabilityGateModelLevelShadowAndEnforce(t *testing.T) {
	setupCapabilityGateTest(t)
	needs := &capability.Needs{ToolsCount: 1}

	shadow := &CapabilityGate{mode: CapabilityGateShadow, needs: needs, modelVerdicts: map[string]capabilityVerdict{}}
	assert.True(t, shadow.Allows("gated-model"))

	enforce := &CapabilityGate{mode: CapabilityGateEnforce, needs: needs, modelVerdicts: map[string]capabilityVerdict{}}
	assert.True(t, enforce.Allows("gated-model"))
	assert.False(t, enforce.Allows("unregistered-model"))
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
	assert.False(t, enforce.Allows("gated-model"))
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
