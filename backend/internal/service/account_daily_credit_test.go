//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestOpenAIUsageCreditsFromGJSON(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want float64
	}{
		{"qoder 复数 credits 优先", `{"credits":1.25,"credit":9}`, 1.25},
		{"workbuddy 单数 credit 兜底", `{"credit":1.25}`, 1.25},
		{"两者皆缺返回 0", `{"total_tokens":10}`, 0},
		{"零值不产生记录", `{"credits":0,"credit":0}`, 0},
		{"负数视为无效", `{"credits":-3}`, 0},
		{"字符串数值可解析", `{"credits":"2.5"}`, 2.5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, openAIUsageCreditsFromGJSON(parseGJSON(tc.raw)))
		})
	}
}

func TestOpenAIUsageFromGJSONCarriesCredits(t *testing.T) {
	usage, ok := openAIUsageFromGJSON(parseGJSON(`{"prompt_tokens":3,"completion_tokens":2,"credits":0.75}`))
	require.True(t, ok)
	require.Equal(t, 0.75, usage.Credits)
	require.Equal(t, 3, usage.InputTokens)
}

func TestAddOpenAIUsageAccumulatesCredits(t *testing.T) {
	dst := OpenAIUsage{}
	addOpenAIUsage(&dst, OpenAIUsage{Credits: 1.5})
	addOpenAIUsage(&dst, OpenAIUsage{Credits: 0.5})
	require.Equal(t, 2.0, dst.Credits)
}

func TestRecordAccountDailyCreditsPlatformFilter(t *testing.T) {
	repo := &accountDailyCreditRepoStub{}
	svc := &OpenAIGatewayService{}
	svc.SetAccountDailyCreditRepository(repo)

	// 非目标平台不写。
	svc.RecordAccountDailyCredits(context.Background(), &Account{ID: 1, Platform: PlatformTrae}, 5)
	svc.RecordAccountDailyCredits(context.Background(), &Account{ID: 1, Platform: PlatformOpenAI}, 5)
	// 目标平台但 credits <= 0 不写。
	svc.RecordAccountDailyCredits(context.Background(), &Account{ID: 1, Platform: PlatformQoder}, 0)
	require.Empty(t, repo.calls)

	svc.RecordAccountDailyCredits(context.Background(), &Account{ID: 7, Platform: PlatformQoder}, 2.5)
	svc.RecordAccountDailyCredits(context.Background(), &Account{ID: 8, Platform: PlatformWorkbuddy}, 1)
	require.Len(t, repo.calls, 2)
	require.Equal(t, int64(7), repo.calls[0].accountID)
	require.Equal(t, 2.5, repo.calls[0].credits)
	require.Equal(t, int64(8), repo.calls[1].accountID)
	require.Equal(t, 1.0, repo.calls[1].credits)
	require.NotEmpty(t, repo.calls[0].day)
}

func TestRecordAccountDailyCreditsNilRepoIsNoop(t *testing.T) {
	svc := &OpenAIGatewayService{}
	require.NotPanics(t, func() {
		svc.RecordAccountDailyCredits(context.Background(), &Account{ID: 1, Platform: PlatformQoder}, 1)
	})
}

type accountDailyCreditCall struct {
	accountID int64
	day       string
	credits   float64
}

type accountDailyCreditRepoStub struct {
	calls []accountDailyCreditCall
}

func (s *accountDailyCreditRepoStub) AddCredits(_ context.Context, accountID int64, day string, credits float64) error {
	s.calls = append(s.calls, accountDailyCreditCall{accountID: accountID, day: day, credits: credits})
	return nil
}

func (s *accountDailyCreditRepoStub) CreditsByAccounts(_ context.Context, _ []int64, _ string) (map[int64]float64, error) {
	return map[int64]float64{}, nil
}

// parseGJSON 解析测试用 JSON 字符串为 gjson.Result。
func parseGJSON(raw string) gjson.Result {
	return gjson.Parse(raw)
}
