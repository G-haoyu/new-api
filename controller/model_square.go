package controller

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/capability"

	"github.com/gin-gonic/gin"
)

type modelSquareItem struct {
	ID                     int                            `json:"id"`
	ModelName              string                         `json:"model_name"`
	CatalogSlug            string                         `json:"catalog_slug"`
	Description            string                         `json:"description,omitempty"`
	Icon                   string                         `json:"icon,omitempty"`
	Tags                   string                         `json:"tags,omitempty"`
	VendorID               int                            `json:"vendor_id,omitempty"`
	VendorName             string                         `json:"vendor_name,omitempty"`
	VendorIcon             string                         `json:"vendor_icon,omitempty"`
	VendorDescription      string                         `json:"vendor_description,omitempty"`
	ContextLength          int64                          `json:"context_length,omitempty"`
	ParameterCount         string                         `json:"parameter_count,omitempty"`
	ReleaseDate            string                         `json:"release_date,omitempty"`
	QuotaType              int                            `json:"quota_type"`
	ModelRatio             float64                        `json:"model_ratio"`
	ModelPrice             float64                        `json:"model_price"`
	CompletionRatio        float64                        `json:"completion_ratio"`
	CacheRatio             *float64                       `json:"cache_ratio,omitempty"`
	CreateCacheRatio       *float64                       `json:"create_cache_ratio,omitempty"`
	EnableGroup            []string                       `json:"enable_groups"`
	SupportedEndpointTypes []string                       `json:"supported_endpoint_types"`
	EndpointMap            map[string]common.EndpointInfo `json:"endpoint_map,omitempty"`
	BillingMode            string                         `json:"billing_mode,omitempty"`
	BillingExpr            string                         `json:"billing_expr,omitempty"`

	// 能力摘要来自模型级 capabilities 声明；未声明时全部为空。
	Capabilities     []string               `json:"capabilities,omitempty"`
	InputModalities  []string               `json:"input_modalities,omitempty"`
	OutputModalities []string               `json:"output_modalities,omitempty"`
	MaxOutputTokens  *int64                 `json:"max_output_tokens,omitempty"`
	Parameters       []capability.Parameter `json:"parameters,omitempty"`
}

func modelSquareItemFromModel(
	meta *model.Model,
	pricingByName map[string]model.Pricing,
	vendors map[int]model.Vendor,
) modelSquareItem {
	catalogSlug := meta.ModelName
	if vendor, ok := vendors[meta.VendorID]; ok && vendor.Name != "" {
		ownerSlug := strings.ToLower(strings.TrimSpace(vendor.Name))
		ownerSlug = strings.NewReplacer(" ", "-", "/", "-", "_", "-").Replace(ownerSlug)
		catalogSlug = ownerSlug + "/" + meta.ModelName
	}
	item := modelSquareItem{
		ID:                     meta.Id,
		ModelName:              meta.ModelName,
		CatalogSlug:            catalogSlug,
		Description:            meta.Description,
		Icon:                   meta.Icon,
		Tags:                   meta.Tags,
		VendorID:               meta.VendorID,
		ContextLength:          meta.ContextLength,
		ParameterCount:         meta.ParameterCount,
		ReleaseDate:            meta.ReleaseDate,
		EnableGroup:            []string{},
		SupportedEndpointTypes: []string{},
	}
	if vendor, ok := vendors[meta.VendorID]; ok {
		item.VendorName = vendor.Name
		item.VendorIcon = vendor.Icon
		item.VendorDescription = vendor.Description
	}
	spec := capability.MustParse(meta.Capabilities)
	summary := capability.Summarize(spec)
	item.Capabilities = summary.Capabilities
	item.InputModalities = summary.InputModalities
	item.OutputModalities = summary.OutputModalities
	item.MaxOutputTokens = summary.MaxOutputTokens
	item.Parameters = capability.Parameters(spec)

	if pricing, ok := pricingByName[meta.ModelName]; ok {
		item.QuotaType = pricing.QuotaType
		item.ModelRatio = pricing.ModelRatio
		item.ModelPrice = pricing.ModelPrice
		item.CompletionRatio = pricing.CompletionRatio
		item.CacheRatio = pricing.CacheRatio
		item.CreateCacheRatio = pricing.CreateCacheRatio
		item.EnableGroup = append([]string{}, pricing.EnableGroup...)
		item.SupportedEndpointTypes = make([]string, 0, len(pricing.SupportedEndpointTypes))
		for _, endpoint := range pricing.SupportedEndpointTypes {
			item.SupportedEndpointTypes = append(item.SupportedEndpointTypes, string(endpoint))
		}
		item.BillingMode = pricing.BillingMode
		item.BillingExpr = pricing.BillingExpr
	}
	return item
}

