package model

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecordRelayLogsPreservesIselfEmail(t *testing.T) {
	if common.UsingLogDatabase(common.DatabaseTypeSQLite) {
		var version string
		require.NoError(t, LOG_DB.Raw("SELECT sqlite_version()").Scan(&version).Error)
		t.Logf("SQLite %s", version)
	}
	clearDialectTestLogs(t)
	t.Cleanup(func() { clearDialectTestLogs(t) })
	previousExport, previousConsume := common.DataExportEnabled, common.LogConsumeEnabled
	common.DataExportEnabled, common.LogConsumeEnabled = false, true
	t.Cleanup(func() { common.DataExportEnabled, common.LogConsumeEnabled = previousExport, previousConsume })

	for _, tt := range []struct {
		name    string
		logType int
		email   string
	}{
		{"consume with email and no affinity", LogTypeConsume, "alice@example.com"},
		{"error with email and no affinity", LogTypeError, "alice@example.com"},
		{"consume without email", LogTypeConsume, ""},
		{"error without email", LogTypeError, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Set("username", "iself")
			c.Set(common.RequestIdKey, tt.name)
			common.SetContextKey(c, constant.ContextKeyIselfEmail, tt.email)
			if tt.logType == LogTypeConsume {
				RecordConsumeLog(c, 0, RecordConsumeLogParams{ModelName: "test-model", Quota: 5, PromptTokens: 2, CompletionTokens: 1})
			} else {
				RecordErrorLog(c, 0, 0, "test-model", "test-token", "upstream failed", 0, 0, false, "default", nil)
			}

			var logs []Log
			require.NoError(t, LOG_DB.Where("request_id = ?", tt.name).Find(&logs).Error)
			require.Len(t, logs, 1)
			assert.Equal(t, tt.email, logs[0].IselfEmail)
			assert.Equal(t, tt.logType, logs[0].Type)
		})
	}
}
