package common

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func pageQueryContext(query string) *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/api/log/?"+query, nil)
	return c
}

// TestGetPageQueryCaps guards that ordinary list endpoints stay capped at 100
// while the admin log list may go up to 1000, and that PageSize/offset always
// reflect the effective value so callers can trust the echoed page_size.
func TestGetPageQueryCaps(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name      string
		query     string
		maxSize   int
		wantSize  int
		wantStart int
	}{
		{name: "default size", query: "p=1", maxSize: MaxPageSize, wantSize: ItemsPerPage, wantStart: 0},
		{name: "ordinary list capped at 100", query: "p=2&page_size=300", maxSize: MaxPageSize, wantSize: 100, wantStart: 100},
		{name: "admin log allows 500", query: "p=2&page_size=500", maxSize: MaxAdminLogPageSize, wantSize: 500, wantStart: 500},
		{name: "admin log capped at 1000", query: "p=3&page_size=5000", maxSize: MaxAdminLogPageSize, wantSize: 1000, wantStart: 2000},
		{name: "legacy ps param", query: "p=1&ps=50", maxSize: MaxAdminLogPageSize, wantSize: 50, wantStart: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := GetPageQueryWithMax(pageQueryContext(tt.query), tt.maxSize)
			assert.Equal(t, tt.wantSize, info.GetPageSize())
			assert.Equal(t, tt.wantStart, info.GetStartIdx())
		})
	}

	assert.Equal(t, 100, GetPageQuery(pageQueryContext("page_size=1000")).GetPageSize(), "GetPageQuery keeps the 100 cap")
}
