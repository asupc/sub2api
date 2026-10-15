package admin

import (
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestWriteModelBenchmarkErrorStableStatuses(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := []struct {
		name   string
		err    error
		status int
		reason string
	}{
		{name: "unsupported", err: service.ErrModelBenchmarkUnsupportedTarget, status: http.StatusBadRequest, reason: "MODEL_BENCHMARK_UNSUPPORTED_TARGET"},
		{name: "invalid", err: service.ErrModelBenchmarkInvalidPlan, status: http.StatusBadRequest},
		{name: "conflict", err: sql.ErrNoRows, status: http.StatusConflict},
		{name: "unknown", err: errors.New("database unavailable"), status: http.StatusInternalServerError},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(recorder)
			context.Request = httptest.NewRequest(http.MethodGet, "/", nil)
			writeModelBenchmarkError(context, testCase.err)
			require.Equal(t, testCase.status, recorder.Code)
			if testCase.reason != "" {
				require.Contains(t, recorder.Body.String(), testCase.reason)
			}
		})
	}
}
