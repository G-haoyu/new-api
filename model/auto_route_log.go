package model

import (
	"context"
)

// 一次 auto 请求的选路结果。
const (
	AutoRouteOutcomeRouted = "routed" // 选出了模型
	AutoRouteOutcomeFailed = "failed" // 没有任何模型能服务，请求被拦下
)

const (
	// AutoRouteLogDetailMax 是 detail 的字节上限。MySQL 的 TEXT 最多 65535 字节，
	// 一行超限会让整批 INSERT 失败，所以写入方超出时要先精简内容，不能靠截断 JSON。
	AutoRouteLogDetailMax = 32 * 1024

	// autoRouteLogInsertChunk 是单条 INSERT 的行数。15 列 × 50 行 = 750 个占位符，
	// 加上最坏 50 × 32KB 的 detail，仍远低于各数据库的单语句和单包上限。
	autoRouteLogInsertChunk = 50
)

// AutoRouteLog 是一次 auto 请求的选路留痕：指令长什么样、怎么判断的、各榜单试了什么、
// 为什么没选更靠前的模型。它是追加写的遥测数据，落在日志库（LOG_SQL_DSN 为空时与主库同库），
// 由 service 里的异步 writer 批量写入，不在请求路径上。
//
// 列只放列表要显示、要筛选的内容，完整过程在 Detail（JSON）里。
type AutoRouteLog struct {
	Id        int64 `json:"id" gorm:"primaryKey"`
	CreatedAt int64 `json:"created_at" gorm:"bigint;index"`
	// RequestId 与 logs、relay_attempts 的 request_id 同源，使用日志的详情靠它找到这里。
	RequestId  string `json:"request_id" gorm:"type:varchar(64);index"`
	UserId     int    `json:"user_id" gorm:"index"`
	TokenId    int    `json:"token_id"`
	UsingGroup string `json:"using_group" gorm:"type:varchar(64)"`
	Outcome    string `json:"outcome" gorm:"type:varchar(16);index"`
	// Judgment 是判断的来源（现调 jev、缓存、辅助请求……），取值见 pkg/autorouter 的 Judgment*。
	Judgment string `json:"judgment" gorm:"type:varchar(32)"`
	// Source 是路由的来源：jev、cache、inherit、default。
	Source string `json:"source" gorm:"type:varchar(16)"`
	// TaskType 是 jev 判出的任务类型，没有判断时为空。
	TaskType string `json:"task_type" gorm:"type:varchar(64)"`
	// Model 是榜单里的基础模型名，不带档位后缀，选路失败时为空。
	Model string `json:"model" gorm:"type:varchar(255);index"`
	// Effort 是这次请求最终用的推理档位；沿用客户端自己字段的请求没有这个值。
	Effort     string `json:"effort" gorm:"type:varchar(16)"`
	Preference string `json:"preference" gorm:"type:varchar(16)"`
	// JevLatencyMs 只在这次请求调过 jev 时有值。
	JevLatencyMs int64 `json:"jev_latency_ms"`
	// Detail 是完整的选路过程（JSON），不超过 AutoRouteLogDetailMax 字节。
	Detail string `json:"detail" gorm:"type:text"`
}

// BatchInsertAutoRouteLogs 向日志库追加一批留痕，调用方是异步 writer。
func BatchInsertAutoRouteLogs(logs []*AutoRouteLog) error {
	if len(logs) == 0 {
		return nil
	}
	return LOG_DB.CreateInBatches(logs, autoRouteLogInsertChunk).Error
}

// CleanupAutoRouteLogs 分块删除 before 之前的留痕，每块行数由
// AUTO_ROUTE_LOG_CLEANUP_CHUNK 调整，细节见 purgeBefore。
func CleanupAutoRouteLogs(ctx context.Context, before int64) (int64, error) {
	return purgeBefore[AutoRouteLog](ctx, "AUTO_ROUTE_LOG_CLEANUP_CHUNK", before)
}

// HasAutoRouteLogsBefore 报告是否还有 before 之前的留痕，供清理任务判断要不要运行。
func HasAutoRouteLogsBefore(before int64) bool {
	return hasRowsBefore[AutoRouteLog](before)
}
