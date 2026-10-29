package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func setupResourcePoolTestDB(t *testing.T) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		NamingStrategy: schema.NamingStrategy{TablePrefix: "rp_test_"},
	})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&ResourcePoolSource{}))

	previous := DB
	DB = db
	t.Cleanup(func() {
		DB = previous
		assert.NoError(t, sqlDB.Close())
	})
}

func TestSaveResourcePoolSourcesInsertUpdateDelete(t *testing.T) {
	setupResourcePoolTestDB(t)

	// Insert two sources.
	err := SaveResourcePoolSources(ResourcePoolClaude, []*ResourcePoolSource{
		{Name: "ch-1", Endpoint: "https://one.example.com/usage", AdminPassword: "p1", Enabled: true},
		{Name: "ch-2", Endpoint: "https://two.example.com/usage", AdminPassword: "p2", Enabled: true},
	})
	require.NoError(t, err)

	sources, err := GetResourcePoolSources(ResourcePoolClaude)
	require.NoError(t, err)
	require.Len(t, sources, 2)
	firstId := sources[0].Id
	secondId := sources[1].Id
	assert.Positive(t, firstId)
	assert.Equal(t, ResourcePoolClaude, sources[0].Provider)

	// Re-save keeping only the first row, renamed, with a rotated password.
	err = SaveResourcePoolSources(ResourcePoolClaude, []*ResourcePoolSource{
		{Id: firstId, Name: "ch-1-renamed", Endpoint: "https://one.example.com/usage", AdminPassword: "rotated", Enabled: false},
	})
	require.NoError(t, err)

	sources, err = GetResourcePoolSources(ResourcePoolClaude)
	require.NoError(t, err)
	require.Len(t, sources, 1)
	assert.Equal(t, firstId, sources[0].Id, "id survives a re-save")
	assert.Equal(t, "ch-1-renamed", sources[0].Name)
	assert.Equal(t, "rotated", sources[0].AdminPassword)
	assert.False(t, sources[0].Enabled)
	assert.NotEqual(t, secondId, sources[0].Id)
}

func TestSaveResourcePoolSourcesScopesDeletionByProvider(t *testing.T) {
	setupResourcePoolTestDB(t)

	require.NoError(t, SaveResourcePoolSources(ResourcePoolClaude, []*ResourcePoolSource{
		{Name: "claude-1", Endpoint: "https://c.example.com", Enabled: true},
	}))
	require.NoError(t, SaveResourcePoolSources(ResourcePoolGPT, []*ResourcePoolSource{
		{Name: "gpt-1", Endpoint: "https://g.example.com", Enabled: true},
	}))

	// Saving an empty claude list must not touch the gpt rows.
	require.NoError(t, SaveResourcePoolSources(ResourcePoolClaude, nil))

	claude, err := GetResourcePoolSources(ResourcePoolClaude)
	require.NoError(t, err)
	assert.Empty(t, claude)

	gpt, err := GetResourcePoolSources(ResourcePoolGPT)
	require.NoError(t, err)
	assert.Len(t, gpt, 1)
}

func TestGetResourcePoolSourcesAllProviders(t *testing.T) {
	setupResourcePoolTestDB(t)

	require.NoError(t, SaveResourcePoolSources(ResourcePoolClaude, []*ResourcePoolSource{
		{Name: "c", Endpoint: "https://c.example.com", Enabled: true},
	}))
	require.NoError(t, SaveResourcePoolSources(ResourcePoolGemini, []*ResourcePoolSource{
		{Name: "g", Endpoint: "https://g.example.com", Enabled: true},
	}))

	all, err := GetResourcePoolSources("")
	require.NoError(t, err)
	assert.Len(t, all, 2)
}

func TestDeleteResourcePoolSource(t *testing.T) {
	setupResourcePoolTestDB(t)

	require.NoError(t, SaveResourcePoolSources(ResourcePoolGrok, []*ResourcePoolSource{
		{Name: "x", Endpoint: "https://x.example.com", Enabled: true},
	}))
	sources, err := GetResourcePoolSources(ResourcePoolGrok)
	require.NoError(t, err)
	require.Len(t, sources, 1)

	require.NoError(t, DeleteResourcePoolSource(sources[0].Id))
	sources, err = GetResourcePoolSources(ResourcePoolGrok)
	require.NoError(t, err)
	assert.Empty(t, sources)
}

