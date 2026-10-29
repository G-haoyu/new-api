package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/autorouter_setting"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// setupAutoRouterControllerTest gives the handlers a database, an empty option map,
// and an auto router setting that no environment variable of the developer leaks into.
func setupAutoRouterControllerTest(t *testing.T) *gorm.DB {
	t.Helper()
	gin.SetMode(gin.TestMode)
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.SystemTask{}, &model.SystemTaskLock{}, &model.Option{}, &model.AuditLog{}, &model.User{}, &model.AutoRouteLog{}))

	previousDB, previousLogDB, previousOptions := model.DB, model.LOG_DB, common.OptionMap
	model.DB, model.LOG_DB, common.OptionMap = db, db, map[string]string{}
	setting := autorouter_setting.GetAutoRouterSetting()
	savedSetting := *setting
	t.Cleanup(func() {
		*setting = savedSetting
		model.DB, model.LOG_DB, common.OptionMap = previousDB, previousLogDB, previousOptions
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})

	for _, name := range []string{
		"JEV_ENDPOINT", "JEV_MODEL", "JEV_API_KEY",
		"ARTIFICIAL_ANALYSIS_API_KEY", "DESIGN_ARENA_API_KEY", "OPENROUTER_API_KEY",
	} {
		t.Setenv(name, "")
	}
	setting.JevEndpoint, setting.JevModel, setting.JevAPIKey = "", "", ""
	setting.ArtificialAnalysisKey, setting.DesignArenaKey, setting.OpenRouterKey = "", "", ""
	return db
}

func callAutoRouterHandler(t *testing.T, handler gin.HandlerFunc) tokenAPIResponse {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/autorouter", nil)
	c.Set("id", 1)
	c.Set("role", common.RoleRootUser)
	handler(c)
	require.Equal(t, http.StatusOK, recorder.Code)
	var response tokenAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	return response
}

func TestAutoRouterRankingRefreshNeedsKeysAndDoesNotStack(t *testing.T) {
	db := setupAutoRouterControllerTest(t)
	setting := autorouter_setting.GetAutoRouterSetting()

	response := callAutoRouterHandler(t, RefreshAutoRouterRanking)
	assert.False(t, response.Success, "没有榜单 key 时不建任务")
	var tasks int64
	require.NoError(t, db.Model(&model.SystemTask{}).Count(&tasks).Error)
	assert.Zero(t, tasks)

	setting.ArtificialAnalysisKey, setting.OpenRouterKey = "aa-key", "or-key"
	type refreshData struct {
		Created bool                     `json:"created"`
		Task    model.SystemTaskResponse `json:"task"`
	}
	response = callAutoRouterHandler(t, RefreshAutoRouterRanking)
	require.True(t, response.Success, response.Message)
	var first refreshData
	require.NoError(t, common.Unmarshal(response.Data, &first))
	assert.True(t, first.Created)
	assert.Equal(t, model.SystemTaskTypeAutoRouterRank, first.Task.Type)

	response = callAutoRouterHandler(t, RefreshAutoRouterRanking)
	require.True(t, response.Success, response.Message)
	var second refreshData
	require.NoError(t, common.Unmarshal(response.Data, &second))
	assert.False(t, second.Created, "已有进行中的刷新时返回它，不重复建")
	assert.Equal(t, first.Task.TaskID, second.Task.TaskID)

	assert.False(t, callAutoRouterHandler(t, RestoreAutoRouterRanking).Success, "刷新进行中不能恢复内置，否则马上被刷新结果盖掉")

	var audits int64
	require.NoError(t, db.Model(&model.AuditLog{}).Where("action = ?", "autorouter.rank_refresh").Count(&audits).Error)
	assert.EqualValues(t, 1, audits, "只有真正新建的那次刷新才记审计")
}

