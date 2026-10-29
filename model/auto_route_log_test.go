package model

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestAutoRouteLogsAreInsertedInChunksAndCleanedUpByAge(t *testing.T) {
	require.NoError(t, LOG_DB.Exec("DELETE FROM auto_route_logs").Error)
	t.Cleanup(func() { LOG_DB.Exec("DELETE FROM auto_route_logs") })

	// 一批 125 行要分三条 INSERT（50 + 50 + 25）；其中 122 条过期，3 条新的必须保留。
	logs := make([]*AutoRouteLog, 0, 125)
	for i := range 122 {
		logs = append(logs, &AutoRouteLog{CreatedAt: int64(1 + i%25), RequestId: "old", Outcome: AutoRouteOutcomeRouted, Detail: "{}"})
	}
	for i := range 3 {
		logs = append(logs, &AutoRouteLog{CreatedAt: int64(10000 + i), RequestId: "new", Outcome: AutoRouteOutcomeFailed, Detail: "{}"})
	}
	require.NoError(t, BatchInsertAutoRouteLogs(logs))
	var total int64
	require.NoError(t, LOG_DB.Model(&AutoRouteLog{}).Count(&total).Error)
	assert.Equal(t, int64(125), total)
	assert.True(t, HasAutoRouteLogsBefore(5000))

	// 清理块的大小用 AUTO_ROUTE_LOG_CLEANUP_CHUNK 调，122 行按 50 一块要删三次。
	var deletes atomic.Int64
	const callbackName = "count_auto_route_log_deletes"
	require.NoError(t, LOG_DB.Callback().Delete().After("gorm:delete").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table == "auto_route_logs" && tx.Error == nil {
			deletes.Add(1)
		}
	}))
	t.Cleanup(func() { require.NoError(t, LOG_DB.Callback().Delete().Remove(callbackName)) })
	t.Setenv("AUTO_ROUTE_LOG_CLEANUP_CHUNK", "50")

	deleted, err := CleanupAutoRouteLogs(context.Background(), 5000)

	require.NoError(t, err)
	assert.Equal(t, int64(122), deleted)
	assert.Equal(t, int64(3), deletes.Load())
	assert.False(t, HasAutoRouteLogsBefore(5000))
	var remaining []AutoRouteLog
	require.NoError(t, LOG_DB.Find(&remaining).Error)
	require.Len(t, remaining, 3)
	for _, log := range remaining {
		assert.Equal(t, "new", log.RequestId)
	}
}

// PostgreSQL 一条语句最多 65535 个绑定参数，而 AUTO_ROUTE_LOG_BATCH_SIZE 最大可调到 10000 行，
// 所以再大的一批也不能拼成一条 INSERT。SQLite 本身不卡这个上限，靠统计每条语句的参数个数来守住。
func TestBatchInsertAutoRouteLogsKeepsEveryStatementUnderThePostgresParameterLimit(t *testing.T) {
	require.NoError(t, LOG_DB.Exec("DELETE FROM auto_route_logs").Error)
	t.Cleanup(func() { LOG_DB.Exec("DELETE FROM auto_route_logs") })
	var widest atomic.Int64
	const callbackName = "measure_auto_route_log_inserts"
	require.NoError(t, LOG_DB.Callback().Create().After("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table != "auto_route_logs" {
			return
		}
		for width := int64(len(tx.Statement.Vars)); ; {
			current := widest.Load()
			if width <= current || widest.CompareAndSwap(current, width) {
				return
			}
		}
	}))
	t.Cleanup(func() { require.NoError(t, LOG_DB.Callback().Create().Remove(callbackName)) })

	logs := make([]*AutoRouteLog, 10000)
	for i := range logs {
		logs[i] = &AutoRouteLog{CreatedAt: int64(1 + i), Detail: "{}"}
	}
	require.NoError(t, BatchInsertAutoRouteLogs(logs))

	var total int64
	require.NoError(t, LOG_DB.Model(&AutoRouteLog{}).Count(&total).Error)
	assert.Equal(t, int64(10000), total)
	assert.Positive(t, widest.Load())
	assert.LessOrEqual(t, widest.Load(), int64(65535))
}

// 这张表是新增的，没有旧结构要升级；迁移连跑两次证明幂等，列表要用的索引都在。
func TestAutoRouteLogTableMigrationIsIdempotent(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })

	for range 2 {
		require.NoError(t, db.AutoMigrate(&AutoRouteLog{}))
	}

	for _, column := range []string{"created_at", "request_id", "user_id", "outcome", "model"} {
		assert.True(t, db.Migrator().HasIndex(&AutoRouteLog{}, "idx_auto_route_logs_"+column), "index on %s", column)
	}
	require.NoError(t, db.Create(&AutoRouteLog{CreatedAt: 1, RequestId: "req", Detail: "{}"}).Error)
	var total int64
	require.NoError(t, db.Model(&AutoRouteLog{}).Count(&total).Error)
	assert.Equal(t, int64(1), total)
}
