package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupModelSquareControllerTest(t *testing.T) *gorm.DB {
	t.Helper()
	previousDB := model.DB
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.Model{},
		&model.Vendor{},
		&model.Channel{},
		&model.Ability{},
	))
	model.DB = db
	model.InvalidatePricingCache()
	t.Cleanup(func() {
		model.DB = previousDB
		model.InvalidatePricingCache()
		sqlDB, dbErr := db.DB()
		if dbErr == nil {
			require.NoError(t, sqlDB.Close())
		}
	})
	return db
}

func seedModelSquareModel(t *testing.T, db *gorm.DB, id int, name string, capabilities string) {
	t.Helper()
	require.NoError(t, db.Create(&model.Model{
		Id:           id,
		ModelName:    name,
		Status:       1,
		NameRule:     model.NameRuleExact,
		Capabilities: capabilities,
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		Group:     "default",
		Model:     name,
		ChannelId: id,
		Enabled:   true,
	}).Error)
}

func TestGetModelSquareSupportsPagination(t *testing.T) {
	db := setupModelSquareControllerTest(t)
	for index := 1; index <= 3; index++ {
		seedModelSquareModel(t, db, index, fmt.Sprintf("catalog-model-%d", index), "")
	}
	model.InvalidatePricingCache()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodGet, "/api/model-square?offset=1&limit=1", nil)

	GetModelSquare(context)

	assert.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Data struct {
			Items  []modelSquareItem `json:"items"`
			Total  int64             `json:"total"`
			Offset int               `json:"offset"`
			Limit  int               `json:"limit"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.Len(t, response.Data.Items, 1)
	assert.Equal(t, 2, response.Data.Items[0].ID)
	assert.Equal(t, "catalog-model-2", response.Data.Items[0].ModelName)
	assert.Equal(t, int64(3), response.Data.Total)
	assert.Equal(t, 1, response.Data.Offset)
	assert.Equal(t, 1, response.Data.Limit)
}

func TestGetModelSquareDetailExposesCapabilityDerivedFields(t *testing.T) {
	db := setupModelSquareControllerTest(t)
	capabilities := `{
		"context": {"max_output_tokens": 8192},
		"modalities": {"input": {"text": true, "image": {"supported": true}}, "output": ["text"]},
		"tools": {"supported": true, "tool_choice": ["auto", "required"]},
		"structured_output": {"json_schema": true},
		"reasoning": {"supported": true, "efforts": ["low", "high"]},
		"params": {"temperature": {"min": 0, "max": 1.5}, "top_p": {"min": 0, "max": 1}, "n": {"max": 2}},
		"streaming": {"supported": true}
	}`
	seedModelSquareModel(t, db, 1, "capability-model", capabilities)
	model.InvalidatePricingCache()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Params = gin.Params{{Key: "id", Value: "1"}}
	context.Request = httptest.NewRequest(http.MethodGet, "/api/model-square/1", nil)

	GetModelSquareDetail(context)

	assert.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool            `json:"success"`
		Data    modelSquareItem `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	assert.Contains(t, response.Data.Capabilities, "structured_output")
	assert.Contains(t, response.Data.Capabilities, "reasoning")
	assert.Contains(t, response.Data.InputModalities, "image")
	assert.Equal(t, int64(8192), *response.Data.MaxOutputTokens)
	parameters := make(map[string]struct{ Type, Range string }, len(response.Data.Parameters))
	for _, parameter := range response.Data.Parameters {
		parameters[parameter.Name] = struct{ Type, Range string }{Type: parameter.Type, Range: parameter.Range}
	}
	assert.Equal(t, "number", parameters["temperature"].Type)
	assert.Equal(t, "0 ~ 1.5", parameters["temperature"].Range)
	assert.Equal(t, "integer", parameters["n"].Type)
	assert.Equal(t, "1 ~ 2", parameters["n"].Range)
}

func TestGetModelSquareDetailRejectsUnknownModel(t *testing.T) {
	setupModelSquareControllerTest(t)

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Params = gin.Params{{Key: "id", Value: "404"}}
	context.Request = httptest.NewRequest(http.MethodGet, "/api/model-square/404", nil)

	GetModelSquareDetail(context)

	var response struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.False(t, response.Success)
	assert.Equal(t, "model not found", response.Message)
}
