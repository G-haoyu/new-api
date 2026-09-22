package common

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFetchClickHouseLogDSN(t *testing.T) {
	for _, extra := range []string{"", `,"other":{"database":"other_logs","host":"127.0.0.2","password":"other-secret","port":9001,"user":"other"}`} {
		name := "single config"
		if extra != "" {
			name = "multiple configs"
		}
		t.Run(name, func(t *testing.T) {
			payload := `{"clickhouse":{"cht_maas_log":{"database":"cht_maas_log","host":"10.2.8.75","password":"p@ss:/?#","port":9000,"user":"maas_test"}` + extra + `}}`
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/v1/kv/database/autom/maas/clickhouse/maas_logs", r.URL.Path)
				assert.Equal(t, "test-token", r.Header.Get("X-Consul-Token"))
				assert.Empty(t, r.URL.RawQuery, "token must not be sent in the query string")
				fmt.Fprint(w, consulResponse(payload))
			}))
			defer server.Close()

			dsn, err := fetchClickHouseLogDSN(context.Background(), server.Client(), server.URL, "test-token", "database/autom/maas/clickhouse/maas_logs")
			require.NoError(t, err)
			parsed, err := url.Parse(dsn)
			require.NoError(t, err)
			require.NotNil(t, parsed.User)
			password, _ := parsed.User.Password()
			assert.Equal(t, "clickhouse", parsed.Scheme)
			assert.Equal(t, "maas_test", parsed.User.Username())
			assert.Equal(t, "p@ss:/?#", password)
			assert.Equal(t, "10.2.8.75:9000", parsed.Host)
			assert.Equal(t, "/cht_maas_log", parsed.Path)
		})
	}
}

func TestFetchClickHouseLogDSNErrors(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "empty value", body: `[{"Value":""}]`, want: "empty value"},
		{name: "invalid base64", body: `[{"Value":"%%%"}]`, want: "decode Consul KV value"},
		{name: "missing config", body: consulResponse(`{"clickhouse":{}}`), want: "contains no clickhouse configuration"},
		{name: "several configs without the default name", body: consulResponse(`{"clickhouse":{"a":{"database":"logs","host":"localhost","password":"secret","port":9000,"user":"logger"},"b":{"database":"logs","host":"localhost","password":"secret","port":9000,"user":"logger"}}}`), want: `must contain "cht_maas_log" when multiple clickhouse configurations exist`},
		{name: "incomplete config", body: consulResponse(`{"clickhouse":{"cht_maas_log":{"host":"localhost"}}}`), want: "is incomplete"},
		{name: "incomplete target with valid sibling", body: consulResponse(`{"clickhouse":{"cht_maas_log":{},"other":{"database":"logs","host":"localhost","password":"secret","port":9000,"user":"logger"}}}`), want: `ClickHouse configuration "cht_maas_log" is incomplete`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				fmt.Fprint(w, tt.body)
			}))
			defer server.Close()

			_, err := fetchClickHouseLogDSN(context.Background(), server.Client(), server.URL, "token", "key")
			require.ErrorContains(t, err, tt.want)
		})
	}
}

// The Consul KV payload is keyed by database name, which differs per
// environment, so the target entry is resolved rather than hardcoded.
func TestSelectClickHouseConfigResolution(t *testing.T) {
	sole := clickHouseConfig{Database: "sandbox_logs", Host: "10.0.0.1", Password: "s", Port: 9000, User: "u"}
	dflt := clickHouseConfig{Database: "cht_maas_log", Host: "10.0.0.2", Password: "s", Port: 9000, User: "u"}

	t.Run("sole entry under any name", func(t *testing.T) {
		name, config, err := selectClickHouseConfig(map[string]clickHouseConfig{"sandbox_maas_log": sole})
		require.NoError(t, err)
		assert.Equal(t, "sandbox_maas_log", name)
		assert.Equal(t, sole, config)
	})

	t.Run("default name wins over siblings", func(t *testing.T) {
		name, config, err := selectClickHouseConfig(map[string]clickHouseConfig{"cht_maas_log": dflt, "other": sole})
		require.NoError(t, err)
		assert.Equal(t, "cht_maas_log", name)
		assert.Equal(t, dflt, config)
	})
}

