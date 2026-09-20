package model

import (
	"errors"
	"math"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

type ProviderModelRelation struct {
	ModelId    int                 `json:"model_id"`
	ModelName  string              `json:"model_name"`
	ModelIcon  string              `json:"model_icon,omitempty"`
	VendorName string              `json:"vendor_name,omitempty"`
	VendorIcon string              `json:"vendor_icon,omitempty"`
	Configured bool                `json:"configured"`
	Available  bool                `json:"available"`
	Price      *ModelProviderPrice `json:"price,omitempty"`
}

// ModelProviderPrice stores platform display prices in USD per one million
// tokens for one model-provider pair.
type ModelProviderPrice struct {
	Id                  int      `json:"id" gorm:"primaryKey"`
	ModelId             int      `json:"model_id" gorm:"not null;uniqueIndex:idx_model_provider_price,priority:1;index"`
	ProviderSlug        string   `json:"provider_slug" gorm:"size:64;not null;uniqueIndex:idx_model_provider_price,priority:2;index"`
	InputPrice          float64  `json:"input_price" gorm:"not null"`
	OutputPrice         float64  `json:"output_price" gorm:"not null"`
	CacheReadPrice      *float64 `json:"cache_read_price,omitempty"`
	CacheWritePrice     *float64 `json:"cache_write_price,omitempty"`
	InputListPrice      *float64 `json:"input_list_price,omitempty"`
	OutputListPrice     *float64 `json:"output_list_price,omitempty"`
	CacheReadListPrice  *float64 `json:"cache_read_list_price,omitempty"`
	CacheWriteListPrice *float64 `json:"cache_write_list_price,omitempty"`
	DiscountRate        *float64 `json:"discount_rate,omitempty" gorm:"column:discount_percent"`
	SourceURL           string   `json:"source_url,omitempty" gorm:"size:512"`
	EffectiveAt         int64    `json:"effective_at,omitempty" gorm:"bigint"`
	CreatedTime         int64    `json:"created_time" gorm:"bigint"`
	UpdatedTime         int64    `json:"updated_time" gorm:"bigint"`
	ModelName           string   `json:"model_name,omitempty" gorm:"size:128"`
	ContextLength       int64    `json:"context_length,omitempty" gorm:"bigint"`
	MaxOutputTokens     int64    `json:"max_output_tokens,omitempty" gorm:"bigint"`
	Region              string   `json:"region,omitempty" gorm:"size:128"`
	Precision           string   `json:"precision,omitempty" gorm:"size:64"`
	Quantization        string   `json:"quantization,omitempty" gorm:"size:64"`
	SupportedParameters string   `json:"supported_parameters,omitempty" gorm:"type:text"`
	StreamCancellation  *bool    `json:"stream_cancellation,omitempty"`
	Free                *bool    `json:"free,omitempty"`
	Batch               *bool    `json:"batch,omitempty"`
}

func (ModelProviderPrice) TableName() string {
	return "model_provider_prices"
}

func (price *ModelProviderPrice) normalize() error {
	price.ProviderSlug = normalizeProviderRegistrySlug(price.ProviderSlug)
	price.ModelName = strings.TrimSpace(price.ModelName)
	price.Region = strings.TrimSpace(price.Region)
	price.Precision = strings.TrimSpace(price.Precision)
	price.Quantization = strings.TrimSpace(price.Quantization)
	price.SupportedParameters = strings.TrimSpace(price.SupportedParameters)
	price.SourceURL = strings.TrimSpace(price.SourceURL)
	if price.ModelId <= 0 || price.ProviderSlug == "" {
		return errors.New("model id and provider slug are required")
	}
	if !isFiniteNonNegative(price.InputPrice) || !isFiniteNonNegative(price.OutputPrice) {
		return errors.New("provider prices must be non-negative")
	}
	if price.ContextLength < 0 || price.MaxOutputTokens < 0 {
		return errors.New("context length and max output tokens must be non-negative")
	}
	if price.CacheReadPrice != nil && !isFiniteNonNegative(*price.CacheReadPrice) {
		return errors.New("cache read price must be non-negative")
	}
	if price.CacheWritePrice != nil && !isFiniteNonNegative(*price.CacheWritePrice) {
		return errors.New("cache write price must be non-negative")
	}
	listPrices := []*float64{
		price.InputListPrice,
		price.OutputListPrice,
		price.CacheReadListPrice,
		price.CacheWriteListPrice,
	}
	for _, listPrice := range listPrices {
		if listPrice != nil && !isFiniteNonNegative(*listPrice) {
			return errors.New("provider list prices must be non-negative")
		}
	}
	if price.DiscountRate != nil &&
		(!isFiniteNonNegative(*price.DiscountRate) || *price.DiscountRate > 100) {
		return errors.New("discount rate must be between 0 and 100")
	}
	if price.DiscountRate != nil && *price.DiscountRate < 100 &&
		price.InputListPrice == nil && price.OutputListPrice == nil &&
		price.CacheReadListPrice == nil && price.CacheWriteListPrice == nil {
		return errors.New("discount rate requires at least one provider list price")
	}
	price.RecalculateEffectivePrices()
	return nil
}

func isFiniteNonNegative(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0
}

// RecalculateEffectivePrices refreshes display prices from the configured
// provider list prices and discount rate. Legacy rows without list prices
// retain their stored effective prices.
func (price *ModelProviderPrice) RecalculateEffectivePrices() {
	rate := 100.0
	if price.DiscountRate != nil {
		rate = *price.DiscountRate
	}
	factor := rate / 100
	if price.InputListPrice != nil {
		price.InputPrice = *price.InputListPrice * factor
	}
	if price.OutputListPrice != nil {
		price.OutputPrice = *price.OutputListPrice * factor
	}
	if price.CacheReadListPrice != nil {
		value := *price.CacheReadListPrice * factor
		price.CacheReadPrice = &value
	}
	if price.CacheWriteListPrice != nil {
		value := *price.CacheWriteListPrice * factor
		price.CacheWritePrice = &value
	}
}

func (price *ModelProviderPrice) Create() error {
	if err := price.normalize(); err != nil {
		return err
	}
	if err := price.validateReferences(); err != nil {
		return err
	}
	now := common.GetTimestamp()
	price.CreatedTime = now
	price.UpdatedTime = now
	return DB.Create(price).Error
}

func (price *ModelProviderPrice) Update() error {
	if price.Id <= 0 {
		return errors.New("model provider price id is required")
	}
	if err := price.normalize(); err != nil {
		return err
	}
	if err := price.validateReferences(); err != nil {
		return err
	}
	var existing ModelProviderPrice
	if err := DB.Where("id = ?", price.Id).First(&existing).Error; err != nil {
		return errors.New("model provider price not found")
	}
	price.UpdatedTime = common.GetTimestamp()
	return DB.Model(&ModelProviderPrice{}).Where("id = ?", price.Id).Updates(map[string]any{
		"model_id":               price.ModelId,
		"provider_slug":          price.ProviderSlug,
		"input_price":            price.InputPrice,
		"output_price":           price.OutputPrice,
		"cache_read_price":       price.CacheReadPrice,
		"cache_write_price":      price.CacheWritePrice,
		"input_list_price":       price.InputListPrice,
		"output_list_price":      price.OutputListPrice,
		"cache_read_list_price":  price.CacheReadListPrice,
		"cache_write_list_price": price.CacheWriteListPrice,
		"discount_percent":       price.DiscountRate,
		"source_url":             price.SourceURL,
		"effective_at":           price.EffectiveAt,
		"model_name":             price.ModelName,
		"context_length":         price.ContextLength,
		"max_output_tokens":      price.MaxOutputTokens,
		"region":                 price.Region,
		"precision":              price.Precision,
		"quantization":           price.Quantization,
		"supported_parameters":   price.SupportedParameters,
		"stream_cancellation":    price.StreamCancellation,
		"free":                   price.Free,
		"batch":                  price.Batch,
		"updated_time":           price.UpdatedTime,
	}).Error
}

func (price *ModelProviderPrice) validateReferences() error {
	var modelCount int64
	if err := DB.Model(&Model{}).Where("id = ?", price.ModelId).Count(&modelCount).Error; err != nil {
		return err
	}
	if modelCount == 0 {
		return errors.New("model not found")
	}

	var providerCount int64
	if err := DB.Model(&Provider{}).Where("LOWER(slug) = ?", price.ProviderSlug).Count(&providerCount).Error; err != nil {
		return err
	}
	if providerCount == 0 {
		return errors.New("provider not found")
	}
	return nil
}

func DeleteModelProviderPrice(id int) error {
	if id <= 0 {
		return errors.New("model provider price id is required")
	}
	return DB.Delete(&ModelProviderPrice{}, id).Error
}

func ListModelProviderPrice(modelId int) ([]ModelProviderPrice, error) {
	var prices []ModelProviderPrice
	err := DB.Where("model_id = ?", modelId).Order("provider_slug ASC").Find(&prices).Error
	for i := range prices {
		prices[i].RecalculateEffectivePrices()
	}
	return prices, err
}

func GetModelProviderPriceMap(modelIds []int) (map[int]map[string]ModelProviderPrice, error) {
	result := make(map[int]map[string]ModelProviderPrice)
	if len(modelIds) == 0 {
		return result, nil
	}
	var prices []ModelProviderPrice
	if err := DB.Where("model_id IN ?", modelIds).Find(&prices).Error; err != nil {
		return nil, err
	}
	for _, price := range prices {
		price.RecalculateEffectivePrices()
		if result[price.ModelId] == nil {
			result[price.ModelId] = make(map[string]ModelProviderPrice)
		}
		result[price.ModelId][price.ProviderSlug] = price
	}
	return result, nil
}

// ListProviderModels returns only models with an explicit Model-Provider
// configuration. Channel abilities are used only to report runtime
// availability for those existing associations.
func ListProviderModels(providerSlug string) ([]ProviderModelRelation, error) {
	providerSlug = normalizeProviderRegistrySlug(providerSlug)
	if providerSlug == "" {
		return []ProviderModelRelation{}, errors.New("provider slug is required")
	}

	var models []Model
	if err := DB.Where("status = ?", 1).Order("id ASC").Find(&models).Error; err != nil {
		return nil, err
	}
	if len(models) == 0 {
		return []ProviderModelRelation{}, nil
	}

	modelIds := make([]int, 0, len(models))
	for _, model := range models {
		modelIds = append(modelIds, model.Id)
	}
	var prices []ModelProviderPrice
	if err := DB.Where("provider_slug = ? AND model_id IN ?", providerSlug, modelIds).
		Find(&prices).Error; err != nil {
		return nil, err
	}
	pricesByModel := make(map[int]ModelProviderPrice, len(prices))
	for _, price := range prices {
		price.RecalculateEffectivePrices()
		pricesByModel[price.ModelId] = price
	}

	type availableModel struct {
		ModelName    string
		ProviderSlug string
		ChannelType  int
	}
	var availableRows []availableModel
	if err := DB.Table("abilities").
		Select("abilities.model as model_name, channels.provider_slug, channels.type as channel_type").
		Joins("JOIN channels ON channels.id = abilities.channel_id").
		Where("abilities.enabled = ? AND channels.status = ?", true, common.ChannelStatusEnabled).
		Find(&availableRows).Error; err != nil {
		return nil, err
	}
	availableByName := make(map[string]bool, len(availableRows))
	for _, row := range availableRows {
		channel := &Channel{
			ProviderSlug: row.ProviderSlug,
			Type:         row.ChannelType,
		}
		if channel.GetProviderSlug() == providerSlug {
			availableByName[row.ModelName] = true
		}
	}
	providerEnabled := false
	if provider, ok := GetProviderMetadataMap()[providerSlug]; ok {
		providerEnabled = provider.Status == 1
	}

	vendorIds := make([]int, 0, len(models))
	seenVendorIds := make(map[int]struct{})
	for _, model := range models {
		if model.VendorID == 0 {
			continue
		}
		if _, exists := seenVendorIds[model.VendorID]; exists {
			continue
		}
		seenVendorIds[model.VendorID] = struct{}{}
		vendorIds = append(vendorIds, model.VendorID)
	}
	var vendors []Vendor
	if len(vendorIds) > 0 {
		if err := DB.Where("id IN ?", vendorIds).Find(&vendors).Error; err != nil {
			return nil, err
		}
	}
	vendorById := make(map[int]Vendor, len(vendors))
	for _, vendor := range vendors {
		vendorById[vendor.Id] = vendor
	}

	result := make([]ProviderModelRelation, 0, len(prices))
	for _, model := range models {
		price, configured := pricesByModel[model.Id]
		available := providerEnabled && availableByName[model.ModelName]
		if !configured {
			continue
		}
		relation := ProviderModelRelation{
			ModelId:    model.Id,
			ModelName:  model.ModelName,
			ModelIcon:  model.Icon,
			Configured: configured,
			Available:  available,
		}
		if vendor, ok := vendorById[model.VendorID]; ok {
			relation.VendorName = vendor.Name
			relation.VendorIcon = vendor.Icon
		}
		if configured {
			relation.Price = &price
		}
		result = append(result, relation)
	}
	return result, nil
}
