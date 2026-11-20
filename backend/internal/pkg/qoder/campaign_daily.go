// campaign_daily.go 活动平台「每日签到」判据（移植自 trae-workbuddy-switch
// 4a634d6 / qoder2api-hub PR #210）。
//
// 背景：活动平台把「每日签到」与一次性促销体验包（如「极致模型限时体验」）
// 放在同一个列表里下发。旧实现只要 CLAIMED 且有奖励就计入 already，于是历史
// 领过的一次性促销会让账号永久显示「今日已签到」，真正的当日签到反而被跳过。
//
// 判据（与源实现同口径）：
//  1. 必须是积分类且处于投放窗口内；
//  2. 一次性促销词优先排除（不受窗口长短影响——24 小时体验包同样不是每日签到）；
//  3. 标题/描述/活动键含每日签到特征词，或活动实例周期约为 1 天（实测 86340 秒）。
package qoder

import "strings"

// oneTimeCampaignKeywords 一次性促销特征词：命中即判非每日签到。
var oneTimeCampaignKeywords = []string{
	"限时体验", "首购", "体验包", "新人", "新手", "升级包",
	"trial", "welcome", "experience", "promo", "package",
}

// dailyCampaignKeywords 每日签到特征词：标题/描述/活动键任一命中即可。
var dailyCampaignKeywords = []string{
	"每天", "每日", "签到", "打卡",
	"daily", "checkin", "check-in", "check in", "everyday", "every day",
}

// campaignDailyWindowMaxSec 每日活动实例窗口上限（24h 实际为 86340 秒，留余量）。
const campaignDailyWindowMaxSec = 90000

// campaignIsCreditKind 活动是否为积分类（空 kind 视为积分类，对齐源实现）。
func campaignIsCreditKind(item CampaignItem) bool {
	kind := strings.ToUpper(strings.TrimSpace(item.BenefitKind))
	return kind == "" || kind == "CREDITS"
}

// campaignIsActiveAt 活动是否处于投放窗口内；缺窗口视为长期有效（兼容老活动）。
func campaignIsActiveAt(item CampaignItem, nowUnix int64) bool {
	if item.StartAt > 0 && nowUnix < item.StartAt {
		return false
	}
	if item.EndAt > 0 && nowUnix >= item.EndAt {
		return false
	}
	return true
}

// campaignIsDailyCheckinAt 以显式时刻判断是否为「每日签到」类活动（纯函数）。
func campaignIsDailyCheckinAt(item CampaignItem, nowUnix int64) bool {
	if !campaignIsCreditKind(item) || !campaignIsActiveAt(item, nowUnix) {
		return false
	}
	title := strings.ToLower(item.TitleZh + " " + item.TitleEn)
	desc := strings.ToLower(item.DescZh)
	key := strings.ToLower(item.CampaignKey)

	for _, kw := range oneTimeCampaignKeywords {
		if strings.Contains(title, kw) || strings.Contains(desc, kw) {
			return false
		}
	}
	for _, kw := range dailyCampaignKeywords {
		if strings.Contains(title, kw) || strings.Contains(desc, kw) || strings.Contains(key, kw) {
			return true
		}
	}
	if item.StartAt > 0 && item.EndAt > item.StartAt {
		if item.EndAt-item.StartAt <= campaignDailyWindowMaxSec {
			return true
		}
	}
	return false
}
