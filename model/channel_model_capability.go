package model

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service/capability"

	"gorm.io/gorm"
)

// ChannelModelCapability 是某个渠道对某个模型的能力收窄。
// 键是渠道声明的公开模型名（管理员在渠道 Models 里勾的名字），不是映射后的上游名。
type ChannelModelCapability struct {
	ChannelId    int    `json:"channel_id" gorm:"primaryKey;autoIncrement:false"`
	Model        string `json:"model" gorm:"type:varchar(255);primaryKey;autoIncrement:false"`
	Capabilities string `json:"capabilities" gorm:"type:text"`
	UpdatedTime  int64  `json:"updated_time" gorm:"bigint"`
}

func (ChannelModelCapability) TableName() string {
	return "channel_model_capabilities"
}

type modelCapabilityRecord struct {
	name string
	rule int
	spec capability.Spec
}

var capabilityCache struct {
	sync.RWMutex
	initialized bool
	exact       map[string]capability.Spec
	prefix      []modelCapabilityRecord
	suffix      []modelCapabilityRecord
	contains    []modelCapabilityRecord
	channel     map[int]map[string]capability.Spec
}

// InitCapabilityCache 重新加载模型级声明与渠道收窄。写入路径在提交事务后调用。
func InitCapabilityCache() error {
	if DB == nil {
		return nil
	}
	var models []Model
	if err := DB.Select("id", "model_name", "name_rule", "capabilities").Order("id").Find(&models).Error; err != nil {
		return err
	}
	exact := make(map[string]capability.Spec)
	var prefix, suffix, contains []modelCapabilityRecord
	for _, metadata := range models {
		spec, problems := capability.Parse(metadata.Capabilities)
		if len(problems) > 0 {
			common.SysError(fmt.Sprintf("model capability declaration ignored: model=%s id=%d: %s", metadata.ModelName, metadata.Id, strings.Join(problems, "; ")))
			continue
		}
		if strings.TrimSpace(metadata.Capabilities) == "" {
			continue
		}
		record := modelCapabilityRecord{name: metadata.ModelName, rule: metadata.NameRule, spec: spec}
		switch metadata.NameRule {
		case NameRulePrefix:
			prefix = append(prefix, record)
		case NameRuleSuffix:
			suffix = append(suffix, record)
		case NameRuleContains:
			contains = append(contains, record)
		default:
			exact[metadata.ModelName] = spec
		}
	}

	var rows []ChannelModelCapability
	if err := DB.Find(&rows).Error; err != nil {
		return err
	}
	channel := make(map[int]map[string]capability.Spec)
	for _, row := range rows {
		spec, problems := capability.Parse(row.Capabilities)
		if len(problems) > 0 {
			common.SysError(fmt.Sprintf("channel capability declaration ignored: channel=%d model=%s: %s", row.ChannelId, row.Model, strings.Join(problems, "; ")))
			continue
		}
		if channel[row.ChannelId] == nil {
			channel[row.ChannelId] = make(map[string]capability.Spec)
		}
		channel[row.ChannelId][row.Model] = spec
	}

	capabilityCache.Lock()
	capabilityCache.exact = exact
	capabilityCache.prefix = prefix
	capabilityCache.suffix = suffix
	capabilityCache.contains = contains
	capabilityCache.channel = channel
	capabilityCache.initialized = true
	capabilityCache.Unlock()
	return nil
}

// InvalidateCapabilityCache 清空解析结果；数据库被外部直接修改后可调用。
func InvalidateCapabilityCache() {
	capabilityCache.Lock()
	capabilityCache.initialized = false
	capabilityCache.exact = nil
	capabilityCache.prefix = nil
	capabilityCache.suffix = nil
	capabilityCache.contains = nil
	capabilityCache.channel = nil
	capabilityCache.Unlock()
}

// HasCapabilityDeclarations 报告是否至少有一条模型级能力声明。
// 没有声明时门控整体关闭，避免 default shadow 下白解析每个请求体。
func HasCapabilityDeclarations() bool {
	capabilityCache.RLock()
	defer capabilityCache.RUnlock()
	return capabilityCache.initialized &&
		(len(capabilityCache.exact) > 0 || len(capabilityCache.prefix) > 0 ||
			len(capabilityCache.suffix) > 0 || len(capabilityCache.contains) > 0)
}

