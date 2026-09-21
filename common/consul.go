package common

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type consulKVEntry struct {
	Value string `json:"Value"`
}

type consulDatabaseConfig struct {
	ClickHouse map[string]clickHouseConfig `json:"clickhouse"`
}

const (
	// Prefer this entry when the "clickhouse" object contains multiple configs.
	defaultConsulClickHouseConfigName = "cht_maas_log"
	consulKVMaxResponseBytes          = 1 << 20
	defaultConsulTimeoutSeconds       = 10
)

type clickHouseConfig struct {
	Database string `json:"database"`
	Host     string `json:"host"`
	Password string `json:"password"`
	Port     int    `json:"port"`
	User     string `json:"user"`
}

// LoadLogSQLDSN returns the log database DSN from Consul when all Consul
// settings are present. It falls back to LOG_SQL_DSN only when all are absent.
func LoadLogSQLDSN() (string, error) {
	kvPath := strings.Trim(strings.TrimSpace(os.Getenv("CONSUL_KV_DB_PATH")), "/")
	addr := strings.TrimSpace(os.Getenv("CONSUL_HTTP_ADDR"))
	token := strings.TrimSpace(os.Getenv("CONSUL_HTTP_TOKEN"))
	if kvPath == "" && addr == "" && token == "" {
		return os.Getenv("LOG_SQL_DSN"), nil
	}
	var missing []string
	if addr == "" {
		missing = append(missing, "CONSUL_HTTP_ADDR")
	}
	if token == "" {
		missing = append(missing, "CONSUL_HTTP_TOKEN")
	}
	if kvPath == "" {
		missing = append(missing, "CONSUL_KV_DB_PATH")
	}
	if len(missing) > 0 {
		return "", fmt.Errorf("incomplete Consul configuration: missing %s", strings.Join(missing, ", "))
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultConsulTimeoutSeconds*time.Second)
	defer cancel()

	client := &http.Client{Timeout: defaultConsulTimeoutSeconds * time.Second}
	return fetchClickHouseLogDSN(ctx, client, addr, token, kvPath)
}

func fetchClickHouseLogDSN(ctx context.Context, client *http.Client, addr, token, kvPath string) (string, error) {
	endpoint, err := consulKVURL(addr, kvPath)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("create Consul request: %w", err)
	}
	if token != "" {
		req.Header.Set("X-Consul-Token", token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("request Consul KV: %w", err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return "", fmt.Errorf("Consul KV path %q was not found", kvPath)
	case http.StatusForbidden, http.StatusUnauthorized:
		return "", fmt.Errorf("Consul KV path %q denied (%s); check CONSUL_HTTP_TOKEN", kvPath, resp.Status)
	default:
		return "", fmt.Errorf("request Consul KV: unexpected status %s", resp.Status)
	}

	var entries []consulKVEntry
	if err := DecodeJson(io.LimitReader(resp.Body, consulKVMaxResponseBytes), &entries); err != nil {
		return "", fmt.Errorf("decode Consul KV response: %w", err)
	}
	if len(entries) == 0 || entries[0].Value == "" {
		return "", fmt.Errorf("Consul KV returned an empty value")
	}

	payload, err := base64.StdEncoding.DecodeString(entries[0].Value)
	if err != nil {
		return "", fmt.Errorf("decode Consul KV value: %w", err)
	}
	var config consulDatabaseConfig
	if err := Unmarshal(payload, &config); err != nil {
		return "", fmt.Errorf("decode ClickHouse configuration: %w", err)
	}
	name, clickHouse, err := selectClickHouseConfig(config.ClickHouse)
	if err != nil {
		return "", err
	}
	if clickHouse.Host == "" || clickHouse.User == "" || clickHouse.Password == "" || clickHouse.Database == "" || clickHouse.Port < 1 || clickHouse.Port > 65535 {
		return "", fmt.Errorf("ClickHouse configuration %q is incomplete", name)
	}

	dsn := &url.URL{
		Scheme: "clickhouse",
		User:   url.UserPassword(clickHouse.User, clickHouse.Password),
		Host:   net.JoinHostPort(clickHouse.Host, strconv.Itoa(clickHouse.Port)),
		Path:   "/" + clickHouse.Database,
	}
	return dsn.String(), nil
}

func consulKVURL(addr, kvPath string) (string, error) {
	addr = strings.TrimSpace(addr)
	if !strings.Contains(addr, "://") {
		addr = "http://" + addr
	}
	endpoint, err := url.Parse(strings.TrimRight(addr, "/"))
	if err != nil || endpoint.Host == "" || (endpoint.Scheme != "http" && endpoint.Scheme != "https") {
		return "", fmt.Errorf("invalid CONSUL_HTTP_ADDR %q: expected [http://|https://]host:port", addr)
	}
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + "/v1/kv/" + strings.TrimLeft(kvPath, "/")
	return endpoint.String(), nil
}

func selectClickHouseConfig(configs map[string]clickHouseConfig) (string, clickHouseConfig, error) {
	if len(configs) == 0 {
		return "", clickHouseConfig{}, fmt.Errorf("Consul KV value contains no clickhouse configuration")
	}
	if config, ok := configs[defaultConsulClickHouseConfigName]; ok {
		return defaultConsulClickHouseConfigName, config, nil
	}
	if len(configs) == 1 {
		for name, config := range configs {
			return name, config, nil
		}
	}
	return "", clickHouseConfig{}, fmt.Errorf("Consul KV value must contain %q when multiple clickhouse configurations exist", defaultConsulClickHouseConfigName)
}