func buildModelSquareItems(metas []model.Model) ([]modelSquareItem, error) {
	pricingByName := make(map[string]model.Pricing)
	for _, pricing := range model.GetPricing() {
		pricingByName[pricing.ModelName] = pricing
	}
	var vendorRows []model.Vendor
	if err := model.DB.Find(&vendorRows).Error; err != nil {
		return nil, err
	}
	vendors := make(map[int]model.Vendor, len(vendorRows))
	for _, vendor := range vendorRows {
		vendors[vendor.Id] = vendor
	}
	items := make([]modelSquareItem, 0, len(metas))
	for i := range metas {
		items = append(items, modelSquareItemFromModel(&metas[i], pricingByName, vendors))
	}
	return items, nil
}

func getModelSquareItems(offset, limit int) ([]modelSquareItem, int64, error) {
	pricingByName := make(map[string]model.Pricing)
	for _, pricing := range model.GetPricing() {
		pricingByName[pricing.ModelName] = pricing
	}
	var enabledAbilities []model.AbilityWithChannel
	if err := model.DB.Table("abilities").
		Select("abilities.*, channels.type as channel_type").
		Joins("left join channels on abilities.channel_id = channels.id").
		Where("abilities.enabled = ?", true).
		Scan(&enabledAbilities).Error; err != nil {
		return nil, 0, err
	}
	names := make([]string, 0, len(enabledAbilities))
	seen := make(map[string]struct{}, len(enabledAbilities))
	for _, ability := range enabledAbilities {
		if _, hasPricing := pricingByName[ability.Model]; !hasPricing {
			continue
		}
		if _, exists := seen[ability.Model]; exists {
			continue
		}
		seen[ability.Model] = struct{}{}
		names = append(names, ability.Model)
	}
	if len(names) == 0 {
		return []modelSquareItem{}, 0, nil
	}
	var metas []model.Model
	query := model.DB.Model(&model.Model{}).
		Where("status = ? AND name_rule = ? AND model_name IN ?", 1, model.NameRuleExact, names)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	query = query.Order("id ASC")
	if offset > 0 {
		query = query.Offset(offset)
	}
	if limit > 0 {
		query = query.Limit(limit)
	}
	if err := query.Find(&metas).Error; err != nil {
		return nil, 0, err
	}
	items, err := buildModelSquareItems(metas)
	return items, total, err
}

func getModelSquareItemByID(id int) (*modelSquareItem, error) {
	var meta model.Model
	if err := model.DB.Where("id = ? AND status = ? AND name_rule = ?", id, 1, model.NameRuleExact).First(&meta).Error; err != nil {
		return nil, err
	}
	items, err := buildModelSquareItems([]model.Model{meta})
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, errors.New("model not found")
	}
	items[0].EndpointMap = getModelSquareEndpointMap(items[0].SupportedEndpointTypes)
	return &items[0], nil
}

func getModelSquareEndpointMap(endpointTypes []string) map[string]common.EndpointInfo {
	if len(endpointTypes) == 0 {
		return nil
	}
	configured := model.GetSupportedEndpointMap()
	result := make(map[string]common.EndpointInfo, len(endpointTypes))
	for _, endpointType := range endpointTypes {
		if info, ok := configured[endpointType]; ok {
			result[endpointType] = info
			continue
		}
		if info, ok := common.GetDefaultEndpointInfo(constant.EndpointType(endpointType)); ok {
			result[endpointType] = info
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func parseModelSquarePagination(c *gin.Context) (int, int, error) {
	offset := 0
	limit := 24
	if rawOffset := c.Query("offset"); rawOffset != "" {
		parsed, err := strconv.Atoi(rawOffset)
		if err != nil || parsed < 0 {
			return 0, 0, errors.New("offset must be a non-negative integer")
		}
		offset = parsed
	}
	if rawLimit := c.Query("limit"); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil || parsed <= 0 {
			return 0, 0, errors.New("limit must be a positive integer")
		}
		if parsed > 100 {
			parsed = 100
		}
		limit = parsed
	}
	return offset, limit, nil
}

func GetModelSquare(c *gin.Context) {
	offset, limit, err := parseModelSquarePagination(c)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	items, total, err := getModelSquareItems(offset, limit)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{
		"items":  items,
		"total":  total,
		"offset": offset,
		"limit":  limit,
	}})
}

func GetModelSquareDetail(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	item, err := getModelSquareItemByID(id)
	if err != nil {
		common.ApiErrorMsg(c, "model not found")
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": item})
}
