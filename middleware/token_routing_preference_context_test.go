package middleware

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetupContextForTokenInjectsRoutingPreference(t *testing.T) {
	ctx := newTokenAutoGroupsContext()
	// 旧版本写入的约束字段（max_attempts 等）已不再生效，解析时被忽略。
	token := &model.Token{Id: 1, UserId: 2, RoutingPreference: `{"mode":"latency","max_attempts":2}`}

	require.NoError(t, SetupContextForToken(ctx, token))
	preference, ok := common.GetContextKeyType[*dto.RoutingPreference](ctx, constant.ContextKeyTokenRoutingPreference)
	require.True(t, ok)
	assert.Equal(t, "latency", preference.Mode)
	assert.Equal(t, dto.RoutingPreference{Mode: "latency"}, *preference)
}

func TestSetupContextForTokenWithoutRoutingPreferenceInheritsDefault(t *testing.T) {
	ctx := newTokenAutoGroupsContext()
	require.NoError(t, SetupContextForToken(ctx, &model.Token{Id: 1, UserId: 2}))
	_, ok := common.GetContextKey(ctx, constant.ContextKeyTokenRoutingPreference)
	assert.False(t, ok)
}

func TestSetupContextForTokenMalformedRoutingPreferenceFallsBackToDefault(t *testing.T) {
	ctx := newTokenAutoGroupsContext()
	token := &model.Token{Id: 1, UserId: 2, RoutingPreference: `not-json`}

	require.NoError(t, SetupContextForToken(ctx, token))
	_, ok := common.GetContextKey(ctx, constant.ContextKeyTokenRoutingPreference)
	assert.False(t, ok)
}
