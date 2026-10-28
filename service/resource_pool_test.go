package service

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFetchResourcePoolSourceParsesAccountsPayload(t *testing.T) {
	var gotPassword, gotKey string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPassword = r.Header.Get("X-Admin-Password")
		gotKey = r.Header.Get("X-Api-Key")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"accounts":[
			{"id":"a1","email":"a1@example.com","enabled":true,"usage_current":120,"usage_limit":1000},
			{"id":"a2","email":"a2@example.com","enabled":false,"usage_current":40,"usage_percent":0.25}
		]}`))
	}))
	defer server.Close()

	source := &model.ResourcePoolSource{
		Id:            7,
		Name:          "tokenmanagement21",
		Endpoint:      server.URL,
		AdminPassword: "s3cret",
		GroupName:     "default",
	}

	accounts, status := fetchResourcePoolSource(source)

	require.Equal(t, "success", status.Status)
	require.Len(t, accounts, 2)
	assert.Equal(t, "s3cret", gotPassword)
	assert.Equal(t, "s3cret", gotKey)

	// usage_percentage falls back to current/limit × 100.
	assert.InDelta(t, 12.0, accounts[0].UsagePercentage, 0.001)
	// A fractional usage_percent is scaled to a percentage.
	assert.InDelta(t, 25.0, accounts[1].UsagePercentage, 0.001)
	// The source metadata is stamped onto every account.
	assert.Equal(t, int64(7), accounts[0].SourceId)
	assert.Equal(t, "tokenmanagement21", accounts[0].SourceName)
	assert.Equal(t, "default", accounts[1].SourceGroup)
}

func TestFetchResourcePoolSourceAcceptsDataEnvelope(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"b1","email":"b1@example.com","usage_current":5,"usage_limit":10}]}`))
	}))
	defer server.Close()

	accounts, status := fetchResourcePoolSource(&model.ResourcePoolSource{Id: 1, Endpoint: server.URL})

	require.Equal(t, "success", status.Status)
	require.Len(t, accounts, 1)
	assert.InDelta(t, 50.0, accounts[0].UsagePercentage, 0.001)
}

func TestFetchResourcePoolSourceReportsHTTPFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"bad password"}`))
	}))
	defer server.Close()

	accounts, status := fetchResourcePoolSource(&model.ResourcePoolSource{Id: 1, Endpoint: server.URL})

	assert.Nil(t, accounts)
	assert.Equal(t, "failed", status.Status)
	assert.Contains(t, status.Error, "401")
	assert.Contains(t, status.Error, "bad password")
}

func TestFetchResourcePoolSourceRejectsUnknownPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"unexpected":true}`))
	}))
	defer server.Close()

	accounts, status := fetchResourcePoolSource(&model.ResourcePoolSource{Id: 1, Endpoint: server.URL})

	assert.Nil(t, accounts)
	assert.Equal(t, "failed", status.Status)
	assert.NotEmpty(t, status.Error)
}

func TestFetchResourcePoolSourceRejectsEmptyEndpoint(t *testing.T) {
	accounts, status := fetchResourcePoolSource(&model.ResourcePoolSource{Id: 1, Endpoint: "  "})

	assert.Nil(t, accounts)
	assert.Equal(t, "failed", status.Status)
	assert.NotEmpty(t, status.Error)
}

