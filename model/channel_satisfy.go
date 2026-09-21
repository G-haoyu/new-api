package model

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

func IsChannelEnabledForGroupModel(group string, modelName string, channelID int) bool {
	if group == "" || modelName == "" || channelID <= 0 {
		return false
	}
	if !common.MemoryCacheEnabled {
		return isChannelEnabledForGroupModelDB(group, modelName, channelID)
	}

	channelSyncLock.RLock()
	defer channelSyncLock.RUnlock()

	if group2model2channels == nil {
		return false
	}

	if isChannelIDInList(group2model2channels[group][modelName], channelID) {
		return true
	}
	normalized := ratio_setting.FormatMatchingModelName(modelName)
	if normalized != "" && normalized != modelName {
		return isChannelIDInList(group2model2channels[group][normalized], channelID)
	}
	return false
}

func IsChannelEnabledForAnyGroupModel(groups []string, modelName string, channelID int) bool {
	if len(groups) == 0 {
		return false
	}
	for _, g := range groups {
		if IsChannelEnabledForGroupModel(g, modelName, channelID) {
			return true
		}
	}
	return false
}

func isChannelEnabledForGroupModelDB(group string, modelName string, channelID int) bool {
	var count int64
	err := DB.Model(&Ability{}).
		Where(commonGroupCol+" = ? and model = ? and channel_id = ? and enabled = ?", group, modelName, channelID, true).
		Count(&count).Error
	if err == nil && count > 0 {
		return true
	}
	normalized := ratio_setting.FormatMatchingModelName(modelName)
	if normalized == "" || normalized == modelName {
		return false
	}
	count = 0
	err = DB.Model(&Ability{}).
		Where(commonGroupCol+" = ? and model = ? and channel_id = ? and enabled = ?", group, normalized, channelID, true).
		Count(&count).Error
	return err == nil && count > 0
}

func isChannelIDInList(list []int, channelID int) bool {
	for _, id := range list {
		if id == channelID {
			return true
		}
	}
	return false
}

// GetEnabledChannelIDsForGroupModel returns the same group/model channel
// scope used by native routing. It is intentionally a snapshot copy so callers
// can safely add request-path and other per-request filters.
func GetEnabledChannelIDsForGroupModel(group, modelName string) []int {
	if group == "" || modelName == "" {
		return []int{}
	}
	var ids []int
	if common.MemoryCacheEnabled {
		channelSyncLock.RLock()
		if group2model2channels != nil {
			ids = append(ids, group2model2channels[group][modelName]...)
			if len(ids) == 0 {
				normalized := ratio_setting.FormatMatchingModelName(modelName)
				if normalized != "" && normalized != modelName {
					ids = append(ids, group2model2channels[group][normalized]...)
				}
			}
		}
		channelSyncLock.RUnlock()
	} else {
		DB.Model(&Ability{}).
			Where(commonGroupCol+" = ? and enabled = ? and model = ?", group, commonTrueVal, modelName).
			Pluck("channel_id", &ids)
		if len(ids) == 0 {
			normalized := ratio_setting.FormatMatchingModelName(modelName)
			if normalized != "" && normalized != modelName {
				DB.Model(&Ability{}).
					Where(commonGroupCol+" = ? and enabled = ? and model = ?", group, commonTrueVal, normalized).
					Pluck("channel_id", &ids)
			}
		}
	}
	if ids == nil {
		return []int{}
	}
	return ids
}