// ResolveModelCapability 按 name_rule 的优先级（精确、前缀、后缀、包含）取模型级声明。
// 同一规则内以更长的匹配键优先，这样 claude-sonnet-4-5 的具体声明不会被
// claude-sonnet-4 的家族默认声明先命中。
func ResolveModelCapability(modelName string) (capability.Spec, bool) {
	capabilityCache.RLock()
	defer capabilityCache.RUnlock()
	if !capabilityCache.initialized {
		return capability.Spec{}, false
	}
	if spec, ok := capabilityCache.exact[modelName]; ok {
		return spec, true
	}
	for _, records := range [][]modelCapabilityRecord{capabilityCache.prefix, capabilityCache.suffix, capabilityCache.contains} {
		var matched *modelCapabilityRecord
		for i := range records {
			if !metadataMatchesName(records[i].name, records[i].rule, modelName) {
				continue
			}
			if matched == nil || len(records[i].name) > len(matched.name) {
				matched = &records[i]
			}
		}
		if matched != nil {
			return matched.spec, true
		}
	}
	return capability.Spec{}, false
}

func metadataMatchesName(name string, rule int, modelName string) bool {
	switch rule {
	case NameRulePrefix:
		return strings.HasPrefix(modelName, name)
	case NameRuleSuffix:
		return strings.HasSuffix(modelName, name)
	case NameRuleContains:
		return strings.Contains(modelName, name)
	default:
		return modelName == name
	}
}

// GetChannelModelCapability 返回渠道层收窄声明；没有记录时 ok=false。
func GetChannelModelCapability(channelID int, modelName string) (capability.Spec, bool) {
	capabilityCache.RLock()
	defer capabilityCache.RUnlock()
	if !capabilityCache.initialized {
		return capability.Spec{}, false
	}
	spec, ok := capabilityCache.channel[channelID][modelName]
	return spec, ok
}

// EffectiveChannelCapability 返回模型层声明被渠道收窄后的生效能力。
// 没有渠道记录时直接返回模型层声明；模型层未声明时 ok=false。
func EffectiveChannelCapability(channelID int, modelName string) (capability.Spec, bool) {
	base, ok := ResolveModelCapability(modelName)
	if !ok {
		return capability.Spec{}, false
	}
	over, hasOver := GetChannelModelCapability(channelID, modelName)
	if !hasOver {
		return base, true
	}
	return capability.Merge(base, over), true
}

// ValidateModelCapabilities 校验模型级声明自身是否合法。
func ValidateModelCapabilities(raw string) error {
	_, problems := capability.Parse(raw)
	if len(problems) == 0 {
		return nil
	}
	return errors.New(strings.Join(problems, "; "))
}

// ValidateChannelModelCapabilities 校验渠道收窄：声明自身合法，且不越出模型层。
func ValidateChannelModelCapabilities(modelName string, raw string) error {
	spec, problems := capability.Parse(raw)
	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	base, ok := ResolveModelCapability(modelName)
	if !ok {
		return fmt.Errorf("model %q has no model-level capability declaration", modelName)
	}
	if violations := capability.CheckSubset(base, spec); len(violations) > 0 {
		return errors.New(strings.Join(violations, "; "))
	}
	return nil
}

// SetChannelModelCapability 保存一条渠道收窄并刷新缓存。
func SetChannelModelCapability(channelID int, modelName string, raw string) error {
	if channelID <= 0 || strings.TrimSpace(modelName) == "" {
		return errors.New("channel_id and model are required")
	}
	channel, err := GetChannelById(channelID, false)
	if err != nil || channel == nil {
		return fmt.Errorf("channel #%d not found", channelID)
	}
	if !slices.Contains(channel.GetModels(), modelName) {
		return fmt.Errorf("channel #%d does not serve model %q", channelID, modelName)
	}
	if err := ValidateChannelModelCapabilities(modelName, raw); err != nil {
		return err
	}
	row := ChannelModelCapability{
		ChannelId:    channelID,
		Model:        modelName,
		Capabilities: strings.TrimSpace(raw),
		UpdatedTime:  common.GetTimestamp(),
	}
	if err := DB.Save(&row).Error; err != nil {
		return err
	}
	return InitCapabilityCache()
}

