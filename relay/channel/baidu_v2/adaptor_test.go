package baidu_v2

import (
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetRequestURL(t *testing.T) {
	tests := []struct {
		name        string
		relayFormat types.RelayFormat
		relayMode   int
		wantURL     string
		wantErr     string
	}{
		{
			name:        "claude format with unknown mode",
			relayFormat: types.RelayFormatClaude,
			relayMode:   constant.RelayModeUnknown,
			wantURL:     "https://qianfan.baidubce.com/v2/chat/completions",
		},
		{
			name:        "gemini format with unknown mode",
			relayFormat: types.RelayFormatGemini,
			relayMode:   constant.RelayModeUnknown,
			wantURL:     "https://qianfan.baidubce.com/v2/chat/completions",
		},
		{
			name:        "claude format but responses mode",
			relayFormat: types.RelayFormatClaude,
			relayMode:   constant.RelayModeResponses,
			wantErr:     "unsupported relay mode",
		},
		{
			name:        "chat completions",
			relayFormat: types.RelayFormatOpenAI,
			relayMode:   constant.RelayModeChatCompletions,
			wantURL:     "https://qianfan.baidubce.com/v2/chat/completions",
		},
		{
			name:        "embeddings",
			relayFormat: types.RelayFormatEmbedding,
			relayMode:   constant.RelayModeEmbeddings,
			wantURL:     "https://qianfan.baidubce.com/v2/embeddings",
		},
		{
			name:        "images generations",
			relayMode:   constant.RelayModeImagesGenerations,
			wantURL:     "https://qianfan.baidubce.com/v2/images/generations",
		},
		{
			name:        "images edits",
			relayMode:   constant.RelayModeImagesEdits,
			wantURL:     "https://qianfan.baidubce.com/v2/images/edits",
		},
		{
			name:        "rerank",
			relayMode:   constant.RelayModeRerank,
			wantURL:     "https://qianfan.baidubce.com/v2/rerank",
		},
		{
			name:      "openai format with unknown mode",
			relayMode: constant.RelayModeUnknown,
			wantErr:   "unsupported relay mode: 0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := &relaycommon.RelayInfo{
				RelayFormat: tt.relayFormat,
				RelayMode:   tt.relayMode,
				ChannelMeta: &relaycommon.ChannelMeta{
					ChannelBaseUrl: "https://qianfan.baidubce.com",
				},
			}

			url, err := (&Adaptor{}).GetRequestURL(info)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantURL, url)
		})
	}
}
