package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"regexp"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
)

const (
	forbiddenTypeValidation = "validation"
	forbiddenTypeViolation  = "violation"
	forbiddenTypeForbidden  = "forbidden"

	// 机器可读的错误码
	errorCodeForbidden       = "forbidden"
	errorCodeUnauthenticated = "unauthenticated"
	errorCodeRateLimited     = "rate_limited"
	errorCodeNetworkError    = "network_error"

	// Antigravity 额度组的稳定 key（上游 displayName 可读性差且可能变化）
	antigravityQuotaGroupGemini    = "gemini"
	antigravityQuotaGroupClaudeGPT = "claude-gpt"

	antigravityWindowFiveHour = "5h"
	antigravityWindowWeekly   = "weekly"
)

// AntigravityQuotaFetcher 从 Antigravity API 获取额度
type AntigravityQuotaFetcher struct {
	proxyRepo ProxyRepository
	cfg       *config.Config
}

// NewAntigravityQuotaFetcher 创建 AntigravityQuotaFetcher
func NewAntigravityQuotaFetcher(proxyRepo ProxyRepository, cfg *config.Config) *AntigravityQuotaFetcher {
	return &AntigravityQuotaFetcher{proxyRepo: proxyRepo, cfg: cfg}
}

// CanFetch 检查是否可以获取此账户的额度
func (f *AntigravityQuotaFetcher) CanFetch(account *Account) bool {
	if account.Platform != PlatformAntigravity {
		return false
	}
	accessToken := account.GetCredential("access_token")
	return accessToken != ""
}

// FetchQuota 获取 Antigravity 账户额度信息
func (f *AntigravityQuotaFetcher) FetchQuota(ctx context.Context, account *Account, proxyURL string) (*QuotaResult, error) {
	accessToken := account.GetCredential("access_token")
	projectID := account.GetCredential("project_id")

	client, err := antigravity.NewClient(proxyURL)
	if err != nil {
		return nil, fmt.Errorf("create antigravity client failed: %w", err)
	}

	// 额度查询使用 retrieveUserQuotaSummary：按额度组返回 5h / weekly 窗口。
	// fetchAvailableModels 的 quotaInfo 恒为 100% 剩余，不能用于展示用量。
	// 不带 project 也能返回同一份分组数据，这里不强制要求 project_id。
	quotaResp, quotaRaw, err := client.RetrieveUserQuotaSummary(ctx, accessToken, projectID)
	if err != nil {
		// 403 Forbidden: 不报错，返回 is_forbidden 标记
		var forbiddenErr *antigravity.ForbiddenError
		if errors.As(err, &forbiddenErr) {
			now := time.Now()
			fbType := classifyForbiddenType(forbiddenErr.Body)
			return &QuotaResult{
				UsageInfo: &UsageInfo{
					UpdatedAt:       &now,
					IsForbidden:     true,
					ForbiddenReason: forbiddenErr.Body,
					ForbiddenType:   fbType,
					ValidationURL:   extractValidationURL(forbiddenErr.Body),
					NeedsVerify:     fbType == forbiddenTypeValidation,
					IsBanned:        fbType == forbiddenTypeViolation,
					ErrorCode:       errorCodeForbidden,
				},
			}, nil
		}
		return nil, err
	}

	// 调用 LoadCodeAssist 获取订阅等级和 AI Credits 余额（非关键路径，失败不影响主流程）
	tierRaw, tierNormalized, loadResp := f.fetchSubscriptionTier(ctx, client, accessToken)

	// 转换为 UsageInfo
	usageInfo := f.buildUsageInfo(quotaResp, tierRaw, tierNormalized, loadResp)

	return &QuotaResult{
		UsageInfo: usageInfo,
		Raw:       quotaRaw,
	}, nil
}

// fetchSubscriptionTier 获取账号订阅等级，失败返回空字符串。
// 同时返回 LoadCodeAssistResponse，以便提取 AI Credits 余额。
func (f *AntigravityQuotaFetcher) fetchSubscriptionTier(ctx context.Context, client *antigravity.Client, accessToken string) (raw, normalized string, loadResp *antigravity.LoadCodeAssistResponse) {
	loadResp, _, err := client.LoadCodeAssist(ctx, accessToken)
	if err != nil {
		slog.Warn("failed to fetch subscription tier", "error", err)
		return "", "", nil
	}
	if loadResp == nil {
		return "", "", nil
	}

	raw = loadResp.GetTier() // 已有方法：paidTier > currentTier
	normalized = normalizeTier(raw)
	return raw, normalized, loadResp
}

// normalizeTier 将原始 tier 字符串归一化为 FREE/PRO/ULTRA/UNKNOWN
func normalizeTier(raw string) string {
	if raw == "" {
		return ""
	}
	lower := strings.ToLower(raw)
	switch {
	case strings.Contains(lower, "ultra"):
		return "ULTRA"
	case strings.Contains(lower, "pro"):
		return "PRO"
	case strings.Contains(lower, "free"):
		return "FREE"
	default:
		return "UNKNOWN"
	}
}

// antigravityQuotaGroupKey 把上游额度组名归一为稳定 key
func antigravityQuotaGroupKey(displayName string) string {
	lower := strings.ToLower(strings.TrimSpace(displayName))
	switch {
	case strings.Contains(lower, "claude") || strings.Contains(lower, "gpt"):
		return antigravityQuotaGroupClaudeGPT
	case strings.Contains(lower, "gemini"):
		return antigravityQuotaGroupGemini
	default:
		return lower
	}
}

