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
	previousPricing := getModelSquarePricing
	getModelSquarePricing = func() []model.Pricing { return nil }
	t.Cleanup(func() {
		getModelSquarePricing = previousPricing
		model.DB = previousDB
		sqlDB, dbErr := db.DB()
		if dbErr == nil {
			require.NoError(t, sqlDB.Close())
		}
	})
	return db
}

func TestGetModelSquareSupportsPagination(t *testing.T) {
	db := setupModelSquareControllerTest(t)
	for index := 1; index <= 3; index++ {
		require.NoError(t, db.Create(&model.Model{
			Id:        index,
			ModelName: fmt.Sprintf("catalog-model-%d", index),
			Status:    1,
			NameRule:  model.NameRuleExact,
		}).Error)
		seedModelSquareListing(t, db, index)
	}

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

func TestGetModelSquareUsesDefaultPageSize(t *testing.T) {
	db := setupModelSquareControllerTest(t)
	for index := 1; index <= 25; index++ {
		require.NoError(t, db.Create(&model.Model{
			Id:        index,
			ModelName: fmt.Sprintf("default-page-model-%d", index),
			Status:    1,
			NameRule:  model.NameRuleExact,
		}).Error)
		seedModelSquareListing(t, db, index)
	}

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodGet, "/api/model-square", nil)

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
	assert.Len(t, response.Data.Items, 24)
	assert.Equal(t, int64(25), response.Data.Total)
	assert.Equal(t, 0, response.Data.Offset)
	assert.Equal(t, 24, response.Data.Limit)
}

func TestGetModelSquareExcludesModelsWithoutEnabledChannel(t *testing.T) {
	db := setupModelSquareControllerTest(t)
	require.NoError(t, db.Create(&model.Model{
		Id:        1,
		ModelName: "channel-only-model",
		Status:    1,
		NameRule:  model.NameRuleExact,
	}).Error)

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodGet, "/api/model-square", nil)

	GetModelSquare(context)

	assert.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Data struct {
			Items []modelSquareItem `json:"items"`
			Total int64             `json:"total"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.Empty(t, response.Data.Items)
	assert.Zero(t, response.Data.Total)
}

func TestGetModelSquareDetailExcludesModelsWithoutEnabledChannel(t *testing.T) {
	db := setupModelSquareControllerTest(t)
	require.NoError(t, db.Create(&model.Model{
		Id:        1,
		ModelName: "channel-only-detail-model",
		Status:    1,
		NameRule:  model.NameRuleExact,
	}).Error)

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Params = gin.Params{{Key: "id", Value: "1"}}
	context.Request = httptest.NewRequest(http.MethodGet, "/api/model-square/1", nil)

	GetModelSquareDetail(context)

	assert.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.False(t, response.Success)
	assert.Equal(t, "model not found", response.Message)
}

// seedModelSquareListing makes a model visible in the square: an enabled
// channel ability plus an entry in the pricing cache.
func seedModelSquareListing(t *testing.T, db *gorm.DB, modelID int) {
	t.Helper()
	var meta model.Model
	require.NoError(t, db.First(&meta, modelID).Error)
	require.NoError(t, db.Create(&model.Ability{
		Group:     "default",
		Model:     meta.ModelName,
		ChannelId: 1,
		Enabled:   true,
	}).Error)
	existing := getModelSquarePricing()
	getModelSquarePricing = func() []model.Pricing {
		return append(existing, model.Pricing{ModelName: meta.ModelName, ModelRatio: 1, CompletionRatio: 2})
	}
}

func TestModelSquarePricingFromRatio(t *testing.T) {
	cacheRatio := 0.1
	pricing := modelSquarePricingFromPricing(model.Pricing{
		ModelRatio:      1.5,
		CompletionRatio: 2,
		CacheRatio:      &cacheRatio,
	})

	assert.Equal(t, 3.0, pricing.InputPrice)
	assert.Equal(t, 6.0, pricing.OutputPrice)
	require.NotNil(t, pricing.CacheReadPrice)
	assert.InDelta(t, 0.3, *pricing.CacheReadPrice, 0.000001)
}

func TestModelSquarePricingFromFixedPrice(t *testing.T) {
	pricing := modelSquarePricingFromPricing(model.Pricing{QuotaType: 1, ModelPrice: 0.05})

	assert.Equal(t, 0.05, pricing.InputPrice)
	assert.Equal(t, 0.05, pricing.OutputPrice)
	assert.Nil(t, pricing.CacheReadPrice)
}
