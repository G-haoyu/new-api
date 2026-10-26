package model

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
)

const (
	// passed：全部渠道满足；soft：没有渠道被淘汰，但有"仅记录"维度的越界。
	CapabilityGateEventPassed = "passed"
	CapabilityGateEventSoft   = "soft"
	// filtered：enforce 下淘汰了部分渠道，请求仍由剩余渠道完成。
	CapabilityGateEventFiltered = "filtered"
	// rejected：enforce 下所有候选渠道都被淘汰，请求被拦下。
	CapabilityGateEventRejected = "rejected"
	// would_filter / would_reject 是 shadow 模式下"如果开启强制会怎样"的预演。
	CapabilityGateEventWouldFilter = "would_filter"
	CapabilityGateEventWouldReject = "would_reject"
)

// CapabilityGateEvent 是一次渠道级能力判定留痕。
// 只在真的有违规（hard 淘汰或 soft 越界）时写入，正常请求不产生行。
type CapabilityGateEvent struct {
	Id                int64  `json:"id" gorm:"primaryKey"`
	Mode              string `json:"mode" gorm:"type:varchar(16);index"`
	Model             string `json:"model" gorm:"type:varchar(255);index"`
	Action            string `json:"action" gorm:"type:varchar(16);index"`
	ChannelsEvaluated int    `json:"channels_evaluated"`
	RejectedChannels  string `json:"rejected_channels" gorm:"type:varchar(255)"`
	Dimensions        string `json:"dimensions" gorm:"type:text"`
	CreatedAt         int64  `json:"created_at" gorm:"bigint;index"`
}

func RecordCapabilityGateEvent(event *CapabilityGateEvent) error {
	if event == nil || strings.TrimSpace(event.Model) == "" || strings.TrimSpace(event.Action) == "" {
		return nil
	}
	if event.CreatedAt == 0 {
		event.CreatedAt = common.GetTimestamp()
	}
	return DB.Create(event).Error
}

type CapabilityGateEventQuery struct {
	Model     string
	Action    string
	StartTime int64
	EndTime   int64
	Offset    int
	Limit     int
}

func GetCapabilityGateEvents(query CapabilityGateEventQuery) ([]CapabilityGateEvent, int64, error) {
	db := DB.Model(&CapabilityGateEvent{})
	if modelName := strings.TrimSpace(query.Model); modelName != "" {
		db = db.Where("model = ?", modelName)
	}
	if action := strings.TrimSpace(query.Action); action != "" {
		db = db.Where("action = ?", action)
	}
	if query.StartTime > 0 {
		db = db.Where("created_at >= ?", query.StartTime)
	}
	if query.EndTime > 0 {
		db = db.Where("created_at <= ?", query.EndTime)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	limit := query.Limit
	if limit < 1 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	var events []CapabilityGateEvent
	err := db.Order("id DESC").Offset(max(query.Offset, 0)).Limit(limit).Find(&events).Error
	return events, total, err
}

func CleanupCapabilityGateEvents(before int64) (int64, error) {
	if before <= 0 {
		return 0, nil
	}
	result := DB.Where("created_at < ?", before).Delete(&CapabilityGateEvent{})
	return result.RowsAffected, result.Error
}