// buildUsageInfo 将 retrieveUserQuotaSummary 响应转换为 UsageInfo。
func (f *AntigravityQuotaFetcher) buildUsageInfo(quotaResp *antigravity.RetrieveUserQuotaSummaryResponse, tierRaw, tierNormalized string, loadResp *antigravity.LoadCodeAssistResponse) *UsageInfo {
	now := time.Now()
	info := &UsageInfo{
		UpdatedAt:              &now,
		AntigravityQuotaGroups: make([]AntigravityQuotaGroup, 0, len(quotaResp.Groups)),
		SubscriptionTier:       tierNormalized,
		SubscriptionTierRaw:    tierRaw,
	}

	for _, group := range quotaResp.Groups {
		quotaGroup := AntigravityQuotaGroup{
			Key:         antigravityQuotaGroupKey(group.DisplayName),
			DisplayName: group.DisplayName,
			Buckets:     make([]AntigravityQuotaBucket, 0, len(group.Buckets)),
		}
		for _, bucket := range group.Buckets {
			// remainingFraction 是剩余比例 (0.0-1.0)，转换为使用率百分比。
			// 必须四舍五入：截断会把 1-0.8 算成 19%。
			utilization := int(math.Round((1.0 - bucket.RemainingFraction) * 100))
			if utilization < 0 {
				utilization = 0
			}
			if utilization > 100 {
				utilization = 100
			}
			quotaGroup.Buckets = append(quotaGroup.Buckets, AntigravityQuotaBucket{
				Window:      bucket.Window,
				Utilization: utilization,
				ResetTime:   bucket.ResetTime,
				Description: bucket.Description,
			})
		}
		if len(quotaGroup.Buckets) > 0 {
			info.AntigravityQuotaGroups = append(info.AntigravityQuotaGroups, quotaGroup)
		}
	}

	// 兼容字段：FiveHour 取首个 5h 窗口，供仍按 5h 口径统计的消费方使用。
	for _, group := range info.AntigravityQuotaGroups {
		for _, bucket := range group.Buckets {
			if bucket.Window != antigravityWindowFiveHour {
				continue
			}
			progress := &UsageProgress{Utilization: float64(bucket.Utilization)}
			if bucket.ResetTime != "" {
				if resetTime, err := time.Parse(time.RFC3339, bucket.ResetTime); err == nil {
					progress.ResetsAt = &resetTime
					progress.RemainingSeconds = int(time.Until(resetTime).Seconds())
				}
			}
			info.FiveHour = progress
			break
		}
		if info.FiveHour != nil {
			break
		}
	}

	if loadResp != nil {
		for _, credit := range loadResp.GetAvailableCredits() {
			info.AICredits = append(info.AICredits, AICredit{
				CreditType:     credit.CreditType,
				Amount:         credit.GetAmount(),
				MinimumBalance: credit.GetMinimumAmount(),
			})
		}
	}

	return info
}

// GetProxyURL 获取账户的代理 URL
func (f *AntigravityQuotaFetcher) GetProxyURL(ctx context.Context, account *Account) string {
	if account.ProxyID == nil || f.proxyRepo == nil {
		return ""
	}
	proxy, err := f.proxyRepo.GetByID(ctx, *account.ProxyID)
	if err != nil || proxy == nil {
		return ""
	}
	return proxy.URL()
}

// classifyForbiddenType 根据 403 响应体判断禁止类型
func classifyForbiddenType(body string) string {
	lower := strings.ToLower(body)
	switch {
	case strings.Contains(lower, "validation_required") ||
		strings.Contains(lower, "verify your account") ||
		strings.Contains(lower, "validation_url"):
		return forbiddenTypeValidation
	case strings.Contains(lower, "terms of service") ||
		strings.Contains(lower, "violation"):
		return forbiddenTypeViolation
	default:
		return forbiddenTypeForbidden
	}
}

// urlPattern 用于从 403 响应体中提取 URL（降级方案）
var urlPattern = regexp.MustCompile(`https://[^\s"'\\]+`)

// extractValidationURL 从 403 响应 JSON 中提取验证/申诉链接
func extractValidationURL(body string) string {
	// 1. 尝试结构化 JSON 提取: /error/details[*]/metadata/validation_url 或 appeal_url
	var parsed struct {
		Error struct {
			Details []struct {
				Metadata map[string]string `json:"metadata"`
			} `json:"details"`
		} `json:"error"`
	}
	if json.Unmarshal([]byte(body), &parsed) == nil {
		for _, detail := range parsed.Error.Details {
			if u := detail.Metadata["validation_url"]; u != "" {
				return u
			}
			if u := detail.Metadata["appeal_url"]; u != "" {
				return u
			}
		}
	}

	// 2. 降级：正则匹配 URL
	lower := strings.ToLower(body)
	if !strings.Contains(lower, "validation") &&
		!strings.Contains(lower, "verify") &&
		!strings.Contains(lower, "appeal") {
		return ""
	}
	// 先解码常见转义再匹配
	normalized := strings.ReplaceAll(body, `\u0026`, "&")
	if m := urlPattern.FindString(normalized); m != "" {
		return m
	}
	return ""
}
