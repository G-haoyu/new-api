package service

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedResourcePoolProvider points one provider at a counting upstream.
func seedResourcePoolProvider(t *testing.T, provider string) *atomic.Int32 {
	t.Helper()
	require.NoError(t, model.DB.AutoMigrate(&model.ResourcePoolSource{}))

	hits := &atomic.Int32{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte(`{"accounts":[{"id":"a1","email":"a1@example.com","enabled":true,"usage_current":1,"usage_limit":4}]}`))
	}))
	t.Cleanup(server.Close)

	require.NoError(t, model.SaveResourcePoolSources(provider, []*model.ResourcePoolSource{
		{Name: "upstream", Endpoint: server.URL, Enabled: true},
	}))
	InvalidateResourcePoolUsage(provider)
	t.Cleanup(func() {
		require.NoError(t, model.SaveResourcePoolSources(provider, nil))
		InvalidateResourcePoolUsage(provider)
	})
	return hits
}

func TestGetResourcePoolUsageServesFreshCache(t *testing.T) {
	hits := seedResourcePoolProvider(t, model.ResourcePoolGPT)

	first, err := GetResourcePoolUsage(model.ResourcePoolGPT, false)
	require.NoError(t, err)
	second, err := GetResourcePoolUsage(model.ResourcePoolGPT, false)
	require.NoError(t, err)

	assert.Equal(t, int32(1), hits.Load())
	assert.Same(t, first, second)
	require.Len(t, second.Accounts, 1)
	assert.Positive(t, second.RefreshedAt)
}

func TestGetResourcePoolUsageForceRefreshPollsUpstream(t *testing.T) {
	hits := seedResourcePoolProvider(t, model.ResourcePoolGrok)

	_, err := GetResourcePoolUsage(model.ResourcePoolGrok, false)
	require.NoError(t, err)
	_, err = GetResourcePoolUsage(model.ResourcePoolGrok, true)
	require.NoError(t, err)

	assert.Equal(t, int32(2), hits.Load())
}

func TestInvalidateResourcePoolUsageDropsCachedPoll(t *testing.T) {
	hits := seedResourcePoolProvider(t, model.ResourcePoolGemini)

	_, err := GetResourcePoolUsage(model.ResourcePoolGemini, false)
	require.NoError(t, err)
	InvalidateResourcePoolUsage(model.ResourcePoolGemini)
	_, err = GetResourcePoolUsage(model.ResourcePoolGemini, false)
	require.NoError(t, err)

	assert.Equal(t, int32(2), hits.Load())
}

func TestGetResourcePoolUsageServesStaleAndRefreshesInBackground(t *testing.T) {
	hits := seedResourcePoolProvider(t, model.ResourcePoolClaude)

	first, err := GetResourcePoolUsage(model.ResourcePoolClaude, false)
	require.NoError(t, err)

	// Age the cached poll past the fresh window but within the stale window.
	resourcePoolCache.Lock()
	entry := resourcePoolCache.entries[model.ResourcePoolClaude]
	entry.fetchedAt = time.Now().Add(-resourcePoolFreshTTL - time.Second)
	resourcePoolCache.entries[model.ResourcePoolClaude] = entry
	resourcePoolCache.Unlock()

	stale, err := GetResourcePoolUsage(model.ResourcePoolClaude, false)
	require.NoError(t, err)
	assert.Same(t, first, stale)

	assert.Eventually(t, func() bool { return hits.Load() == 2 }, 2*time.Second, 10*time.Millisecond)
	assert.Eventually(t, func() bool {
		latest, err := GetResourcePoolUsage(model.ResourcePoolClaude, false)
		return err == nil && latest != first
	}, 2*time.Second, 10*time.Millisecond)
}

func TestGetResourcePoolUsageCoalescesConcurrentPolls(t *testing.T) {
	hits := seedResourcePoolProvider(t, model.ResourcePoolGPT)

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := GetResourcePoolUsage(model.ResourcePoolGPT, false)
			assert.NoError(t, err)
		}()
	}
	wg.Wait()

	// Concurrent cold requests share in-flight polls instead of each hitting the upstream.
	assert.LessOrEqual(t, hits.Load(), int32(2))
}
