package controller

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// TestFinishInputForCostMultiplierKeysByUpstreamModel pins the <channel, model>
// cost multiplier contract at the collection point: the multiplier is read at
// Finish time from the channel setting the distributor refreshed for this
// attempt, keyed by the upstream (model-mapped) model name rather than the
// client-facing one.
func TestFinishInputForCostMultiplierKeysByUpstreamModel(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name           string
		costMultiplier map[string]float64
		upstreamModel  string
		want           float64
	}{
		{
			name:           "keyed by mapped upstream model name",
			costMultiplier: map[string]float64{"gpt-4o-2024-08": 2.5, "gpt-4o": 9},
			upstreamModel:  "gpt-4o-2024-08",
			want:           2.5,
		},
		{
			name:           "client model name alone does not match when mapping renamed it",
			costMultiplier: map[string]float64{"gpt-4o": 9},
			upstreamModel:  "gpt-4o-2024-08",
			want:           1,
		},
		{
			name:           "unmapped model matches its own name",
			costMultiplier: map[string]float64{"gpt-4o": 1.5},
			upstreamModel:  "gpt-4o",
			want:           1.5,
		},
		{
			name:           "no channel setting in context defaults to one",
			upstreamModel:  "gpt-4o",
			want:           1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			if tt.costMultiplier != nil {
				common.SetContextKey(c, constant.ContextKeyChannelSetting, dto.ChannelSettings{
					CostMultiplierMap: tt.costMultiplier,
				})
			}
			info := &relaycommon.RelayInfo{
				ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: tt.upstreamModel},
			}

			in := finishInputFor(c, info, nil)

			assert.InDelta(t, tt.want, in.CostMultiplier, 0.000001)
			assert.Equal(t, tt.upstreamModel, in.UpstreamModelName)
		})
	}
}
