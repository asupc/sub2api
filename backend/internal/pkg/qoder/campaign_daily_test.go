//go:build unit

package qoder

// campaign_daily_test.go 2026-11-20 移植（trae-workbuddy-switch 4a634d6）回归：
// 每日签到与一次性体验包的区分。

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestCampaignIsDailyCheckin(t *testing.T) {
	now := int64(1_800_000_000)
	daily := CampaignItem{
		TitleZh:     "每日签到领积分",
		BenefitKind: "CREDITS",
		StartAt:     now - 3600,
		EndAt:       now + 3600,
	}
	if !campaignIsDailyCheckinAt(daily, now) {
		t.Fatal("daily checkin must be detected")
	}
	// 标题无特征词但实例窗口 24h（86340s）→ 判每日。
	duration := CampaignItem{
		TitleZh:     "Credits Reward",
		BenefitKind: "CREDITS",
		StartAt:     now - 4320,
		EndAt:       now - 4320 + 86340,
	}
	if !campaignIsDailyCheckinAt(duration, now) {
		t.Fatal("24h window must be treated as daily")
	}
	oneTime := CampaignItem{
		TitleZh:     "极致模型限时体验",
		DescZh:      "限时体验包",
		BenefitKind: "CREDITS",
		StartAt:     now - 3600,
		EndAt:       now + 7*86400,
	}
	if campaignIsDailyCheckinAt(oneTime, now) {
		t.Fatal("one-time promo must not be daily")
	}
	// 24 小时的一次性促销也不得被周期启发式误判。
	oneTimeShort := oneTime
	oneTimeShort.StartAt = now - 100
	oneTimeShort.EndAt = oneTimeShort.StartAt + 86400
	if campaignIsDailyCheckinAt(oneTimeShort, now) {
		t.Fatal("24h one-time promo must not be daily")
	}
	coupon := CampaignItem{
		TitleZh:     "每日签到",
		BenefitKind: "COUPON",
		StartAt:     now - 60,
		EndAt:       now + 60,
	}
	if campaignIsDailyCheckinAt(coupon, now) {
		t.Fatal("non-credit kinds are not checkin")
	}
	// 窗口已结束的历史每日活动不算今天的签到。
	old := daily
	old.StartAt = now - 100000
	old.EndAt = now - 1000
	if campaignIsDailyCheckinAt(old, now) {
		t.Fatal("expired campaign must not count for today")
	}
}

func TestCampaignCheckinClaimedOneTimeGoesToViews(t *testing.T) {
	now := int64(1_800_000_000)
	item := CampaignItem{
		CampaignID:    "p1",
		CampaignKey:   "promo-1",
		TitleZh:       "极致模型限时体验",
		DescZh:        "限时体验包",
		BenefitKind:   "CREDITS",
		BenefitAmount: 500,
		ClaimStatus:   "CLAIMED",
		ActionType:    "CLAIM_BENEFIT",
		StartAt:       now - 3600,
		EndAt:         now + 7*86400,
	}
	if campaignIsDailyCheckinAt(item, now) {
		t.Fatal("fixture must be one-time")
	}
	if !strings.Contains(item.TitleZh, "限时体验") {
		t.Fatal("fixture title changed")
	}
}

// TestCampaignCheckinOneTimeClaimedIsNotAlready 历史已领一次性促销包不得伪装成
// 「今日已签」（4a634d6）：CLAIMED + 有奖励，但标题命中一次性促销词 → views。
func TestCampaignCheckinOneTimeClaimedIsNotAlready(t *testing.T) {
	client, _ := campaignServer(t, map[string]http.HandlerFunc{
		PathCampaigns: jsonHandler(`{"showCampaign":true,"campaigns":[
			{"campaignId":"p1","campaignKey":"promo-1","actionType":"CLAIM_BENEFIT",
				"claimStatus":"CLAIMED","benefit":{"kind":"CREDITS","amount":500},
				"placements":[{"content":{"zh":{"title":"极致模型限时体验","description":"限时体验包"}}}]}]}`),
	})
	out, err := client.CampaignCheckin(context.Background(), CampaignCheckinInput{
		Creds: campaignTestCreds(), OnlyKinds: []string{"", "CREDITS"}, ClaimGap: time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Already) != 0 || len(out.Views) != 1 {
		t.Fatalf("one-time claimed must go to views: %+v", out)
	}
}