// CONSUL_HTTP_ADDR is conventionally host:port, which url.Parse reads as a
// scheme unless a default is applied.
func TestConsulKVURL(t *testing.T) {
	for _, tt := range []struct {
		addr string
		want string
	}{
		{addr: "consul-uc-a.intsig.net:8500", want: "http://consul-uc-a.intsig.net:8500/v1/kv/database/autom/maas/clickhouse/maas_logs"},
		{addr: "http://consul-uc-a.intsig.net:8500", want: "http://consul-uc-a.intsig.net:8500/v1/kv/database/autom/maas/clickhouse/maas_logs"},
		{addr: "https://consul-uc-a.intsig.net:8501/", want: "https://consul-uc-a.intsig.net:8501/v1/kv/database/autom/maas/clickhouse/maas_logs"},
		{addr: "127.0.0.1:8500", want: "http://127.0.0.1:8500/v1/kv/database/autom/maas/clickhouse/maas_logs"},
	} {
		t.Run(tt.addr, func(t *testing.T) {
			got, err := consulKVURL(tt.addr, "database/autom/maas/clickhouse/maas_logs")
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}

	for _, addr := range []string{"", "ftp://consul:8500", "http://"} {
		t.Run("invalid "+addr, func(t *testing.T) {
			_, err := consulKVURL(addr, "key")
			require.ErrorContains(t, err, "invalid CONSUL_HTTP_ADDR")
		})
	}
}

func TestFetchClickHouseLogDSNHTTPStatus(t *testing.T) {
	for _, tt := range []struct {
		status int
		want   string
	}{
		{status: http.StatusNotFound, want: `Consul KV path "key" was not found`},
		{status: http.StatusForbidden, want: "check CONSUL_HTTP_TOKEN"},
		{status: http.StatusInternalServerError, want: "unexpected status"},
	} {
		t.Run(fmt.Sprint(tt.status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
			}))
			defer server.Close()

			_, err := fetchClickHouseLogDSN(context.Background(), server.Client(), server.URL, "token", "key")
			require.ErrorContains(t, err, tt.want)
		})
	}
}

// The HTTP helper accepts host:port and omits empty token headers; the public
// loader still requires all three environment settings before using Consul.
func TestFetchClickHouseLogDSNWithoutToken(t *testing.T) {
	payload := `{"clickhouse":{"cht_maas_log":{"database":"logs","host":"127.0.0.1","password":"secret","port":9000,"user":"logger"}}}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, hasToken := r.Header["X-Consul-Token"]
		assert.False(t, hasToken, "no empty token header should be sent")
		fmt.Fprint(w, consulResponse(payload))
	}))
	defer server.Close()

	dsn, err := fetchClickHouseLogDSN(context.Background(), server.Client(), strings.TrimPrefix(server.URL, "http://"), "", "key")
	require.NoError(t, err)
	assert.Equal(t, "clickhouse://logger:secret@127.0.0.1:9000/logs", dsn)
}

func TestLoadLogSQLDSNFromConsul(t *testing.T) {
	payload := `{"clickhouse":{"cht_maas_log":{"database":"logs","host":"127.0.0.1","password":"secret","port":9000,"user":"logger"}}}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, consulResponse(payload))
	}))
	defer server.Close()

	t.Setenv("CONSUL_HTTP_ADDR", server.URL)
	t.Setenv("CONSUL_HTTP_TOKEN", "token")
	t.Setenv("CONSUL_KV_DB_PATH", "database/clickhouse")
	t.Setenv("LOG_SQL_DSN", "old-value")

	dsn, err := LoadLogSQLDSN()
	require.NoError(t, err)
	assert.Equal(t, "clickhouse://logger:secret@127.0.0.1:9000/logs", dsn)
	assert.Equal(t, "old-value", os.Getenv("LOG_SQL_DSN"))
}