func TestFetchResourcePoolSourceParsesCamelCaseUpstream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"accounts":[{
			"id":"29","email":"declan@example.com","userId":"d-123","enabled":true,
			"groups":["aws2"],"sourceChannel":"aws2",
			"rpmLimit":1000,"rpmCurrent":3,
			"subscriptionType":"POWER","subscriptionTitle":"KIRO POWER",
			"usageCurrent":10000.0,"usageLimit":10000.0,
			"usagePercent":1.0,"usagePercentage":100.0,
			"nextResetDate":"2026-11-01","lastRefresh":1792562535
		}]}`))
	}))
	defer server.Close()

	accounts, status := fetchResourcePoolSource(&model.ResourcePoolSource{
		Id: 2, Endpoint: server.URL, GroupName: "aws2",
	})

	require.Equal(t, "success", status.Status)
	require.Len(t, accounts, 1)
	a := accounts[0]
	assert.Equal(t, "d-123", a.UserId)
	assert.Equal(t, "POWER", a.SubscriptionType)
	assert.Equal(t, "KIRO POWER", a.SubscriptionTitle)
	assert.InDelta(t, 10000.0, a.UsageCurrent, 0.001)
	assert.InDelta(t, 10000.0, a.UsageLimit, 0.001)
	assert.InDelta(t, 1.0, a.UsagePercent, 0.001)
	assert.InDelta(t, 100.0, a.UsagePercentage, 0.001)
	assert.Equal(t, "2026-11-01", a.NextResetDate)
	assert.Equal(t, "aws2", a.SourceChannel)
	assert.Equal(t, int64(1000), a.RpMLimit)
	assert.Equal(t, int64(3), a.RpmCurrent)
	assert.Equal(t, int64(1792562535), a.LastRefresh)
}

func TestFetchResourcePoolSourceParsesCodex2apiShape(t *testing.T) {
	var gotAdminKey string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAdminKey = r.Header.Get("X-Admin-Key")
		// Shape mirrors the live tokenproxy response (2026-10-21): the weekly
		// percentage lives in grok_billing, and usage_percent_7d is null.
		_, _ = w.Write([]byte(`{"accounts":[{
			"id":68,"name":"zhouyu123666@gmail.com","email":"zhouyu123666@gmail.com",
			"plan_type":"SuperGrokPro","status":"active","account_type":"grok","enabled":true,
			"models":["grok-2","grok-4.5","grok-4.6"],
			"total_requests":2791,
			"usage_percent_7d":null,"usage_percent_5h":null,
			"usage_7d_detail":{"requests":264,"tokens":29794793,"account_billed":27.36,"user_billed":27.36},
			"grok_billing":{"weekly_percent":22,"weekly_period_start":"2026-10-15T07:25:36Z","weekly_period_end":"2026-10-22T07:25:36Z"},
			"grok_plan_display":{"plan":"SuperGrokPro","source":"user","status":"fresh"}
		}]}`))
	}))
	defer server.Close()

	accounts, status := fetchResourcePoolSource(&model.ResourcePoolSource{
		Id: 3, Name: "tokenproxy", Endpoint: server.URL, AdminPassword: "admin-secret", GroupName: "grok",
	})

	require.Equal(t, "success", status.Status)
	require.Len(t, accounts, 1)
	assert.Equal(t, "admin-secret", gotAdminKey)

	a := accounts[0]
	assert.Equal(t, "68", a.Id)
	assert.Equal(t, "zhouyu123666@gmail.com", a.Email)
	assert.Equal(t, "SuperGrokPro", a.PlanType)
	assert.Equal(t, "SuperGrokPro", a.PlanDisplay)
	assert.Equal(t, []string{"grok-2", "grok-4.5", "grok-4.6"}, a.Models)
	assert.Equal(t, int64(264), a.Requests7d)
	assert.Equal(t, int64(29794793), a.Tokens7d)
	assert.Equal(t, "2026-10-22T07:25:36Z", a.Reset7dAt)
	// Weekly billing percentage becomes the water level; 7d tokens become usage.
	assert.InDelta(t, 22.0, a.UsagePercentage, 0.001)
	assert.InDelta(t, 29794793.0, a.UsageCurrent, 0.001)
}

func TestTruncateBody(t *testing.T) {
	assert.Equal(t, "short", truncateBody("  short  "))
	long := make([]byte, 300)
	for i := range long {
		long[i] = 'x'
	}
	assert.Len(t, truncateBody(string(long)), 203)
}
