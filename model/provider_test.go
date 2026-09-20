package model

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestProviderDeleteRemovesMetadataRecord(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Provider{}, &ModelProviderPrice{}, &Channel{}))

	provider := &Provider{Slug: "example", DisplayName: "Example", Status: 1}
	require.NoError(t, db.Create(provider).Error)

	oldDB := DB
	DB = db
	t.Cleanup(func() { DB = oldDB })

	require.NoError(t, provider.Delete())
	var stored Provider
	require.ErrorIs(t, db.First(&stored, provider.Id).Error, gorm.ErrRecordNotFound)
}

func TestProviderDeleteRejectsReferencedChannels(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Provider{}, &Channel{}))

	provider := &Provider{Slug: "referenced", DisplayName: "Referenced", Status: 1}
	require.NoError(t, db.Create(provider).Error)
	require.NoError(t, db.Create(&Channel{ProviderSlug: provider.Slug}).Error)

	oldDB := DB
	DB = db
	t.Cleanup(func() { DB = oldDB })

	assert.EqualError(t, provider.Delete(), "provider is referenced by channels")
	var stored Provider
	require.NoError(t, db.First(&stored, provider.Id).Error)
}

func TestChannelUpdateCanClearProviderSlug(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Channel{}, &Ability{}))

	oldDB := DB
	DB = db
	t.Cleanup(func() { DB = oldDB })

	channel := &Channel{
		Name:         "internal",
		Models:       "example-model",
		Group:        "default",
		ProviderSlug: "old-provider",
		Status:       1,
	}
	require.NoError(t, db.Create(channel).Error)
	channel.ProviderSlug = ""
	require.NoError(t, channel.Update())

	var stored Channel
	require.NoError(t, db.First(&stored, channel.Id).Error)
	assert.Empty(t, stored.ProviderSlug)
}

func TestModelProviderPriceValidatesModelAndProviderReferences(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Model{}, &Provider{}, &ModelProviderPrice{}))

	oldDB := DB
	DB = db
	t.Cleanup(func() { DB = oldDB })

	validModel := &Model{ModelName: "valid-model", Status: 1}
	validProvider := &Provider{Slug: "valid-provider", DisplayName: "Valid Provider", Status: 1}
	require.NoError(t, db.Create(validModel).Error)
	require.NoError(t, db.Create(validProvider).Error)

	assert.NoError(t, (&ModelProviderPrice{
		ModelId: validModel.Id, ProviderSlug: validProvider.Slug,
		InputPrice: 1, OutputPrice: 2,
	}).Create())
	assert.EqualError(t, (&ModelProviderPrice{
		ModelId: 999, ProviderSlug: validProvider.Slug,
		InputPrice: 1, OutputPrice: 2,
	}).Create(), "model not found")
	assert.EqualError(t, (&ModelProviderPrice{
		ModelId: validModel.Id, ProviderSlug: "missing-provider",
		InputPrice: 1, OutputPrice: 2,
	}).Create(), "provider not found")
	assert.EqualError(t, (&ModelProviderPrice{
		Id: 999, ModelId: validModel.Id, ProviderSlug: validProvider.Slug,
		InputPrice: 1, OutputPrice: 2,
	}).Update(), "model provider price not found")
}

func TestModelProviderPriceCalculatesDiscountedPrices(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Model{}, &Provider{}, &ModelProviderPrice{}))

	oldDB := DB
	DB = db
	t.Cleanup(func() { DB = oldDB })

	validModel := &Model{ModelName: "discounted-model", Status: 1}
	validProvider := &Provider{Slug: "discounted-provider", DisplayName: "Discounted Provider", Status: 1}
	require.NoError(t, db.Create(validModel).Error)
	require.NoError(t, db.Create(validProvider).Error)

	discount := 90.0
	inputListPrice := 1.40
	outputListPrice := 4.40
	price := &ModelProviderPrice{
		ModelId:         validModel.Id,
		ProviderSlug:    validProvider.Slug,
		InputPrice:      999,
		OutputPrice:     999,
		InputListPrice:  &inputListPrice,
		OutputListPrice: &outputListPrice,
		DiscountRate:    &discount,
	}
	require.NoError(t, price.Create())

	assert.InDelta(t, 1.26, price.InputPrice, 0.000001)
	assert.InDelta(t, 3.96, price.OutputPrice, 0.000001)
	prices, err := ListModelProviderPrice(validModel.Id)
	require.NoError(t, err)
	require.Len(t, prices, 1)
	assert.InDelta(t, 1.26, prices[0].InputPrice, 0.000001)
}

func TestModelProviderPriceRejectsInvalidDiscount(t *testing.T) {
	price := &ModelProviderPrice{InputPrice: 1, OutputPrice: 2}
	discount := 100.1
	price.DiscountRate = &discount

	assert.EqualError(t, price.normalize(), "discount rate must be between 0 and 100")
}