func TestLoadLogSQLDSNSettings(t *testing.T) {
	for _, tt := range []struct {
		name  string
		addr  string
		token string
		path  string
		want  string
	}{
		{name: "all missing"},
		{name: "all blank", addr: " ", token: " ", path: " / "},
		{name: "address only", addr: "http://consul:8500", want: "CONSUL_HTTP_TOKEN, CONSUL_KV_DB_PATH"},
		{name: "token only", token: "token", want: "CONSUL_HTTP_ADDR, CONSUL_KV_DB_PATH"},
		{name: "path only", path: "key", want: "CONSUL_HTTP_ADDR, CONSUL_HTTP_TOKEN"},
		{name: "missing address", token: "token", path: "key", want: "CONSUL_HTTP_ADDR"},
		{name: "missing token", addr: "http://consul:8500", path: "key", want: "CONSUL_HTTP_TOKEN"},
		{name: "missing path", addr: "http://consul:8500", token: "token", want: "CONSUL_KV_DB_PATH"},
		{name: "blank address", addr: " ", token: "token", path: "key", want: "CONSUL_HTTP_ADDR"},
		{name: "blank token", addr: "http://consul:8500", token: " ", path: "key", want: "CONSUL_HTTP_TOKEN"},
		{name: "empty normalized path", addr: "http://consul:8500", token: "token", path: " / ", want: "CONSUL_KV_DB_PATH"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("CONSUL_HTTP_ADDR", tt.addr)
			t.Setenv("CONSUL_HTTP_TOKEN", tt.token)
			t.Setenv("CONSUL_KV_DB_PATH", tt.path)
			for _, fallback := range []string{"", "existing"} {
				t.Run("fallback="+fallback, func(t *testing.T) {
					t.Setenv("LOG_SQL_DSN", fallback)
					dsn, err := LoadLogSQLDSN()
					if tt.want != "" {
						require.EqualError(t, err, "incomplete Consul configuration: missing "+tt.want)
						assert.Empty(t, dsn)
						return
					}
					require.NoError(t, err)
					assert.Equal(t, fallback, dsn)
				})
			}
		})
	}
}

func consulResponse(payload string) string {
	return fmt.Sprintf(`[{"Value":%q}]`, base64.StdEncoding.EncodeToString([]byte(payload)))
}

func TestFetchClickHouseLogDSNHostList(t *testing.T) {
	tests := []struct {
		name string
		host string
		want string
	}{
		{"single", "10.2.8.75", "10.2.8.75:9000"},
		{"multiple", "10.2.4.215,10.2.4.217,10.2.4.218", "10.2.4.215:9000"},
		{"whitespace", " 10.2.4.215 , 10.2.4.217 ", "10.2.4.215:9000"},
		{"ipv6", "2001:db8::1,2001:db8::2", "[2001:db8::1]:9000"},
		{"empty first", ",10.2.4.217", ""},
		{"blank first", "  ,10.2.4.217", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := fmt.Sprintf(`{"clickhouse":{"cht_maas_log":{"database":"logs","host":%q,"password":"secret","port":9000,"user":"logger"}}}`, tt.host)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				fmt.Fprint(w, consulResponse(payload))
			}))
			defer server.Close()
			dsn, err := fetchClickHouseLogDSN(context.Background(), server.Client(), server.URL, "token", "key")
			if tt.want == "" {
				require.ErrorContains(t, err, "is incomplete")
				return
			}
			require.NoError(t, err)
			parsed, err := url.Parse(dsn)
			require.NoError(t, err)
			assert.Equal(t, tt.want, parsed.Host)
		})
	}
}

func TestFetchClickHouseLogDSNConfiguredKey(t *testing.T) {
	for _, key := range []string{"ch_maas_log", " ch_maas_log ", "missing"} {
		t.Run(key, func(t *testing.T) {
			t.Setenv("CONSUL_CLICKHOUSE_CONFIG_KEY", key)
			payload := `{"clickhouse":{"ch_maas_log":{"database":"prod_logs","host":"10.2.4.215,10.2.4.217","password":"secret","port":9000,"user":"logger"},"cht_maas_log":{"database":"test_logs","host":"localhost","password":"secret","port":9000,"user":"logger"}}}`
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				fmt.Fprint(w, consulResponse(payload))
			}))
			defer server.Close()
			dsn, err := fetchClickHouseLogDSN(context.Background(), server.Client(), server.URL, "token", "key")
			if key == "missing" {
				require.ErrorContains(t, err, "CONSUL_CLICKHOUSE_CONFIG_KEY")
				return
			}
			require.NoError(t, err)
			parsed, err := url.Parse(dsn)
			require.NoError(t, err)
			assert.Equal(t, "10.2.4.215:9000", parsed.Host)
			assert.Equal(t, "/prod_logs", parsed.Path)
		})
	}
}

func TestSelectClickHouseConfigBlankKey(t *testing.T) {
	t.Setenv("CONSUL_CLICKHOUSE_CONFIG_KEY", "  ")
	name, _, err := selectClickHouseConfig(map[string]clickHouseConfig{"ch_maas_log": {}})
	require.NoError(t, err)
	assert.Equal(t, "ch_maas_log", name)
}
