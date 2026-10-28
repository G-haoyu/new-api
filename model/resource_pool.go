package model

import (
	"encoding/json"
	"errors"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

// ResourcePoolProvider identifies the vendor a resource pool source belongs to.
const (
	ResourcePoolClaude = "claude"
	ResourcePoolGPT    = "gpt"
	ResourcePoolGemini = "gemini"
	ResourcePoolGrok   = "grok"
)

// ResourcePoolProviders lists every supported provider in console order.
var ResourcePoolProviders = []string{ResourcePoolClaude, ResourcePoolGPT, ResourcePoolGemini, ResourcePoolGrok}

// ResourcePoolSource is an upstream admin endpoint polled for per-account quota
// usage. One row describes one channel's admin API, for example
// https://tokenmanagement21.intsig.net/api/admin/accounts/usage.
type ResourcePoolSource struct {
	Id            int64  `json:"id" gorm:"primaryKey;autoIncrement"`
	Provider      string `json:"provider" gorm:"type:varchar(32);index;not null"`
	Name          string `json:"name" gorm:"type:varchar(255);not null"`
	Endpoint      string `json:"endpoint" gorm:"type:varchar(512);not null"`
	AdminPassword string `json:"admin_password" gorm:"type:varchar(512)"`
	GroupName     string `json:"group_name" gorm:"type:varchar(64)"`
	Enabled       bool   `json:"enabled" gorm:"default:true"`
	UsageSnapshot string `json:"-" gorm:"type:text"`
	SnapshotAt    int64  `json:"snapshot_at" gorm:"bigint;default:0"`
	CreatedTime   int64  `json:"created_time" gorm:"bigint"`
	UpdatedTime   int64  `json:"updated_time" gorm:"bigint"`
}

func (ResourcePoolSource) TableName() string {
	return "resource_pool_sources"
}

// ResourcePoolAccount is one upstream account parsed from a source snapshot.
// GroupName / RpMLimit / RpmCurrent are filled by the upstream admin API.
// The 7d-window fields come from codex2api-style upstreams (Grok pool).
type ResourcePoolAccount struct {
	Id                string   `json:"id"`
	Email             string   `json:"email"`
	UserId            string   `json:"user_id"`
	Name              string   `json:"name"`
	Enabled           bool     `json:"enabled"`
	SubscriptionType  string   `json:"subscription_type"`
	SubscriptionTitle string   `json:"subscription_title"`
	UsageCurrent      float64  `json:"usage_current"`
	UsageLimit        float64  `json:"usage_limit"`
	UsagePercent      float64  `json:"usage_percent"`
	UsagePercentage   float64  `json:"usage_percentage"`
	NextResetDate     string   `json:"next_reset_date"`
	Groups            []string `json:"groups"`
	SourceGroup       string   `json:"source_group"`
	SourceChannel     string   `json:"source_channel"`
	RpMLimit          int64    `json:"rpm_limit"`
	RpmCurrent        int64    `json:"rpm_current"`
	LastRefresh       int64    `json:"last_refresh"`

	// codex2api (Grok) fields — snake_case payload, matched by the alias path.
	PlanType        string   `json:"plan_type"`
	PlanDisplay     string   `json:"plan_display"`
	Status          string   `json:"status"`
	Models          []string `json:"models"`
	TotalRequests   int64    `json:"total_requests"`
	UsagePercent7d  *float64 `json:"usage_percent_7d"`
	UsagePercent5h  *float64 `json:"usage_percent_5h"`
	WeeklyPercent   *float64 `json:"weekly_percent"`
	Requests7d      int64    `json:"requests_7d"`
	Tokens7d        int64    `json:"tokens_7d"`
	AccountBilled7d float64  `json:"account_billed_7d"`
	Reset7dAt       string   `json:"reset_7d_at"`
	Reset5hAt       string   `json:"reset_5h_at"`

	SourceId       int64  `json:"source_id"`
	SourceName     string `json:"source_name"`
	SourceEndpoint string `json:"source_endpoint"`
}

// UnmarshalJSON accepts both the camelCase shape returned by the upstream
// admin APIs (usageCurrent, usageLimit, ...) and the snake_case shape this
// service uses internally for snapshots and console responses. It also
// flattens the codex2api usage_7d_detail envelope into the 7d fields.
func (a *ResourcePoolAccount) UnmarshalJSON(data []byte) error {
	type alias ResourcePoolAccount
	aux := struct {
		alias
		UserIDC            string   `json:"userId"`
		SubscriptionTypeC  string   `json:"subscriptionType"`
		SubscriptionTitleC string   `json:"subscriptionTitle"`
		UsageCurrentC      *float64 `json:"usageCurrent"`
		UsageLimitC        *float64 `json:"usageLimit"`
		UsagePercentC      *float64 `json:"usagePercent"`
		UsagePercentageC   *float64 `json:"usagePercentage"`
		NextResetDateC     string   `json:"nextResetDate"`
		SourceChannelC     string   `json:"sourceChannel"`
		RpMLimitC          *int64   `json:"rpmLimit"`
		RpmCurrentC        *int64   `json:"rpmCurrent"`
		LastRefreshC       *int64   `json:"lastRefresh"`
		// codex2api nested windows
		Usage7dDetail *struct {
			Requests      int64   `json:"requests"`
			Tokens        int64   `json:"tokens"`
			AccountBilled float64 `json:"account_billed"`
			UserBilled    float64 `json:"user_billed"`
		} `json:"usage_7d_detail"`
		Billed7d *float64 `json:"billed_7d"`
		// codex2api grok envelopes
		GrokBilling *struct {
			WeeklyPercent     *float64 `json:"weekly_percent"`
			WeeklyPeriodEnd   string   `json:"weekly_period_end"`
			WeeklyPeriodStart string   `json:"weekly_period_start"`
		} `json:"grok_billing"`
		GrokPlanDisplay *struct {
			Plan string `json:"plan"`
		} `json:"grok_plan_display"`
		// codex2api antigravity (Gemini pool) quota buckets
		AntigravityQuota *struct {
			QuotaGroups []struct {
				Buckets []antigravityQuotaBucket `json:"buckets"`
			} `json:"quota_groups"`
		} `json:"antigravity_quota"`
		// Shadows the alias Id so numeric codex2api ids land here instead of
		// failing the string-typed field.
		RawId json.RawMessage `json:"id"`
	}{}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	*a = ResourcePoolAccount(aux.alias)
	// id arrives as a JSON string on the claude pool and as a number on
	// codex2api; RawId shadows the alias field so both survive.
	if len(aux.RawId) > 0 {
		if aux.RawId[0] == '"' {
			_ = json.Unmarshal(aux.RawId, &a.Id)
		} else {
			a.Id = string(aux.RawId)
		}
	}
	if aux.UserIDC != "" {
		a.UserId = aux.UserIDC
	}
	if aux.SubscriptionTypeC != "" {
		a.SubscriptionType = aux.SubscriptionTypeC
	}
	if aux.SubscriptionTitleC != "" {
		a.SubscriptionTitle = aux.SubscriptionTitleC
	}
	if aux.UsageCurrentC != nil {
		a.UsageCurrent = *aux.UsageCurrentC
	}
	if aux.UsageLimitC != nil {
		a.UsageLimit = *aux.UsageLimitC
	}
	if aux.UsagePercentC != nil {
		a.UsagePercent = *aux.UsagePercentC
	}
	if aux.UsagePercentageC != nil {
		a.UsagePercentage = *aux.UsagePercentageC
	}
	if aux.NextResetDateC != "" {
		a.NextResetDate = aux.NextResetDateC
	}
	if aux.SourceChannelC != "" {
		a.SourceChannel = aux.SourceChannelC
	}
	if aux.RpMLimitC != nil {
		a.RpMLimit = *aux.RpMLimitC
	}
	if aux.RpmCurrentC != nil {
		a.RpmCurrent = *aux.RpmCurrentC
	}
	if aux.LastRefreshC != nil {
		a.LastRefresh = *aux.LastRefreshC
	}
	if aux.Usage7dDetail != nil {
		a.Requests7d = aux.Usage7dDetail.Requests
		a.Tokens7d = aux.Usage7dDetail.Tokens
		a.AccountBilled7d = aux.Usage7dDetail.AccountBilled
	}
	if aux.Billed7d != nil {
		a.AccountBilled7d = *aux.Billed7d
	}
	if aux.GrokBilling != nil {
		if aux.GrokBilling.WeeklyPercent != nil {
			a.WeeklyPercent = aux.GrokBilling.WeeklyPercent
		}
		if a.Reset7dAt == "" && aux.GrokBilling.WeeklyPeriodEnd != "" {
			a.Reset7dAt = aux.GrokBilling.WeeklyPeriodEnd
		}
	}
	if aux.GrokPlanDisplay != nil && aux.GrokPlanDisplay.Plan != "" {
		a.PlanDisplay = aux.GrokPlanDisplay.Plan
	}
	if aux.AntigravityQuota != nil {
		var buckets []antigravityQuotaBucket
		for _, group := range aux.AntigravityQuota.QuotaGroups {
			buckets = append(buckets, group.Buckets...)
		}
		if weekly := pickAntigravityBucket(buckets, "weekly", "gemini-weekly"); weekly != nil && a.UsagePercent7d == nil {
			used := (1 - min(1, max(0, *weekly.RemainingFraction))) * 100
			a.UsagePercent7d = &used
			if a.Reset7dAt == "" {
				a.Reset7dAt = weekly.ResetTime
			}
		}
		if fiveHour := pickAntigravityBucket(buckets, "5h", "gemini-5h"); fiveHour != nil && a.UsagePercent5h == nil {
			used := (1 - min(1, max(0, *fiveHour.RemainingFraction))) * 100
			a.UsagePercent5h = &used
			if a.Reset5hAt == "" {
				a.Reset5hAt = fiveHour.ResetTime
			}
		}
	}
	return nil
}

// antigravityQuotaBucket is one usage window codex2api reports for an
// antigravity (Gemini) account, e.g. {"bucket_id":"gemini-weekly","window":"weekly"}.
type antigravityQuotaBucket struct {
	BucketId          string   `json:"bucket_id"`
	Window            string   `json:"window"`
	RemainingFraction *float64 `json:"remaining_fraction"`
	FractionPresent   bool     `json:"fraction_present"`
	ResetTime         string   `json:"reset_time"`
}

// pickAntigravityBucket returns the Gemini-model bucket of one window. The pool
// serves Gemini models, so the third-party (Claude/GPT) buckets only count when
// no Gemini bucket is reported; then the most-used bucket of the window wins.
func pickAntigravityBucket(buckets []antigravityQuotaBucket, window, preferredId string) *antigravityQuotaBucket {
	var picked *antigravityQuotaBucket
	for i := range buckets {
		bucket := &buckets[i]
		if bucket.Window != window || !bucket.FractionPresent || bucket.RemainingFraction == nil {
			continue
		}
		if bucket.BucketId == preferredId {
			return bucket
		}
		if picked == nil || *bucket.RemainingFraction < *picked.RemainingFraction {
			picked = bucket
		}
	}
	return picked
}

// ResourcePoolSourceStatus reports whether one source poll succeeded.
type ResourcePoolSourceStatus struct {
	SourceId       int64  `json:"source_id"`
	SourceName     string `json:"source_name"`
	SourceEndpoint string `json:"source_endpoint"`
	Status         string `json:"status"`
	Count          int    `json:"count"`
	Error          string `json:"error"`
}

// GetResourcePoolSources lists configured sources for one provider. An empty
// provider returns every provider's sources.
func GetResourcePoolSources(provider string) ([]*ResourcePoolSource, error) {
	query := DB.Model(&ResourcePoolSource{})
	if provider != "" {
		query = query.Where("provider = ?", provider)
	}
	var sources []*ResourcePoolSource
	err := query.Order("provider asc, id asc").Find(&sources).Error
	return sources, err
}

// GetResourcePoolSourceById loads one source row.
func GetResourcePoolSourceById(id int64) (*ResourcePoolSource, error) {
	var source ResourcePoolSource
	err := DB.Where("id = ?", id).First(&source).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &source, nil
}

// SaveResourcePoolSources replaces the source list of one provider. Rows keep
// their id when the client sends one back, so snapshots survive a re-save.
func SaveResourcePoolSources(provider string, sources []*ResourcePoolSource) error {
	now := common.GetTimestamp()
	keepIds := make([]int64, 0, len(sources))

	return DB.Transaction(func(tx *gorm.DB) error {
		for _, source := range sources {
			source.Provider = provider
			source.UpdatedTime = now
			if source.Name == "" {
				source.Name = "未命名渠道"
			}
			if source.Id > 0 {
				keepIds = append(keepIds, source.Id)
				updates := map[string]any{
					"name":           source.Name,
					"endpoint":       source.Endpoint,
					"admin_password": source.AdminPassword,
					"group_name":     source.GroupName,
					"enabled":        source.Enabled,
					"updated_time":   now,
				}
				if err := tx.Model(&ResourcePoolSource{}).
					Where("id = ? AND provider = ?", source.Id, provider).
					Updates(updates).Error; err != nil {
					return err
				}
				continue
			}
			source.CreatedTime = now
			if err := tx.Create(source).Error; err != nil {
				return err
			}
			keepIds = append(keepIds, source.Id)
		}

		query := tx.Where("provider = ?", provider)
		if len(keepIds) > 0 {
			query = query.Where("id NOT IN ?", keepIds)
		}
		return query.Delete(&ResourcePoolSource{}).Error
	})
}

// UpdateResourcePoolSourceSnapshot stores the latest fetched account list so a
// failed poll can still show the previous values.
func UpdateResourcePoolSourceSnapshot(id int64, snapshot string, at int64) error {
	return DB.Model(&ResourcePoolSource{}).
		Where("id = ?", id).
		Updates(map[string]any{"usage_snapshot": snapshot, "snapshot_at": at}).Error
}

// DeleteResourcePoolSource removes one configured source by id.
func DeleteResourcePoolSource(id int64) error {
	return DB.Where("id = ?", id).Delete(&ResourcePoolSource{}).Error
}

// SnapshotAccounts decodes the stored snapshot of one source.
func (s *ResourcePoolSource) SnapshotAccounts() []*ResourcePoolAccount {
	if s.UsageSnapshot == "" {
		return nil
	}
	var accounts []*ResourcePoolAccount
	if common.Unmarshal([]byte(s.UsageSnapshot), &accounts) != nil {
		return nil
	}
	return accounts
}

// MaskAdminPassword hides the stored secret except the last four characters.
func MaskAdminPassword(secret string) string {
	if secret == "" {
		return ""
	}
	if len(secret) <= 4 {
		return "****"
	}
	return "****" + secret[len(secret)-4:]
}