// DeleteChannelModelCapability 删除一条渠道收窄并刷新缓存。
func DeleteChannelModelCapability(channelID int, modelName string) error {
	if channelID <= 0 || strings.TrimSpace(modelName) == "" {
		return errors.New("channel_id and model are required")
	}
	if err := DB.Where("channel_id = ? AND model = ?", channelID, modelName).
		Delete(&ChannelModelCapability{}).Error; err != nil {
		return err
	}
	return InitCapabilityCache()
}

// CapabilityTargetChannel 是某个模型的一条可配置渠道。
type CapabilityTargetChannel struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	Type       int    `json:"type"`
	Configured bool   `json:"configured"`
}

// CapabilityTarget 是"模型 → 服务渠道"清单的一行。
type CapabilityTarget struct {
	ID           int                       `json:"id"`
	ModelName    string                    `json:"model_name"`
	Capabilities string                    `json:"capabilities,omitempty"`
	Channels     []CapabilityTargetChannel `json:"channels"`
}

// GetCapabilityTargets 返回精确规则模型及其服务渠道。
func GetCapabilityTargets() ([]CapabilityTarget, error) {
	var models []Model
	if err := DB.Where("name_rule = ?", NameRuleExact).Order("model_name").Find(&models).Error; err != nil {
		return nil, err
	}
	if len(models) == 0 {
		return []CapabilityTarget{}, nil
	}

	var rows []struct {
		Model string
		ID    int
		Name  string
		Type  int
	}
	err := DB.Table("abilities").
		Select("abilities.model as model, channels.id as id, channels.name as name, channels.type as type").
		Joins("JOIN channels ON channels.id = abilities.channel_id").
		Where("abilities.enabled = ? AND channels.status = ?", true, common.ChannelStatusEnabled).
		Distinct().
		Order("abilities.model, channels.id").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	channelsByModel := make(map[string][]CapabilityTargetChannel)
	for _, row := range rows {
		channelsByModel[row.Model] = append(channelsByModel[row.Model], CapabilityTargetChannel{
			ID:   row.ID,
			Name: row.Name,
			Type: row.Type,
		})
	}

	configuredRows, err := GetConfiguredModelCapabilityKeys()
	if err != nil {
		return nil, err
	}
	targets := make([]CapabilityTarget, 0, len(models))
	for _, metadata := range models {
		channels := channelsByModel[metadata.ModelName]
		for i := range channels {
			channels[i].Configured = configuredRows[ChannelModelCapabilityKey{ChannelId: channels[i].ID, Model: metadata.ModelName}]
		}
		if channels == nil {
			channels = []CapabilityTargetChannel{}
		}
		targets = append(targets, CapabilityTarget{
			ID:           metadata.Id,
			ModelName:    metadata.ModelName,
			Capabilities: metadata.Capabilities,
			Channels:     channels,
		})
	}
	return targets, nil
}

// ChannelModelCapabilityKey 标识一条渠道收窄记录。
type ChannelModelCapabilityKey struct {
	ChannelId int
	Model     string
}

// GetConfiguredModelCapabilityKeys 返回已有收窄记录的键集合。
func GetConfiguredModelCapabilityKeys() (map[ChannelModelCapabilityKey]bool, error) {
	var rows []ChannelModelCapability
	if err := DB.Select("channel_id", "model").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[ChannelModelCapabilityKey]bool, len(rows))
	for _, row := range rows {
		out[ChannelModelCapabilityKey{ChannelId: row.ChannelId, Model: row.Model}] = true
	}
	return out, nil
}

// pruneChannelModelCapabilitiesLocked 删除渠道已不再服务的模型对应的收窄记录。
// 调用方负责在事务内执行，并在提交后调用 InitCapabilityCache。
func pruneChannelModelCapabilitiesLocked(tx *gorm.DB, channelID int, modelNames []string) error {
	keep := make(map[string]struct{}, len(modelNames))
	for _, name := range modelNames {
		keep[name] = struct{}{}
	}
	var rows []ChannelModelCapability
	if err := tx.Select("channel_id", "model").Where("channel_id = ?", channelID).Find(&rows).Error; err != nil {
		return err
	}
	stale := make([]string, 0)
	for _, row := range rows {
		if _, ok := keep[row.Model]; !ok {
			stale = append(stale, row.Model)
		}
	}
	if len(stale) == 0 {
		return nil
	}
	sort.Strings(stale)
	return tx.Where("channel_id = ? AND model IN ?", channelID, stale).
		Delete(&ChannelModelCapability{}).Error
}
