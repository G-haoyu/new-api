package service

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/tokenkit"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func countRequestTokenFor(t *testing.T, model string, format types.RelayFormat, meta *types.TokenCountMeta) int {
	t.Helper()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(c, constant.ContextKeyOriginalModel, model)
	tokens, err := CountRequestToken(c, meta, &relaycommon.RelayInfo{RelayFormat: format})
	require.NoError(t, err)
	return tokens
}

func TestCountRequestTokenAddsFramingAndMediaOverhead(t *testing.T) {
	previous := constant.GetMediaToken
	constant.GetMediaToken = false // 不联网量图片尺寸，图片按默认尺寸计
	t.Cleanup(func() { constant.GetMediaToken = previous })

	const model = "claude-sonnet-4-5"
	const text = "implement the cache and write tests for it"
	source := types.NewFileSourceFromData("data:image/png;base64,iVBORw0KGgo=", "")
	newMeta := func() *types.TokenCountMeta {
		return &types.TokenCountMeta{
			CombineText: text, ToolsCount: 2, MessagesCount: 3, NameCount: 1,
			Files: []*types.FileMeta{
				{FileType: types.FileTypeImage, Source: source},
				{FileType: types.FileTypeAudio, Source: source},
				{FileType: types.FileTypeVideo, Source: source},
				{FileType: types.FileTypeFile, Source: source},
				{}, // 类型未识别的附件按文件计
			},
		}
	}

	textTokens := tokenkit.Count(model, text)
	imageTokens := tokenkit.ImageTokens(model, tokenkit.Image{})
	framing := 2*8 + 3*3 + 1*3 + 3      // 每个工具 8、每条消息和每个 name 各 3，外加 3
	media := 256 + 4096*2 + 4096 + 4096 // 音频、视频、文件、未识别类型

	assert.Equal(t, textTokens+framing+imageTokens+media, countRequestTokenFor(t, model, types.RelayFormatOpenAI, newMeta()))
	assert.Equal(t, textTokens+imageTokens+media, countRequestTokenFor(t, model, types.RelayFormatClaude, newMeta()),
		"只有 OpenAI 格式有框架开销")
}
