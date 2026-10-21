package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	appI18n "github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDistributeRecordsIselfEmailWithoutAffinity(t *testing.T) {
	require.NoError(t, appI18n.Init())
	setupOriginTaskDB(t)
	channel := insertOriginTaskChannel(t, common.ChannelStatusEnabled)
	setting := operation_setting.GetChannelAffinitySetting()
	previous := *setting
	t.Cleanup(func() { *setting = previous })

	for _, tt := range []struct {
		name    string
		enabled bool
		headers map[string]string
		want    string
	}{
		{"configured header with unmatched affinity rule", true, map[string]string{"X-Custom-Email": " alice@example.com "}, "alice@example.com"},
		{"affinity disabled", false, map[string]string{"X-Custom-Email": "alice@example.com"}, "alice@example.com"},
		{"next configured header", true, map[string]string{"X-Backup-Email": "bob@example.com"}, "bob@example.com"},
		{"no header", true, nil, ""},
		{"blank header", true, map[string]string{"X-Custom-Email": "  "}, ""},
		{"unconfigured header", true, map[string]string{"X-Iself-User-Email": "ignored@example.com"}, ""},
		{"non-email affinity header", true, map[string]string{"X-Custom-Email": "session-123"}, ""},
		{"display name is not a raw email", true, map[string]string{"X-Custom-Email": "Alice <alice@example.com>"}, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			*setting = operation_setting.ChannelAffinitySetting{
				Enabled: tt.enabled,
				Rules: []operation_setting.ChannelAffinityRule{{
					Name: "email-affinity", ModelRegex: []string{"^other-model$"}, PathRegex: []string{"^/other/path$"},
					KeySources: []operation_setting.ChannelAffinityKeySource{
						{Type: "context_string", Key: "username"},
						{Type: "request_header", Key: "X-Custom-Email"},
						{Type: "request_header", Key: "X-Backup-Email"},
					},
				}},
			}
			c := originTaskTestContext(7)
			c.Request = httptest.NewRequest(http.MethodPost, "/vendor/jobs", strings.NewReader(`{}`))
			c.Request.Header.Set("Content-Type", "application/json")
			for name, value := range tt.headers {
				c.Request.Header.Set(name, value)
			}
			c.Set("username", "iself")
			c.Set("resolved_task_model", "resolved-model")
			service.GetChannelConstraints(c).AddPin(dto.ChannelPin{
				ChannelId: channel.Id, Source: dto.PinSourceOriginTask, Rank: dto.PinRankOriginTask, RetryMode: dto.PinRetrySameChannel,
			})

			Distribute()(c)

			require.False(t, c.IsAborted())
			assert.Equal(t, tt.want, common.GetContextKeyString(c, constant.ContextKeyIselfEmail))
			assert.Equal(t, channel.Id, common.GetContextKeyInt(c, constant.ContextKeyChannelId))
			other := model.NewLogOther()
			service.AppendChannelAffinityAdminInfo(c, other)
			assert.Empty(t, other.Snapshot(), "recording an email must not mark affinity as used")
		})
	}
}
