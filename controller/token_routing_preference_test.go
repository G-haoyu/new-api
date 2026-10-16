package controller

import (
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type tokenRoutingPreferenceData struct {
	RoutingPreference *dto.RoutingPreference `json:"routing_preference"`
}

func callTokenRoutingPreference(t *testing.T, handler gin.HandlerFunc, method string, tokenID int, userID int, body any) tokenAPIResponse {
	t.Helper()
	ctx, recorder := newAuthenticatedContext(t, method, "/api/token/"+strconv.Itoa(tokenID)+"/routing-preference", body, userID)
	ctx.Params = gin.Params{{Key: "id", Value: strconv.Itoa(tokenID)}}
	handler(ctx)
	return decodeAPIResponse(t, recorder)
}

func decodeTokenRoutingPreference(t *testing.T, response tokenAPIResponse) *dto.RoutingPreference {
	t.Helper()
	var data tokenRoutingPreferenceData
	if err := common.Unmarshal(response.Data, &data); err != nil {
		t.Fatalf("failed to decode routing preference: %v", err)
	}
	return data.RoutingPreference
}

func TestTokenRoutingPreferenceLifecycle(t *testing.T) {
	runTokenRoutingPreferenceLifecycle(t, setupTokenControllerTestDB(t))
}

func TestTokenRoutingPreferenceLifecycleMySQL(t *testing.T) {
	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("set TEST_MYSQL_DSN to run mysql routing preference test")
	}
	db, managedTokensTable := openTokenControllerExternalDB(t, "mysql", dsn)
	*managedTokensTable = true
	migrateTokenControllerTestDB(t, db)
	runTokenRoutingPreferenceLifecycle(t, db)
}

func TestTokenRoutingPreferenceLifecyclePostgres(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set TEST_POSTGRES_DSN to run postgres routing preference test")
	}
	db, managedTokensTable := openTokenControllerExternalDB(t, "postgres", dsn)
	*managedTokensTable = true
	migrateTokenControllerTestDB(t, db)
	runTokenRoutingPreferenceLifecycle(t, db)
}

func runTokenRoutingPreferenceLifecycle(t *testing.T, db *gorm.DB) {
	t.Helper()
	token := seedToken(t, db, 1, "routing-token", "routing1234token5678")

	response := callTokenRoutingPreference(t, GetTokenRoutingPreference, http.MethodGet, token.Id, 1, nil)
	if !response.Success || decodeTokenRoutingPreference(t, response) != nil {
		t.Fatalf("unconfigured token should return null preference: %+v", response)
	}

	response = callTokenRoutingPreference(t, UpdateTokenRoutingPreference, http.MethodPut, token.Id, 1, map[string]any{
		"routing_preference": map[string]any{"mode": " Price ", "max_price": 2.5, "max_attempts": 4},
	})
	if !response.Success {
		t.Fatalf("update failed: %s", response.Message)
	}

	response = callTokenRoutingPreference(t, GetTokenRoutingPreference, http.MethodGet, token.Id, 1, nil)
	preference := decodeTokenRoutingPreference(t, response)
	if preference == nil || preference.Mode != "price" {
		t.Fatalf("unexpected stored preference: %+v", preference)
	}
	// 高级约束不再对用户开放：请求里携带也不会被持久化。
	var stored model.Token
	if err := db.First(&stored, token.Id).Error; err != nil {
		t.Fatalf("reload token: %v", err)
	}
	if stored.RoutingPreference != `{"mode":"price"}` {
		t.Fatalf("only mode should be stored, got %q", stored.RoutingPreference)
	}

	// 令牌列表/详情响应不暴露原始 routing_preference 列。
	ctx, recorder := newAuthenticatedContext(t, http.MethodGet, "/api/token/"+strconv.Itoa(token.Id), nil, 1)
	ctx.Params = gin.Params{{Key: "id", Value: strconv.Itoa(token.Id)}}
	GetToken(ctx)
	if strings.Contains(recorder.Body.String(), "routing_preference") {
		t.Fatalf("token detail leaked routing_preference column: %s", recorder.Body.String())
	}

	response = callTokenRoutingPreference(t, UpdateTokenRoutingPreference, http.MethodPut, token.Id, 1, map[string]any{"routing_preference": nil})
	if !response.Success {
		t.Fatalf("clear failed: %s", response.Message)
	}
	var reloaded model.Token
	if err := db.First(&reloaded, token.Id).Error; err != nil {
		t.Fatalf("reload token: %v", err)
	}
	if reloaded.RoutingPreference != "" {
		t.Fatalf("preference should be cleared, got %q", reloaded.RoutingPreference)
	}
}

func TestTokenRoutingPreferenceRejectsInvalidAndForeignToken(t *testing.T) {
	db := setupTokenControllerTestDB(t)
	token := seedToken(t, db, 1, "routing-owned", "routingowned1234key")

	response := callTokenRoutingPreference(t, UpdateTokenRoutingPreference, http.MethodPut, token.Id, 1, map[string]any{
		"routing_preference": map[string]any{"mode": "fastest"},
	})
	if response.Success {
		t.Fatalf("invalid mode should be rejected")
	}

	for _, handler := range []gin.HandlerFunc{GetTokenRoutingPreference, UpdateTokenRoutingPreference} {
		response = callTokenRoutingPreference(t, handler, http.MethodPut, token.Id, 2, map[string]any{
			"routing_preference": map[string]any{"mode": "price"},
		})
		if response.Success {
			t.Fatalf("foreign user must not access token routing preference")
		}
	}
	var reloaded model.Token
	if err := db.First(&reloaded, token.Id).Error; err != nil {
		t.Fatalf("reload token: %v", err)
	}
	if reloaded.RoutingPreference != "" {
		t.Fatalf("rejected writes must not persist, got %q", reloaded.RoutingPreference)
	}
}
