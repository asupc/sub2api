package admin

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// ModelBenchmarkHandler manages model capability benchmark plans and evidence.
type ModelBenchmarkHandler struct {
	service *service.ModelBenchmarkService
}

// NewModelBenchmarkHandler creates the handler.
func NewModelBenchmarkHandler(benchmarkService *service.ModelBenchmarkService) *ModelBenchmarkHandler {
	return &ModelBenchmarkHandler{service: benchmarkService}
}

type modelBenchmarkPlanRequest struct {
	ComparisonKey   string `json:"comparison_key"`
	Name            string `json:"name" binding:"required"`
	SuiteID         string `json:"suite_id"`
	ModelID         string `json:"model_id" binding:"required"`
	CronExpression  string `json:"cron_expression" binding:"required"`
	Enabled         *bool  `json:"enabled"`
	SampleCount     int    `json:"sample_count"`
	MaxOutputTokens int    `json:"max_output_tokens"`
	TimeoutSeconds  int    `json:"timeout_seconds"`
	ReasoningEffort string `json:"reasoning_effort"`
	ReasoningLevel  string `json:"reasoning_level"`
	MaxResults      int    `json:"max_results"`
}

func (r modelBenchmarkPlanRequest) toPlan() *service.ModelBenchmarkPlan {
	plan := &service.ModelBenchmarkPlan{ComparisonKey: r.ComparisonKey, Name: r.Name, SuiteID: r.SuiteID, ModelID: r.ModelID, CronExpression: r.CronExpression, SampleCount: r.SampleCount, MaxOutputTokens: r.MaxOutputTokens, TimeoutSeconds: r.TimeoutSeconds, ReasoningEffort: r.ReasoningEffort, ReasoningLevel: r.ReasoningLevel, MaxResults: r.MaxResults}
	if r.Enabled != nil {
		plan.Enabled = *r.Enabled
	}
	return plan
}

// ListSuites GET /admin/model-benchmarks/suites
func (h *ModelBenchmarkHandler) ListSuites(c *gin.Context) {
	response.Success(c, h.service.ListSuites())
}

// ListTargets GET /admin/model-benchmarks/targets
func (h *ModelBenchmarkHandler) ListTargets(c *gin.Context) {
	targets, err := h.service.ListTargets(c.Request.Context())
	if err != nil {
		response.InternalError(c, err.Error())
		return
	}
	response.Success(c, targets)
}

// GetSettings GET /admin/model-benchmarks/settings
func (h *ModelBenchmarkHandler) GetSettings(c *gin.Context) {
	settings, err := h.service.GetBenchmarkSettings(c.Request.Context())
	if err != nil {
		response.InternalError(c, err.Error())
		return
	}
	response.Success(c, settings)
}

// UpdateSettings PUT /admin/model-benchmarks/settings
func (h *ModelBenchmarkHandler) UpdateSettings(c *gin.Context) {
	var req service.ModelBenchmarkSettings
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	settings, err := h.service.UpdateBenchmarkSettings(c.Request.Context(), &req)
	if err != nil {
		writeModelBenchmarkError(c, err)
		return
	}
	response.Success(c, settings)
}

// CreatePlan POST /admin/model-benchmarks/plans
func (h *ModelBenchmarkHandler) CreatePlan(c *gin.Context) {
	var req modelBenchmarkPlanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	plan, err := h.service.CreatePlan(c.Request.Context(), req.toPlan())
	if err != nil {
		writeModelBenchmarkError(c, err)
		return
	}
	response.Created(c, plan)
}

// ListPlans GET /admin/model-benchmarks/plans
func (h *ModelBenchmarkHandler) ListPlans(c *gin.Context) {
	plans, err := h.service.ListPlans(c.Request.Context())
	if err != nil {
		response.InternalError(c, err.Error())
		return
	}
	response.Success(c, plans)
}

// GetPlan GET /admin/model-benchmarks/plans/:id
func (h *ModelBenchmarkHandler) GetPlan(c *gin.Context) {
	id, err := parseModelBenchmarkID(c)
	if err != nil {
		return
	}
	plan, err := h.service.GetPlan(c.Request.Context(), id)
	if err != nil {
		response.NotFound(c, "plan not found")
		return
	}
	response.Success(c, plan)
}

// UpdatePlan PUT /admin/model-benchmarks/plans/:id
func (h *ModelBenchmarkHandler) UpdatePlan(c *gin.Context) {
	id, err := parseModelBenchmarkID(c)
	if err != nil {
		return
	}
	var req modelBenchmarkPlanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	plan, err := h.service.UpdatePlan(c.Request.Context(), id, req.toPlan())
	if err != nil {
		writeModelBenchmarkError(c, err)
		return
	}
	response.Success(c, plan)
}

// DeletePlan DELETE /admin/model-benchmarks/plans/:id
func (h *ModelBenchmarkHandler) DeletePlan(c *gin.Context) {
	id, err := parseModelBenchmarkID(c)
	if err != nil {
		return
	}
	if err := h.service.DeletePlan(c.Request.Context(), id); err != nil {
		response.InternalError(c, err.Error())
		return
	}
	response.Success(c, gin.H{"message": "deleted"})
}

// QueueRun POST /admin/model-benchmarks/plans/:id/run
func (h *ModelBenchmarkHandler) QueueRun(c *gin.Context) {
	id, err := parseModelBenchmarkID(c)
	if err != nil {
		return
	}
	plan, err := h.service.QueueRun(c.Request.Context(), id)
	if err != nil {
		writeModelBenchmarkError(c, err)
		return
	}
	response.Accepted(c, plan)
}