func TestAutoRouterStatusHidesSecretsAndRestoreReturnsToTheEmbeddedRanking(t *testing.T) {
	db := setupAutoRouterControllerTest(t)
	setting := autorouter_setting.GetAutoRouterSetting()
	setting.JevEndpoint, setting.JevAPIKey = "https://jev.example/v1/decisions", "jev-secret-value"
	t.Setenv("OPENROUTER_API_KEY", "or-env-secret")

	response := callAutoRouterHandler(t, GetAutoRouterStatus)
	require.True(t, response.Success, response.Message)
	for _, leaked := range []string{"jev-secret-value", "or-env-secret", "jev.example"} {
		assert.NotContains(t, string(response.Data), leaked)
	}
	var status struct {
		JevReady      bool              `json:"jev_ready"`
		RankKeysReady bool              `json:"rank_keys_ready"`
		Sources       map[string]string `json:"sources"`
		RankSources   map[string]string `json:"rank_sources"`
	}
	require.NoError(t, common.Unmarshal(response.Data, &status))
	assert.True(t, status.JevReady)
	assert.NotEmpty(t, status.RankSources["generated_at"], "状态里带当前榜单的来源快照，前端不必为此拉整份榜单")
	assert.False(t, status.RankKeysReady, "没有 Artificial Analysis 的 key")
	assert.Equal(t, "option", status.Sources["jev_api_key"])
	assert.Equal(t, "env", status.Sources["openrouter_api_key"])
	assert.Empty(t, status.Sources["artificial_analysis_api_key"])

	setting.RankSources = map[string]string{"taxonomy_version": "1"}
	setting.TaskModels = map[string][]string{"stale": {"x"}}
	require.False(t, setting.RankingCurrent())
	response = callAutoRouterHandler(t, RestoreAutoRouterRanking)
	require.True(t, response.Success, response.Message)
	assert.True(t, setting.RankingCurrent())
	assert.NotContains(t, setting.TaskModels, "stale")
	var stored []model.Option
	require.NoError(t, db.Find(&stored).Error)
	assert.Len(t, stored, 4, "task_models、task_scores、default_models、rank_sources 四项写回库")

	response = callAutoRouterHandler(t, GetAutoRouterRanking)
	require.True(t, response.Success, response.Message)
	var ranking struct {
		Current  bool `json:"current"`
		Profiles []struct {
			Profile string `json:"profile"`
			Models  []struct {
				Model string   `json:"model"`
				Score *float64 `json:"score"`
			} `json:"models"`
		} `json:"profiles"`
	}
	require.NoError(t, common.Unmarshal(response.Data, &ranking))
	assert.True(t, ranking.Current)
	profiles := map[string]int{}
	for _, profile := range ranking.Profiles {
		profiles[profile.Profile] = len(profile.Models)
		for _, entry := range profile.Models {
			assert.NotNil(t, entry.Score, "%s/%s 内置榜单每个模型都有榜单分", profile.Profile, entry.Model)
		}
	}
	assert.Equal(t, len(setting.DefaultModels), profiles["general"], "general 画像用 default_models")
	assert.Equal(t, len(setting.TaskModels["code.build"]), profiles["code.build"])
}

func callAutoRouteLogHandler(t *testing.T, handler gin.HandlerFunc, target string, params gin.Params) tokenAPIResponse {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, target, nil)
	c.Params = params
	handler(c)
	require.Equal(t, http.StatusOK, recorder.Code)
	var response tokenAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	return response
}

func TestAutoRouteLogEndpointsListWithoutDetailAndReturnTheProcessByRequestId(t *testing.T) {
	setupAutoRouterControllerTest(t)
	require.NoError(t, model.BatchInsertAutoRouteLogs([]*model.AutoRouteLog{
		{CreatedAt: 100, RequestId: "req-ok", UserId: 1, Model: "coder", Outcome: model.AutoRouteOutcomeRouted, Detail: `{"result":{"resolved_model":"coder-high"},"trace":{"judgment":"live"}}`},
		{CreatedAt: 200, RequestId: "req-failed", UserId: 2, Outcome: model.AutoRouteOutcomeFailed, Detail: `{"error":"no model"}`},
		{CreatedAt: 300, RequestId: "req-broken", UserId: 2, Outcome: model.AutoRouteOutcomeRouted, Detail: `not json`},
	}))

	list := callAutoRouteLogHandler(t, GetAutoRouteLogs, "/api/auto-route-logs/?user_id=2&page_size=10", nil)
	require.True(t, list.Success, list.Message)
	var page struct {
		Items []map[string]any `json:"items"`
		Total int64            `json:"total"`
	}
	require.NoError(t, common.Unmarshal(list.Data, &page))
	assert.Equal(t, int64(2), page.Total)
	require.Len(t, page.Items, 2)
	assert.Equal(t, "req-broken", page.Items[0]["request_id"], "按时间倒序")
	for _, item := range page.Items {
		assert.NotContains(t, item, "detail", "列表不带 detail")
	}

	one := callAutoRouteLogHandler(t, GetAutoRouteLog, "/api/auto-route-logs/req-ok", gin.Params{{Key: "request_id", Value: "req-ok"}})
	require.True(t, one.Success, one.Message)
	var entry struct {
		RequestId string         `json:"request_id"`
		Model     string         `json:"model"`
		Detail    map[string]any `json:"detail"`
	}
	require.NoError(t, common.Unmarshal(one.Data, &entry))
	assert.Equal(t, "req-ok", entry.RequestId)
	assert.Equal(t, "coder", entry.Model)
	assert.Equal(t, "live", entry.Detail["trace"].(map[string]any)["judgment"], "detail 是对象，不是一段 JSON 文本")

	broken := callAutoRouteLogHandler(t, GetAutoRouteLog, "/api/auto-route-logs/req-broken", gin.Params{{Key: "request_id", Value: "req-broken"}})
	require.True(t, broken.Success, broken.Message)
	assert.Contains(t, string(broken.Data), `"detail":null`, "detail 损坏时其余列照样返回")
	assert.Contains(t, string(broken.Data), `"request_id":"req-broken"`)

	missing := callAutoRouteLogHandler(t, GetAutoRouteLog, "/api/auto-route-logs/nope", gin.Params{{Key: "request_id", Value: "nope"}})
	assert.True(t, missing.Success, "没有这条日志是正常情况")
	assert.Equal(t, "null", string(missing.Data))
}