func TestUpdateResourcePoolSourceSnapshot(t *testing.T) {
	setupResourcePoolTestDB(t)

	require.NoError(t, SaveResourcePoolSources(ResourcePoolClaude, []*ResourcePoolSource{
		{Name: "snap", Endpoint: "https://s.example.com", Enabled: true},
	}))
	sources, err := GetResourcePoolSources(ResourcePoolClaude)
	require.NoError(t, err)
	require.Len(t, sources, 1)

	accounts := []*ResourcePoolAccount{{Id: "a1", Email: "a1@example.com", UsageCurrent: 10, UsageLimit: 100}}
	raw, err := common.Marshal(accounts)
	require.NoError(t, err)
	require.NoError(t, UpdateResourcePoolSourceSnapshot(sources[0].Id, string(raw), 12345))

	reloaded, err := GetResourcePoolSourceById(sources[0].Id)
	require.NoError(t, err)
	require.NotNil(t, reloaded)
	assert.Equal(t, int64(12345), reloaded.SnapshotAt)
	decoded := reloaded.SnapshotAccounts()
	require.Len(t, decoded, 1)
	assert.Equal(t, "a1@example.com", decoded[0].Email)
}

func TestGetResourcePoolSourceByIdMissing(t *testing.T) {
	setupResourcePoolTestDB(t)
	source, err := GetResourcePoolSourceById(999)
	require.NoError(t, err)
	assert.Nil(t, source)
}

func TestMaskAdminPassword(t *testing.T) {
	assert.Equal(t, "", MaskAdminPassword(""))
	assert.Equal(t, "****", MaskAdminPassword("abc"))
	assert.Equal(t, "****wxyz", MaskAdminPassword("abcdefghwxyz"))
	// A masked value must still be recognisable as masked by the controller.
	assert.True(t, len(MaskAdminPassword("supersecret")) > 4)
	assert.Equal(t, "****", MaskAdminPassword("supersecret")[:4])
}

func TestResourcePoolAccountParsesAntigravityQuotaBuckets(t *testing.T) {
	payload := []byte(`{
		"id": 28, "status": "active", "enabled": true, "plan_type": "Google AI Ultra",
		"usage_percent_7d": null,
		"antigravity_quota": {"quota_groups": [
			{"buckets": [
				{"bucket_id": "gemini-weekly", "window": "weekly", "remaining_fraction": 0.25, "fraction_present": true, "reset_time": "2026-10-30T23:08:03Z"},
				{"bucket_id": "gemini-5h", "window": "5h", "remaining_fraction": 0.9, "fraction_present": true, "reset_time": "2026-10-28T11:54:29Z"}
			]},
			{"buckets": [
				{"bucket_id": "3p-weekly", "window": "weekly", "remaining_fraction": 0, "fraction_present": true, "reset_time": "2026-11-04T07:46:50Z"}
			]}
		]}
	}`)

	var account ResourcePoolAccount
	require.NoError(t, common.Unmarshal(payload, &account))

	require.NotNil(t, account.UsagePercent7d)
	// The Gemini bucket wins over the more-used third-party bucket.
	assert.InDelta(t, 75.0, *account.UsagePercent7d, 0.001)
	assert.Equal(t, "2026-10-30T23:08:03Z", account.Reset7dAt)
	require.NotNil(t, account.UsagePercent5h)
	assert.InDelta(t, 10.0, *account.UsagePercent5h, 0.001)
	assert.Equal(t, "2026-10-28T11:54:29Z", account.Reset5hAt)
}

func TestResourcePoolAccountIgnoresAbsentAntigravityFraction(t *testing.T) {
	payload := []byte(`{"id": 1, "antigravity_quota": {"quota_groups": [{"buckets": [
		{"bucket_id": "gemini-weekly", "window": "weekly", "remaining_fraction": null, "fraction_present": false}
	]}]}}`)

	var account ResourcePoolAccount
	require.NoError(t, common.Unmarshal(payload, &account))

	assert.Nil(t, account.UsagePercent7d)
	assert.Empty(t, account.Reset7dAt)
}

func TestResourcePoolAccountSnapshotRoundTripKeepsAntigravityUsage(t *testing.T) {
	var account ResourcePoolAccount
	require.NoError(t, common.Unmarshal([]byte(`{"id": 2, "antigravity_quota": {"quota_groups": [{"buckets": [
		{"bucket_id": "gemini-weekly", "window": "weekly", "remaining_fraction": 0, "fraction_present": true, "reset_time": "2026-10-31T03:48:07Z"}
	]}]}}`), &account))

	snapshot, err := common.Marshal(&account)
	require.NoError(t, err)
	var restored ResourcePoolAccount
	require.NoError(t, common.Unmarshal(snapshot, &restored))

	require.NotNil(t, restored.UsagePercent7d)
	assert.InDelta(t, 100.0, *restored.UsagePercent7d, 0.001)
	assert.Equal(t, "2026-10-31T03:48:07Z", restored.Reset7dAt)
}
