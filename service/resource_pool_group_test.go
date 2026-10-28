package service

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// One Kiro (tokenmanagement) source mixes accounts of several groups; each
// account must keep its own group instead of the source's configured one.
func TestFetchResourcePoolUsageKeepsPerAccountGroup(t *testing.T) {
	require.NoError(t, model.DB.AutoMigrate(&model.ResourcePoolSource{}))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"accounts":[
			{"id":"1","email":"a@example.com","enabled":true,"groups":["aws1"],"sourceChannel":"aws1","usageCurrent":1,"usageLimit":10},
			{"id":"2","email":"b@example.com","enabled":true,"groups":[],"sourceChannel":"aws3","usageCurrent":1,"usageLimit":10},
			{"id":"3","email":"c@example.com","enabled":true,"usageCurrent":1,"usageLimit":10}
		]}`))
	}))
	t.Cleanup(server.Close)

	provider := model.ResourcePoolClaude
	require.NoError(t, model.SaveResourcePoolSources(provider, []*model.ResourcePoolSource{
		{Name: "kiro", Endpoint: server.URL, GroupName: "aws2", Enabled: true},
	}))
	t.Cleanup(func() {
		require.NoError(t, model.SaveResourcePoolSources(provider, nil))
		InvalidateResourcePoolUsage(provider)
	})

	result, err := FetchResourcePoolUsage(provider)
	require.NoError(t, err)
	require.Len(t, result.Accounts, 3)

	groups := map[string]string{}
	for _, account := range result.Accounts {
		groups[account.Id] = account.SourceGroup
	}
	assert.Equal(t, map[string]string{"1": "aws1", "2": "aws3", "3": "aws2"}, groups)
}
