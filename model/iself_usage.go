package model

import "strings"

// IselfUsageRow groups consumption by recorded email, not forwarding account.
type IselfUsageRow struct {
	Email            string `json:"email"`
	Source           string `json:"source"`
	ModelName        string `json:"model_name"`
	RequestCount     int64  `json:"request_count"`
	Quota            int64  `json:"quota"`
	InputTokens      int64  `json:"input_tokens"`
	CompletionTokens int64  `json:"completion_tokens"`
	TokenUsed        int64  `json:"token_used"`
	LastCalledAt     int64  `json:"last_called_at"`
}

type IselfUsageParams struct {
	Start     int64
	End       int64
	Username  string
	ModelName string
	UserID    int
}

func GetIselfUsageAnalytics(params IselfUsageParams) ([]IselfUsageRow, error) {
	rows := make([]IselfUsageRow, 0)
	var email string
	if params.UserID > 0 {
		boundEmail, err := GetUserEmail(params.UserID)
		if err != nil {
			return nil, err
		}
		email = NormalizeEmail(boundEmail)
		// An unbound email must never turn into an unfiltered log query.
		if email == "" {
			return rows, nil
		}
	}

	const emailExpr = "LOWER(TRIM(COALESCE(iself_email, '')))"
	query := LOG_DB.Model(&Log{}).
		Where("type = ?", LogTypeConsume).
		Where("created_at >= ? AND created_at <= ?", params.Start, params.End).
		Where(emailExpr + " <> ''")
	if params.UserID > 0 {
		query = query.Where(emailExpr+" = ?", email)
	} else if username := strings.TrimSpace(params.Username); username != "" {
		query = query.Where("username = ?", username)
	}
	if modelName := strings.TrimSpace(params.ModelName); modelName != "" {
		query = query.Where("model_name = ?", modelName)
	}

	// Qualify columns to prevent ClickHouse aggregate-alias substitution.
	err := query.Select(emailExpr + " AS email, 'iself' AS source, model_name, " +
		"COUNT(*) AS request_count, COALESCE(SUM(logs.quota), 0) AS quota, " +
		"COALESCE(SUM(logs.prompt_tokens), 0) AS input_tokens, " +
		"COALESCE(SUM(logs.completion_tokens), 0) AS completion_tokens, " +
		"COALESCE(SUM(logs.prompt_tokens), 0) + COALESCE(SUM(logs.completion_tokens), 0) AS token_used, " +
		"MAX(logs.created_at) AS last_called_at").
		Group(emailExpr + ", model_name").
		Order("quota DESC, email ASC, model_name ASC").
		Scan(&rows).Error
	return rows, err
}
