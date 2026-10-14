package admin

import (
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// PlatformOpsHandler 是三平台（WorkBuddy / Qoder / Trae）运营任务的通用 HTTP 端点：
// 任务执行按账号平台经 service.LookupPlatformOpsRunner 分派到对应 OpsService，
// 本 handler 不感知具体平台。各平台 OAuth 等专属端点仍在各自的 handler 中。
type PlatformOpsHandler struct {
	adminService service.AdminService
	// settings 平台级任务调度配置存取（settings 表 platform_ops_config 键）。
	settings service.SettingRepository
}

func NewPlatformOpsHandler(adminService service.AdminService, settings service.SettingRepository) *PlatformOpsHandler {
	return &PlatformOpsHandler{adminService: adminService, settings: settings}
}

// platformOpsAccountState 是单账号任务状态端点的响应形状。
type platformOpsAccountState struct {
	service.PlatformOpsTaskInfo
	State service.OpsStateEntry `json:"state"`
}

// RunTask 执行账号的一个运营任务（手动触发）。
// POST /admin/platform-ops/accounts/:id/tasks/:task
func (h *PlatformOpsHandler) RunTask(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	taskID := c.Param("task")
	account, runner, err := h.loadAccountRunner(c, accountID)
	if err != nil {
		return
	}
	if _, ok := lookupOpsTask(runner, taskID); !ok {
		response.BadRequest(c, "Unknown ops task "+taskID+" for platform "+account.Platform)
		return
	}
	if err := runner.RunAccountTask(c.Request.Context(), account, taskID); err != nil {
		// 任务业务失败：200 + done=false + error 便于前端展示上游原因（账号与状态已落库）。
		response.Success(c, gin.H{"done": false, "error": err.Error()})
		return
	}
	// 成功结论文本（到账/幂等跳过/未开放原因）：SaveOpsState 已同步内存 Extra，直接回读。
	note := service.LoadOpsState(account, taskID).Note
	response.Success(c, gin.H{"done": true, "note": note})
}

// AccountStates 返回账号全部运营任务的描述与最近状态。
// GET /admin/platform-ops/accounts/:id
func (h *PlatformOpsHandler) AccountStates(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	account, runner, err := h.loadAccountRunner(c, accountID)
	if err != nil {
		return
	}
	states := make([]platformOpsAccountState, 0, len(runner.Tasks()))
	for _, task := range runner.Tasks() {
		states = append(states, platformOpsAccountState{
			PlatformOpsTaskInfo: task.Describe(),
			State:               service.LoadOpsState(account, task.ID),
		})
	}
	response.Success(c, gin.H{"platform": account.Platform, "tasks": states})
}

// TaskCatalog 返回全部平台的运营任务注册表。
// GET /admin/platform-ops/tasks
func (h *PlatformOpsHandler) TaskCatalog(c *gin.Context) {
	type platformTasks struct {
		Platform string                        `json:"platform"`
		Tasks    []service.PlatformOpsTaskInfo `json:"tasks"`
	}
	out := make([]platformTasks, 0, 4)
	for _, runner := range service.AllPlatformOpsRunners() {
		tasks := runner.Tasks()
		infos := make([]service.PlatformOpsTaskInfo, 0, len(tasks))
		for _, task := range tasks {
			infos = append(infos, task.Describe())
		}
		out = append(out, platformTasks{Platform: runner.Platform(), Tasks: infos})
	}
	response.Success(c, out)
}

// platformOpsTaskConfigState 是配置端点里单个任务的展示形状（注册表默认 + 平台覆盖）。
type platformOpsTaskConfigState struct {
	service.PlatformOpsTaskInfo
	Config service.OpsTaskConfig `json:"config"`
}

// loadPlatformRunner 解析平台参数并查其运营任务运行器；写响应后返回错误。
func (h *PlatformOpsHandler) loadPlatformRunner(c *gin.Context) (service.PlatformOpsRunner, bool) {
	platform := strings.TrimSpace(c.Param("platform"))
	runner, ok := service.LookupPlatformOpsRunner(platform)
	if !ok {
		response.Error(c, http.StatusNotFound, "No ops runner registered for platform "+platform)
		return nil, false
	}
	return runner, true
}

// PlatformConfig 返回平台全部任务的默认调度与平台级配置覆盖（统一作用于该平台
// 全部账号）。GET /admin/platform-ops/platforms/:platform/config
func (h *PlatformOpsHandler) PlatformConfig(c *gin.Context) {
	runner, ok := h.loadPlatformRunner(c)
	if !ok {
		return
	}
	configs := service.LoadPlatformOpsTaskConfigs(c.Request.Context(), h.settings, runner.Platform())
	tasks := make([]platformOpsTaskConfigState, 0, len(runner.Tasks()))
	for _, task := range runner.Tasks() {
		tasks = append(tasks, platformOpsTaskConfigState{
			PlatformOpsTaskInfo: task.Describe(),
			Config:              configs[task.ID],
		})
	}
	response.Success(c, gin.H{"platform": runner.Platform(), "tasks": tasks})
}

// UpdatePlatformConfig 保存平台级任务配置（对该平台全部账号统一生效；
// settings 表 platform_ops_config 键内该平台子表整体覆盖）。
// PUT /admin/platform-ops/platforms/:platform/config
// body: {"tasks": {"checkin": {"enabled": true, "hours": [9,21]}, ...}}
func (h *PlatformOpsHandler) UpdatePlatformConfig(c *gin.Context) {
	runner, ok := h.loadPlatformRunner(c)
	if !ok {
		return
	}
	var req struct {
		Tasks map[string]service.OpsTaskConfig `json:"tasks"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	for taskID, cfg := range req.Tasks {
		if _, ok := lookupOpsTask(runner, taskID); !ok {
			response.BadRequest(c, "Unknown ops task "+taskID+" for platform "+runner.Platform())
			return
		}
		if err := validateOpsTaskConfig(cfg); err != nil {
			response.BadRequest(c, fmt.Sprintf("invalid config for task %s: %s", taskID, err.Error()))
			return
		}
	}
	if err := service.SavePlatformOpsTaskConfigs(c.Request.Context(), h.settings, runner.Platform(), req.Tasks); err != nil {
		response.Error(c, http.StatusInternalServerError, "Failed to save platform ops task config")
		return
	}
	response.Success(c, gin.H{"saved": true})
}

// validateOpsTaskConfig 校验单任务配置：hours ∈ [0,23] 且去重排序；interval ∈ [1,168] 小时。
func validateOpsTaskConfig(cfg service.OpsTaskConfig) error {
	if len(cfg.Hours) > 0 {
		seen := make(map[int]struct{}, len(cfg.Hours))
		for _, h := range cfg.Hours {
			if h < 0 || h > 23 {
				return errors.New("hours must be within 0-23")
			}
			if _, dup := seen[h]; dup {
				return errors.New("hours must not contain duplicates")
			}
			seen[h] = struct{}{}
		}
		sort.Ints(cfg.Hours)
	}
	if cfg.IntervalHours != nil && (*cfg.IntervalHours < 1 || *cfg.IntervalHours > 168) {
		return errors.New("interval_hours must be within 1-168")
	}
	return nil
}

// loadAccountRunner 加载账号并解析其平台的运营任务运行器；写响应后返回错误。
func (h *PlatformOpsHandler) loadAccountRunner(c *gin.Context, accountID int64) (*service.Account, service.PlatformOpsRunner, error) {
	account, err := h.adminService.GetAccount(c.Request.Context(), accountID)
	if err != nil {
		response.ErrorFrom(c, err)
		return nil, nil, err
	}
	runner, ok := service.LookupPlatformOpsRunner(account.Platform)
	if !ok {
		response.Error(c, http.StatusNotFound, "No ops runner registered for platform "+account.Platform)
		return nil, nil, errors.New("no ops runner")
	}
	return account, runner, nil
}

func lookupOpsTask(runner service.PlatformOpsRunner, taskID string) (service.PlatformOpsTask, bool) {
	for _, task := range runner.Tasks() {
		if task.ID == taskID {
			return task, true
		}
	}
	return service.PlatformOpsTask{}, false
}
