package service

import (
	"fmt"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/capability"
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

func TestCapabilityGateRejectsUndeclaredModelChannels(t *testing.T) {
	setupCapabilityGateTest(t)
	needs := &capability.Needs{ToolsCount: 1}
	enforce := &CapabilityGate{mode: CapabilityGateEnforce, needs: needs, modelVerdicts: map[string]capabilityVerdict{}}
	assert.Empty(t, enforce.FilterChannels(&gin.Context{}, "unknown-model", []int{1, 2}))
}
