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

func TestCapabilityTargetsExpandPrefixDeclarations(t *testing.T) {
	db := setupCapabilityModelTest(t)
	require.NoError(t, db.Create(&Model{ModelName: "deepseek-v4-pro", NameRule: NameRulePrefix, Capabilities: `{"context":{"max_output_tokens":100}}`}).Error)
	require.NoError(t, db.Create(&Channel{Id: 1, Name: "ds", Group: "default", Models: "deepseek-v4-pro", Status: common.ChannelStatusEnabled}).Error)
	require.NoError(t, db.Create(&Ability{Group: "default", Model: "deepseek-v4-pro", ChannelId: 1, Enabled: true}).Error)
	require.NoError(t, InitCapabilityCache())

	targets, _, err := GetCapabilityTargets(CapabilityTargetQuery{})
	require.NoError(t, err)
	require.Len(t, targets, 1)
	assert.Equal(t, "deepseek-v4-pro", targets[0].ModelName)
	assert.Equal(t, "deepseek-v4-pro", targets[0].DeclarationName)
	assert.Equal(t, NameRulePrefix, targets[0].NameRule)
	assert.Contains(t, targets[0].Capabilities, "max_output_tokens")
	require.Len(t, targets[0].Channels, 1)
	assert.Equal(t, 1, targets[0].Channels[0].ID)
}

func TestCapabilityTargetsFilterAndPaginate(t *testing.T) {
	db := setupCapabilityModelTest(t)
	require.NoError(t, db.Create(&Model{ModelName: "family-", NameRule: NameRulePrefix, Capabilities: `{"context":{"max_output_tokens":100}}`}).Error)
	require.NoError(t, db.Create(&Model{ModelName: "other-model", NameRule: NameRuleExact, Capabilities: `{"context":{"max_output_tokens":100}}`}).Error)
	require.NoError(t, db.Create(&[]Channel{
		{Id: 1, Name: "one", Group: "default", Models: "family-a,family-b", Status: common.ChannelStatusEnabled},
		{Id: 2, Name: "two", Group: "default", Models: "family-a,other-model", Status: common.ChannelStatusEnabled},
	}).Error)
	require.NoError(t, db.Create(&[]Ability{
		{Group: "default", Model: "family-a", ChannelId: 1, Enabled: true},
		{Group: "default", Model: "family-b", ChannelId: 1, Enabled: true},
		{Group: "default", Model: "family-a", ChannelId: 2, Enabled: true},
		{Group: "default", Model: "other-model", ChannelId: 2, Enabled: true},
	}).Error)
	require.NoError(t, InitCapabilityCache())

	targets, total, err := GetCapabilityTargets(CapabilityTargetQuery{})
	require.NoError(t, err)
	assert.Equal(t, 3, total)
	require.Len(t, targets, 3)
	assert.Equal(t, []string{"family-a", "family-b", "other-model"}, []string{targets[0].ModelName, targets[1].ModelName, targets[2].ModelName})
	assert.Equal(t, "family-", targets[0].DeclarationName)
	assert.Equal(t, NameRuleExact, targets[2].NameRule)

	targets, total, err = GetCapabilityTargets(CapabilityTargetQuery{Keyword: "FAMILY-B"})
	require.NoError(t, err)
	assert.Equal(t, 1, total)
	require.Len(t, targets, 1)
	assert.Equal(t, "family-b", targets[0].ModelName)

	// 渠道 2 只服务 family-a 与 other-model。
	targets, total, err = GetCapabilityTargets(CapabilityTargetQuery{ChannelID: 2})
	require.NoError(t, err)
	assert.Equal(t, 2, total)
	require.Len(t, targets, 2)
	assert.Equal(t, "family-a", targets[0].ModelName)
	assert.Equal(t, "other-model", targets[1].ModelName)
	require.Len(t, targets[0].Channels, 2)

	page, total, err := GetCapabilityTargets(CapabilityTargetQuery{Offset: 1, Limit: 1})
	require.NoError(t, err)
	assert.Equal(t, 3, total)
	require.Len(t, page, 1)
	assert.Equal(t, "family-b", page[0].ModelName)

	page, total, err = GetCapabilityTargets(CapabilityTargetQuery{Offset: 5, Limit: 10})
	require.NoError(t, err)
	assert.Equal(t, 3, total)
	assert.Empty(t, page)
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

	targets, _, err := GetCapabilityTargets(CapabilityTargetQuery{})
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

func TestValidateModelCapabilityNarrowingsRejectsStaleChannel(t *testing.T) {
	db := setupCapabilityModelTest(t)
	declaration := Model{ModelName: "strict-model", NameRule: NameRuleExact, Capabilities: `{"params":{"temperature":{"min":0,"max":2}}}`}
	require.NoError(t, db.Create(&declaration).Error)
	require.NoError(t, db.Create(&Channel{Id: 3, Name: "c3", Group: "default", Models: "strict-model", Status: common.ChannelStatusEnabled}).Error)
	require.NoError(t, InitCapabilityCache())
	require.NoError(t, SetChannelModelCapability(3, "strict-model", `{"params":{"temperature":{"min":0,"max":2}}}`))

	// 渠道收窄照抄了旧的模型级区间；模型级收紧后必须拒绝保存。
	tightened := declaration
	tightened.Capabilities = `{"params":{"temperature":{"min":0,"max":1}}}`
	err := ValidateModelCapabilityNarrowings(tightened)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "channel #3")
	assert.Contains(t, err.Error(), "params.temperature")

	// 仍覆盖存量收窄时保存通过。
	require.NoError(t, ValidateModelCapabilityNarrowings(declaration))
}
