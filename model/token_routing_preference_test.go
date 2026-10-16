package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTokenRoutingPreferenceRoundTripThroughRedisHashCache(t *testing.T) {
	useUserCacheMiniRedis(t)
	token := Token{Id: 43, UserId: 7, Key: "token-routing-cache-key", Name: "routing-cache"}
	require.NoError(t, token.SetRoutingPreference(&dto.RoutingPreference{Mode: "price"}))

	require.NoError(t, cacheSetTokenForTest(token))
	cached, err := cacheGetTokenByKey(token.Key)
	require.NoError(t, err)
	preference, err := cached.GetRoutingPreference()
	require.NoError(t, err)
	require.NotNil(t, preference)
	assert.Equal(t, "price", preference.Mode)
}

func TestTokenRoutingPreferenceIsIsolatedFromRegularUpdate(t *testing.T) {
	truncateTables(t)
	useUserCacheMiniRedis(t)
	token := Token{
		UserId:         7,
		Key:            "token-routing-update-key",
		Name:           "routing-update",
		Status:         common.TokenStatusEnabled,
		ExpiredTime:    -1,
		UnlimitedQuota: true,
	}
	require.NoError(t, token.Insert())

	require.NoError(t, token.SetRoutingPreference(&dto.RoutingPreference{Mode: "latency"}))
	require.NoError(t, token.UpdateRoutingPreference())

	// 普通编辑（字段白名单不含 routing_preference）不能覆盖路由倾向。
	edited := token
	edited.RoutingPreference = ""
	edited.Name = "renamed"
	require.NoError(t, edited.Update())

	reloaded, err := GetTokenByKey(token.Key, true)
	require.NoError(t, err)
	assert.Equal(t, "renamed", reloaded.Name)
	preference, err := reloaded.GetRoutingPreference()
	require.NoError(t, err)
	require.NotNil(t, preference)
	assert.Equal(t, "latency", preference.Mode)

	// 专用更新允许清空。
	require.NoError(t, reloaded.SetRoutingPreference(nil))
	require.NoError(t, reloaded.UpdateRoutingPreference())
	cleared, err := GetTokenByKey(token.Key, true)
	require.NoError(t, err)
	assert.Empty(t, cleared.RoutingPreference)
}
