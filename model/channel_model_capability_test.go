package model

import (
	"fmt"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupCapabilityModelTest(t *testing.T) *gorm.DB {
	t.Helper()
	previousDB := DB
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Model{}, &Channel{}, &Ability{}, &ChannelModelStatus{}, &ChannelModelCapability{}))
	DB = db
	t.Cleanup(func() {
		DB = previousDB
		InvalidateCapabilityCache()
		sqlDB, dbErr := db.DB()
		if dbErr == nil {
			require.NoError(t, sqlDB.Close())
		}
	})
	return db
}

func TestCapabilityTargetsListServingChannelsOnce(t *testing.T) {
	db := setupCapabilityModelTest(t)
	require.NoError(t, db.Create(&Model{ModelName: "target-model", NameRule: NameRuleExact, Capabilities: `{"context":{"max_output_tokens":100}}`}).Error)
	require.NoError(t, db.Create(&Channel{Id: 7, Name: "chan", Group: "default", Models: "target-model", Status: common.ChannelStatusEnabled}).Error)
	require.NoError(t, db.Create(&[]Ability{
		{Group: "default", Model: "target-model", ChannelId: 7, Enabled: true},
		{Group: "vip", Model: "target-model", ChannelId: 7, Enabled: true},
	}).Error)
	require.NoError(t, InitCapabilityCache())

	targets, err := GetCapabilityTargets()
	require.NoError(t, err)
	require.Len(t, targets, 1)
	assert.Equal(t, "target-model", targets[0].ModelName)
	require.Len(t, targets[0].Channels, 1)
	assert.Equal(t, 7, targets[0].Channels[0].ID)
	assert.Equal(t, "chan", targets[0].Channels[0].Name)
}

func TestCapabilitySchemaSQLiteIdempotent(t *testing.T) {
	db := setupCapabilityModelTest(t)
	// AutoMigrate 跑两次证明幂等；SQLite 下复合主键与 capabilities 列都必须存在。
	require.NoError(t, db.AutoMigrate(&Model{}, &ChannelModelCapability{}))
	require.NoError(t, db.AutoMigrate(&Model{}, &ChannelModelCapability{}))
	require.True(t, db.Migrator().HasTable(&ChannelModelCapability{}))
	require.True(t, db.Migrator().HasColumn(&Model{}, "capabilities"))
	require.True(t, db.Migrator().HasColumn(&ChannelModelCapability{}, "channel_id"))
	require.True(t, db.Migrator().HasColumn(&ChannelModelCapability{}, "model"))
	require.True(t, db.Migrator().HasColumn(&ChannelModelCapability{}, "capabilities"))

	row := ChannelModelCapability{ChannelId: 1, Model: "m", Capabilities: `{}`}
	require.NoError(t, db.Create(&row).Error)
	err := db.Create(&ChannelModelCapability{ChannelId: 1, Model: "m", Capabilities: `{}`}).Error
	require.Error(t, err)
}

func TestCapabilityResolutionFollowsNameRulePrecedence(t *testing.T) {
	db := setupCapabilityModelTest(t)
	require.NoError(t, db.Create(&Model{ModelName: "gpt-", NameRule: NameRulePrefix, Capabilities: `{"context":{"max_output_tokens":1000}}`}).Error)
	require.NoError(t, db.Create(&Model{ModelName: "gpt-4o-", NameRule: NameRulePrefix, Capabilities: `{"context":{"max_output_tokens":1500}}`}).Error)
	require.NoError(t, db.Create(&Model{ModelName: "gpt-4o", NameRule: NameRuleExact, Capabilities: `{"context":{"max_output_tokens":2000}}`}).Error)
	require.NoError(t, InitCapabilityCache())

	exact, ok := ResolveModelCapability("gpt-4o")
	require.True(t, ok)
	assert.Equal(t, int64(2000), *exact.Context.MaxOutputTokens)

	prefix, ok := ResolveModelCapability("gpt-4o-mini")
	require.True(t, ok)
	assert.Equal(t, int64(1500), *prefix.Context.MaxOutputTokens, "longer prefix must win over the family default")

	family, ok := ResolveModelCapability("gpt-5")
	require.True(t, ok)
	assert.Equal(t, int64(1000), *family.Context.MaxOutputTokens)

	_, ok = ResolveModelCapability("claude-3")
	assert.False(t, ok)
}

func TestChannelNarrowingMustStayWithinModelDeclaration(t *testing.T) {
	db := setupCapabilityModelTest(t)
	require.NoError(t, db.Create(&Model{ModelName: "m", NameRule: NameRuleExact, Capabilities: `{"context":{"max_output_tokens":1000},"tools":{"supported":true}}`}).Error)
	require.NoError(t, db.Create(&Channel{Id: 1, Name: "c1", Group: "default", Models: "m", Status: common.ChannelStatusEnabled}).Error)
	require.NoError(t, InitCapabilityCache())

	err := SetChannelModelCapability(1, "m", `{"context":{"max_output_tokens":2000}}`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds model-level")

	require.NoError(t, SetChannelModelCapability(1, "m", `{"context":{"max_output_tokens":500},"tools":{"supported":false}}`))

	over, ok := GetChannelModelCapability(1, "m")
	require.True(t, ok)
	assert.Equal(t, int64(500), *over.Context.MaxOutputTokens)

	effective, ok := EffectiveChannelCapability(1, "m")
	require.True(t, ok)
	assert.Equal(t, int64(500), *effective.Context.MaxOutputTokens)
	assert.False(t, *effective.Tools.Supported)

	// 没有收窄记录的渠道直接继承模型层声明。
	inherited, ok := EffectiveChannelCapability(2, "m")
	require.True(t, ok)
	assert.Equal(t, int64(1000), *inherited.Context.MaxOutputTokens)
}

func TestChannelCapabilityRowsFollowChannelModelChanges(t *testing.T) {
	db := setupCapabilityModelTest(t)
	require.NoError(t, db.Create(&Model{ModelName: "keep", NameRule: NameRuleExact, Capabilities: `{"context":{"max_output_tokens":100}}`}).Error)
	require.NoError(t, db.Create(&Model{ModelName: "drop", NameRule: NameRuleExact, Capabilities: `{"context":{"max_output_tokens":100}}`}).Error)
	require.NoError(t, InitCapabilityCache())

	channel := &Channel{Id: 1, Name: "c", Group: "default", Models: "keep,drop", Status: common.ChannelStatusEnabled}
	require.NoError(t, db.Create(channel).Error)
	require.NoError(t, SetChannelModelCapability(1, "keep", `{"context":{"max_output_tokens":50}}`))
	require.NoError(t, SetChannelModelCapability(1, "drop", `{"context":{"max_output_tokens":50}}`))

	channel.Models = "keep"
	require.NoError(t, channel.UpdateAbilities(nil))

	var rows []ChannelModelCapability
	require.NoError(t, db.Find(&rows).Error)
	require.Len(t, rows, 1)
	assert.Equal(t, "keep", rows[0].Model)
	_, ok := GetChannelModelCapability(1, "drop")
	assert.False(t, ok)

	require.NoError(t, DeleteChannelModelCapability(1, "keep"))
	require.NoError(t, channel.Delete())
	require.NoError(t, db.Find(&rows).Error)
	assert.Empty(t, rows)
}