// ListRuns GET /admin/model-benchmarks/runs
func (h *ModelBenchmarkHandler) ListRuns(c *gin.Context) {
	filters := service.ModelBenchmarkRunFilters{MaxOutputTokens: modelBenchmarkQueryInt(c, "max_output_tokens"), TimeoutSeconds: modelBenchmarkQueryInt(c, "timeout_seconds"), SampleCount: modelBenchmarkQueryInt(c, "sample_count"), ComparisonKey: c.Query("comparison_key"), ModelID: c.Query("model_id"), Status: c.Query("status"), Limit: parseModelBenchmarkLimit(c.Query("limit"), 50), Offset: parseModelBenchmarkOffset(c.Query("offset"))}
	if id, err := strconv.ParseInt(c.Query("plan_id"), 10, 64); err == nil && id > 0 {
		filters.PlanID = &id
	}
	runs, err := h.service.ListRuns(c.Request.Context(), filters)
	if err != nil {
		response.InternalError(c, err.Error())
		return
	}
	response.Success(c, runs)
}

// GetRun GET /admin/model-benchmarks/runs/:id
func (h *ModelBenchmarkHandler) GetRun(c *gin.Context) {
	id, err := parseModelBenchmarkID(c)
	if err != nil {
		return
	}
	run, err := h.service.GetRun(c.Request.Context(), id)
	if err != nil {
		response.NotFound(c, "run not found")
		return
	}
	response.Success(c, run)
}

// ListRunItems GET /admin/model-benchmarks/runs/:id/items
func (h *ModelBenchmarkHandler) ListRunItems(c *gin.Context) {
	id, err := parseModelBenchmarkID(c)
	if err != nil {
		return
	}
	items, err := h.service.ListRunItems(c.Request.Context(), id)
	if err != nil {
		response.InternalError(c, err.Error())
		return
	}
	response.Success(c, items)
}

// ListHistory GET /admin/model-benchmarks/history
func (h *ModelBenchmarkHandler) ListHistory(c *gin.Context) {
	filters := service.ModelBenchmarkHistoryFilters{MaxOutputTokens: modelBenchmarkQueryInt(c, "max_output_tokens"), TimeoutSeconds: modelBenchmarkQueryInt(c, "timeout_seconds"), SampleCount: modelBenchmarkQueryInt(c, "sample_count"), ComparisonKey: c.Query("comparison_key"), SuiteID: c.Query("suite_id"), SuiteVersion: c.Query("suite_version"), ScoringVersion: c.Query("scoring_version"), ModelID: c.Query("model_id"), Limit: parseModelBenchmarkLimit(c.Query("limit"), 500)}
	if raw := c.Query("from"); raw != "" {
		if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
			filters.From = parsed
		}
	}
	if raw := c.Query("to"); raw != "" {
		if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
			filters.To = parsed
		}
	}
	points, err := h.service.ListHistory(c.Request.Context(), filters)
	if err != nil {
		response.InternalError(c, err.Error())
		return
	}
	response.Success(c, points)
}

// ListRankings GET /admin/model-benchmarks/rankings
// 按题库三元组过滤（缺省为编译期最新题库）；每模型取最新一次成功/部分成功 run，无批次/预算门槛。
func (h *ModelBenchmarkHandler) ListRankings(c *gin.Context) {
	filters := service.ModelBenchmarkRankingFilters{SuiteID: c.Query("suite_id"), SuiteVersion: c.Query("suite_version"), ScoringVersion: c.Query("scoring_version")}
	rankings, err := h.service.ListRankings(c.Request.Context(), filters, parseModelBenchmarkLimit(c.Query("limit"), 50))
	if err != nil {
		response.InternalError(c, err.Error())
		return
	}
	response.Success(c, rankings)
}

// ListRankingSuites GET /admin/model-benchmarks/rankings/suites
func (h *ModelBenchmarkHandler) ListRankingSuites(c *gin.Context) {
	suites, err := h.service.ListRankingSuites(c.Request.Context())
	if err != nil {
		response.InternalError(c, err.Error())
		return
	}
	response.Success(c, suites)
}

// GetModelOverview GET /admin/model-benchmarks/models/:model_id/overview
func (h *ModelBenchmarkHandler) GetModelOverview(c *gin.Context) {
	modelID := c.Param("model_id")
	if modelID == "" {
		response.BadRequest(c, "model_id is required")
		return
	}
	overview, err := h.service.GetModelOverview(c.Request.Context(), modelID)
	if err != nil {
		response.InternalError(c, err.Error())
		return
	}
	response.Success(c, overview)
}

func parseModelBenchmarkID(c *gin.Context) (int64, error) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "invalid id")
		return 0, err
	}
	return id, nil
}
func parseModelBenchmarkLimit(raw string, fallback int) int {
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return fallback
	}
	if value > 1000 {
		return 1000
	}
	return value
}
func parseModelBenchmarkOffset(raw string) int {
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return 0
	}
	return value
}
func writeModelBenchmarkError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrModelBenchmarkUnsupportedTarget):
		response.ErrorWithDetails(c, http.StatusBadRequest, "model benchmark target is not supported", "MODEL_BENCHMARK_UNSUPPORTED_TARGET", nil)
	case errors.Is(err, service.ErrModelBenchmarkInvalidPlan):
		response.BadRequest(c, err.Error())
	case errors.Is(err, sql.ErrNoRows):
		response.Error(c, http.StatusConflict, "model benchmark plan is running or not found")
	default:
		response.ErrorFrom(c, err)
	}
}

func modelBenchmarkQueryInt(c *gin.Context, key string) int {
	value, err := strconv.Atoi(c.Query(key))
	if err != nil || value < 0 || value > 100000 {
		return 0
	}
	return value
}
