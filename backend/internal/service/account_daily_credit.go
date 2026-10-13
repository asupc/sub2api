package service

// account_daily_credit.go 二开新增：qoder / workbuddy 账号每日积分消耗统计。
//
// 背景：这两个平台按积分计费（上游 usage 帧带本次请求消耗），但账号列表的
// 「今日统计」列是美元口径（usage_logs 聚合），账号 extra 的 wb_quota / qd_quota
// 只有余额快照——都看不到「今日消耗了多少积分」。本文件提供共用的：
//   - 上游积分字段解析（credits 复数优先、credit 单数兜底）；
//   - 日累计仓库的窄接口（service 层定义，repository 实现，避免循环依赖）；
//   - 记账入口 RecordAccountDailyCredits（供 OpenAIGatewayService.RecordUsage 调用）。
//
// 存储见 migrations/242_so_account_daily_credits.sql，日期口径与 timezone.Today() 同源。

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/tidwall/gjson"
)

// AccountDailyCreditRepository 每日积分累计的最小读写接口（repository 实现）。
type AccountDailyCreditRepository interface {
	// AddCredits 把一次请求消耗的积分累加到 (accountID, day)。
	AddCredits(ctx context.Context, accountID int64, day string, credits float64) error
	// CreditsByAccounts 批量读取指定账号当日的积分消耗（无记录不返回该键）。
	CreditsByAccounts(ctx context.Context, accountIDs []int64, day string) (map[int64]float64, error)
}

// SetAccountDailyCreditRepository 装配每日积分仓库（构造后调用；先例
// SetPluginManager / SetWorkbuddyOAuthService——不动构造签名，避免波及测试 stub）。
func (s *OpenAIGatewayService) SetAccountDailyCreditRepository(repo AccountDailyCreditRepository) {
	if s == nil {
		return
	}
	s.accountDailyCreditRepo = repo
}

// AccountDailyCreditsDay 返回统计日字符串（timezone.Today() 的 yyyy-mm-dd）。
// 读写两侧共用，保证跨日切分口径一致。
func AccountDailyCreditsDay() string {
	return timezone.Today().Format("2006-01-02")
}

// isAccountDailyCreditsPlatform 报告平台是否参与每日积分统计。
// 仅 qoder / workbuddy（trae 的积分语义与额度快照不同，不在本期范围）。
func isAccountDailyCreditsPlatform(platform string) bool {
	return platform == PlatformQoder || platform == PlatformWorkbuddy
}

// openAIUsageCreditsFromGJSON 解析上游 usage 里的本次积分消耗。
// qoder 上游给的是复数 credits（qoder2api-hub qoder_proxy.py L394-398 实测：
// 单数 credit 恒为 None，历史数据无法回填）；workbuddy 给的是单数 credit。
// 两者互为兜底，防御不同区域/版本的字段差异；字段缺失或非正数返回 0。
func openAIUsageCreditsFromGJSON(value gjson.Result) float64 {
	if !value.Exists() || !value.IsObject() {
		return 0
	}
	credits := value.Get("credits").Float()
	if credits <= 0 {
		credits = value.Get("credit").Float()
	}
	if credits <= 0 {
		return 0
	}
	return credits
}

// RecordAccountDailyCredits 把一次上游请求的积分消耗记入账号日累计（best-effort）。
// 仅 qoder / workbuddy 平台、credits > 0 时落库；失败只记日志——统计缺一条
// 不应影响请求结果（与 usage_log 的 best-effort 语义一致）。
func (s *OpenAIGatewayService) RecordAccountDailyCredits(ctx context.Context, account *Account, credits float64) {
	if s == nil || s.accountDailyCreditRepo == nil || account == nil {
		return
	}
	if !isAccountDailyCreditsPlatform(account.Platform) || credits <= 0 {
		return
	}
	if err := s.accountDailyCreditRepo.AddCredits(ctx, account.ID, AccountDailyCreditsDay(), credits); err != nil {
		logger.LegacyPrintf("service.openai_gateway", "[AccountDailyCredits] add failed: account=%d credits=%v err=%v", account.ID, credits, err)
	}
}
