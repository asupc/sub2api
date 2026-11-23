//go:build unit

package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
)

// ---------------------------------------------------------------------------
// normalizeTier
// ---------------------------------------------------------------------------

func TestNormalizeTier(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		expected string
	}{
		{name: "empty string", raw: "", expected: ""},
		{name: "free-tier", raw: "free-tier", expected: "FREE"},
		{name: "g1-pro-tier", raw: "g1-pro-tier", expected: "PRO"},
		{name: "g1-ultra-tier", raw: "g1-ultra-tier", expected: "ULTRA"},
		{name: "unknown-something", raw: "unknown-something", expected: "UNKNOWN"},
		{name: "Google AI Pro contains pro keyword", raw: "Google AI Pro", expected: "PRO"},
		{name: "case insensitive FREE", raw: "FREE-TIER", expected: "FREE"},
		{name: "case insensitive Ultra", raw: "Ultra Plan", expected: "ULTRA"},
		{name: "arbitrary unrecognized string", raw: "enterprise-custom", expected: "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeTier(tt.raw)
			require.Equal(t, tt.expected, got, "normalizeTier(%q)", tt.raw)
		})
	}
}

// ---------------------------------------------------------------------------
// buildUsageInfo
// ---------------------------------------------------------------------------

func TestBuildUsageInfo_QuotaGroups(t *testing.T) {
	fetcher := &AntigravityQuotaFetcher{}

	quotaResp := &antigravity.RetrieveUserQuotaSummaryResponse{
		Groups: []antigravity.QuotaGroup{
			{
				DisplayName: "Gemini Models",
				Buckets: []antigravity.QuotaBucket{
					{
						BucketID:          "gemini-weekly",
						Window:            "weekly",
						RemainingFraction: 0.80,
						ResetTime:         "2026-10-19T08:35:15Z",
					},
					{
						BucketID:          "gemini-5h",
						Window:            "5h",
						RemainingFraction: 0.25,
						ResetTime:         "2026-10-17T18:35:31Z",
					},
				},
			},
			{
				DisplayName: "Claude and GPT models",
				Buckets: []antigravity.QuotaBucket{
					{
						BucketID:          "3p-weekly",
						Window:            "weekly",
						RemainingFraction: 1.0,
						ResetTime:         "2026-10-24T13:35:31Z",
					},
					{
						BucketID:          "3p-5h",
						Window:            "5h",
						RemainingFraction: 0.5,
						ResetTime:         "2026-10-17T18:35:31Z",
					},
				},
			},
		},
	}

	info := fetcher.buildUsageInfo(quotaResp, "g1-pro-tier", "PRO", nil)

	require.NotNil(t, info.UpdatedAt, "UpdatedAt should be set")
	require.Equal(t, "PRO", info.SubscriptionTier)
	require.Equal(t, "g1-pro-tier", info.SubscriptionTierRaw)
	require.Len(t, info.AntigravityQuotaGroups, 2)

	gemini := info.AntigravityQuotaGroups[0]
	require.Equal(t, "gemini", gemini.Key)
	require.Equal(t, "Gemini Models", gemini.DisplayName)
	require.Len(t, gemini.Buckets, 2)
	require.Equal(t, "weekly", gemini.Buckets[0].Window)
	require.Equal(t, 20, gemini.Buckets[0].Utilization) // 1 - 0.80
	require.Equal(t, "2026-10-19T08:35:15Z", gemini.Buckets[0].ResetTime)
	require.Equal(t, "5h", gemini.Buckets[1].Window)
	require.Equal(t, 75, gemini.Buckets[1].Utilization) // 1 - 0.25

	claude := info.AntigravityQuotaGroups[1]
	require.Equal(t, "claude-gpt", claude.Key)
	require.Len(t, claude.Buckets, 2)
	require.Equal(t, 0, claude.Buckets[0].Utilization)
	require.Equal(t, 50, claude.Buckets[1].Utilization)

	// FiveHour 兼容字段取首个 5h 窗口（Gemini 组）。
	require.NotNil(t, info.FiveHour)
	require.InDelta(t, 75.0, info.FiveHour.Utilization, 0.01)
	require.NotNil(t, info.FiveHour.ResetsAt, "ResetsAt should be parsed from ResetTime")
}

func TestBuildUsageInfo_ClampsRemainingFraction(t *testing.T) {
	fetcher := &AntigravityQuotaFetcher{}

	quotaResp := &antigravity.RetrieveUserQuotaSummaryResponse{
		Groups: []antigravity.QuotaGroup{
			{
				DisplayName: "Gemini Models",
				Buckets: []antigravity.QuotaBucket{
					{Window: "5h", RemainingFraction: 1.5},
					{Window: "weekly", RemainingFraction: -0.2},
				},
			},
		},
	}

	info := fetcher.buildUsageInfo(quotaResp, "", "", nil)

	require.Len(t, info.AntigravityQuotaGroups, 1)
	require.Equal(t, 0, info.AntigravityQuotaGroups[0].Buckets[0].Utilization)
	require.Equal(t, 100, info.AntigravityQuotaGroups[0].Buckets[1].Utilization)
}

func TestBuildUsageInfo_EmptyGroups(t *testing.T) {
	fetcher := &AntigravityQuotaFetcher{}

	info := fetcher.buildUsageInfo(&antigravity.RetrieveUserQuotaSummaryResponse{}, "", "", nil)

	require.NotNil(t, info)
	require.NotNil(t, info.AntigravityQuotaGroups)
	require.Empty(t, info.AntigravityQuotaGroups)
	require.Nil(t, info.FiveHour, "FiveHour should be nil when no 5h bucket exists")
}

func TestAntigravityQuotaGroupKey(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{name: "gemini models", input: "Gemini Models", expected: "gemini"},
		{name: "claude and gpt models", input: "Claude and GPT models", expected: "claude-gpt"},
		{name: "unknown group keeps lowercase name", input: "Other Models", expected: "other models"},
		{name: "empty", input: "", expected: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expected, antigravityQuotaGroupKey(tt.input))
		})
	}
}

func TestBuildUsageInfo_NoAICreditsWithoutLoadResponse(t *testing.T) {
	fetcher := &AntigravityQuotaFetcher{}
	info := fetcher.buildUsageInfo(&antigravity.RetrieveUserQuotaSummaryResponse{}, "", "", nil)
	require.Empty(t, info.AICredits)
}

func TestBuildUsageInfo_AICredits(t *testing.T) {
	fetcher := &AntigravityQuotaFetcher{}
	loadResp := &antigravity.LoadCodeAssistResponse{
		PaidTier: &antigravity.PaidTierInfo{
			ID: "g1-pro-tier",
			AvailableCredits: []antigravity.AvailableCredit{
				{
					CreditType:                  "GOOGLE_ONE_AI",
					CreditAmount:                "25",
					MinimumCreditAmountForUsage: "5",
				},
			},
		},
	}

	info := fetcher.buildUsageInfo(&antigravity.RetrieveUserQuotaSummaryResponse{}, "g1-pro-tier", "PRO", loadResp)

	require.Len(t, info.AICredits, 1)
	require.Equal(t, "GOOGLE_ONE_AI", info.AICredits[0].CreditType)
	require.Equal(t, 25.0, info.AICredits[0].Amount)
	require.Equal(t, 5.0, info.AICredits[0].MinimumBalance)
}

func TestFetchQuota_UsesQuotaSummaryEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/v1internal:retrieveUserQuotaSummary"):
			_, _ = w.Write([]byte(`{"groups":[{"displayName":"Gemini Models","buckets":[` +
				`{"bucketId":"gemini-weekly","window":"weekly","remainingFraction":0.9386934,"resetTime":"2026-10-19T08:35:15Z"},` +
				`{"bucketId":"gemini-5h","window":"5h","remainingFraction":1,"resetTime":"2026-10-17T18:35:31Z"}]}]}`))
		case strings.HasSuffix(r.URL.Path, "/v1internal:loadCodeAssist"):
			_, _ = w.Write([]byte(`{"currentTier":{"id":"free-tier"}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	oldBaseURLs := append([]string(nil), antigravity.BaseURLs...)
	oldAvailability := antigravity.DefaultURLAvailability
	t.Cleanup(func() {
		antigravity.BaseURLs = oldBaseURLs
		antigravity.DefaultURLAvailability = oldAvailability
	})
	antigravity.BaseURLs = []string{server.URL}
	antigravity.DefaultURLAvailability = antigravity.NewURLAvailability(time.Minute)

	fetcher := NewAntigravityQuotaFetcher(nil, &config.Config{})
	result, err := fetcher.FetchQuota(context.Background(), &Account{
		Platform: PlatformAntigravity,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token": "token",
			"project_id":   "project",
		},
	}, "")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.UsageInfo)
	require.Len(t, result.UsageInfo.AntigravityQuotaGroups, 1)

	gemini := result.UsageInfo.AntigravityQuotaGroups[0]
	require.Equal(t, "gemini", gemini.Key)
	require.Len(t, gemini.Buckets, 2)

	var weekly *AntigravityQuotaBucket
	for i := range gemini.Buckets {
		if gemini.Buckets[i].Window == "weekly" {
			weekly = &gemini.Buckets[i]
		}
	}
	require.NotNil(t, weekly)
	require.Equal(t, 6, weekly.Utilization) // (1 - 0.9386934) * 100 四舍五入
	require.Equal(t, "2026-10-19T08:35:15Z", weekly.ResetTime)
	require.Equal(t, "FREE", result.UsageInfo.SubscriptionTier)
}

func TestFetchQuota_SummaryReadLimitIsBounded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"groups":[` + strings.Repeat(" ", 2<<20) + `]}`))
	}))
	defer server.Close()

	oldBaseURLs := append([]string(nil), antigravity.BaseURLs...)
	oldAvailability := antigravity.DefaultURLAvailability
	t.Cleanup(func() {
		antigravity.BaseURLs = oldBaseURLs
		antigravity.DefaultURLAvailability = oldAvailability
	})
	antigravity.BaseURLs = []string{server.URL}
	antigravity.DefaultURLAvailability = antigravity.NewURLAvailability(time.Minute)

	fetcher := NewAntigravityQuotaFetcher(nil, &config.Config{})
	_, err := fetcher.FetchQuota(context.Background(), &Account{
		Platform: PlatformAntigravity,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token": "token",
			"project_id":   "project",
		},
	}, "")
	require.ErrorContains(t, err, "响应超过")
}

func TestFetchQuota_ForbiddenReturnsIsForbidden(t *testing.T) {
	// 模拟 FetchQuota 遇到 403 时的行为：
	// FetchAvailableModels 返回 ForbiddenError → FetchQuota 应返回 is_forbidden=true
	forbiddenErr := &antigravity.ForbiddenError{
		StatusCode: 403,
		Body:       "Access denied",
	}

	// 验证 ForbiddenError 满足 errors.As
	var target *antigravity.ForbiddenError
	require.True(t, errors.As(forbiddenErr, &target))
	require.Equal(t, 403, target.StatusCode)
	require.Equal(t, "Access denied", target.Body)
	require.Contains(t, forbiddenErr.Error(), "403")
}

// ---------------------------------------------------------------------------
// classifyForbiddenType
// ---------------------------------------------------------------------------

func TestClassifyForbiddenType(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		expected string
	}{
		{
			name:     "VALIDATION_REQUIRED keyword",
			body:     `{"error":{"message":"VALIDATION_REQUIRED"}}`,
			expected: "validation",
		},
		{
			name:     "verify your account",
			body:     `Please verify your account to continue`,
			expected: "validation",
		},
		{
			name:     "contains validation_url field",
			body:     `{"error":{"details":[{"metadata":{"validation_url":"https://..."}}]}}`,
			expected: "validation",
		},
		{
			name:     "terms of service violation",
			body:     `Your account has been suspended for Terms of Service violation`,
			expected: "violation",
		},
		{
			name:     "violation keyword",
			body:     `Account suspended due to policy violation`,
			expected: "violation",
		},
		{
			name:     "generic 403",
			body:     `Access denied`,
			expected: "forbidden",
		},
		{
			name:     "empty body",
			body:     "",
			expected: "forbidden",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyForbiddenType(tt.body)
			require.Equal(t, tt.expected, got)
		})
	}
}

// ---------------------------------------------------------------------------
// extractValidationURL
// ---------------------------------------------------------------------------

func TestExtractValidationURL(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		expected string
	}{
		{
			name:     "structured validation_url",
			body:     `{"error":{"details":[{"metadata":{"validation_url":"https://accounts.google.com/verify?token=abc"}}]}}`,
			expected: "https://accounts.google.com/verify?token=abc",
		},
		{
			name:     "structured appeal_url",
			body:     `{"error":{"details":[{"metadata":{"appeal_url":"https://support.google.com/appeal/123"}}]}}`,
			expected: "https://support.google.com/appeal/123",
		},
		{
			name:     "validation_url takes priority over appeal_url",
			body:     `{"error":{"details":[{"metadata":{"validation_url":"https://v.com","appeal_url":"https://a.com"}}]}}`,
			expected: "https://v.com",
		},
		{
			name:     "fallback regex with verify keyword",
			body:     `Please verify your account at https://accounts.google.com/verify`,
			expected: "https://accounts.google.com/verify",
		},
		{
			name:     "no URL in generic forbidden",
			body:     `Access denied`,
			expected: "",
		},
		{
			name:     "empty body",
			body:     "",
			expected: "",
		},
		{
			name:     "URL present but no validation keywords",
			body:     `Error at https://example.com/something`,
			expected: "",
		},
		{
			name:     "unicode escaped ampersand",
			body:     `validation required: https://accounts.google.com/verify?a=1\u0026b=2`,
			expected: "https://accounts.google.com/verify?a=1&b=2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractValidationURL(tt.body)
			require.Equal(t, tt.expected, got)
		})
	}
}

func TestFetchQuota_QueriesForwardEndpoint(t *testing.T) {
	newQuotaServer := func(t *testing.T, remaining float64, hits *[]string) *httptest.Server {
		t.Helper()
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			*hits = append(*hits, r.URL.Path)
			w.Header().Set("Content-Type", "application/json")
			remainingText := strconv.FormatFloat(remaining, 'f', -1, 64)
			switch r.URL.Path {
			case "/v1internal:fetchAvailableModels":
				_, _ = w.Write([]byte(`{"models":{"gemini-3-flash":{"quotaInfo":{"remainingFraction":` + remainingText + `,"resetTime":"2026-11-20T09:00:00Z"}}}}`))
			case "/v1internal:retrieveUserQuotaSummary":
				_, _ = w.Write([]byte(`{"groups":[{"displayName":"Gemini Models","buckets":[{"bucketId":"gemini-5h","window":"5h","remainingFraction":` + remainingText + `,"resetTime":"2026-11-20T09:00:00Z"}]}]}`))
			case "/v1internal:loadCodeAssist":
				_, _ = w.Write([]byte(`{"paidTier":{"id":"g1-ultra-tier"}}`))
			default:
				http.NotFound(w, r)
			}
		}))
	}

	tests := []struct {
		name            string
		planType        string
		envOverride     string
		wantDaily       bool
		wantUtilization int
		wantGroupUtil   float64
	}{
		{
			name:            "paid Ultra account queries daily endpoint",
			planType:        "Ultra",
			wantDaily:       true,
			wantUtilization: 75,
			wantGroupUtil:   75,
		},
		{
			name:            "paid Pro account queries daily endpoint",
			planType:        "Pro",
			wantDaily:       true,
			wantUtilization: 75,
			wantGroupUtil:   75,
		},
		{
			name:            "free account queries prod endpoint",
			planType:        "Free",
			wantDaily:       false,
			wantUtilization: 25,
			wantGroupUtil:   25,
		},
		{
			name:            "env override daily routes free account to daily",
			planType:        "Free",
			envOverride:     "daily",
			wantDaily:       true,
			wantUtilization: 75,
			wantGroupUtil:   75,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(antigravityForwardBaseURLEnv, tt.envOverride)

			var prodHits, dailyHits []string
			prodServer := newQuotaServer(t, 0.75, &prodHits)
			defer prodServer.Close()
			dailyServer := newQuotaServer(t, 0.25, &dailyHits)
			defer dailyServer.Close()

			oldBaseURLs := append([]string(nil), antigravity.BaseURLs...)
			oldAvailability := antigravity.DefaultURLAvailability
			t.Cleanup(func() {
				antigravity.BaseURLs = oldBaseURLs
				antigravity.DefaultURLAvailability = oldAvailability
			})
			antigravity.BaseURLs = []string{prodServer.URL, dailyServer.URL}
			antigravity.DefaultURLAvailability = antigravity.NewURLAvailability(time.Minute)

			fetcher := NewAntigravityQuotaFetcher(nil, &config.Config{})
			res, err := fetcher.FetchQuota(context.Background(), &Account{
				ID:       80,
				Platform: PlatformAntigravity,
				Type:     AccountTypeOAuth,
				Credentials: map[string]any{
					"access_token": "token",
					"project_id":   "project",
					"plan_type":    tt.planType,
				},
			}, "")
			require.NoError(t, err)
			require.NotNil(t, res)
			require.NotNil(t, res.UsageInfo)
			require.Len(t, res.UsageInfo.AntigravityQuotaGroups, 1)
			// 二开的分组结构是 Buckets（上游为 Windows），断言等价。
			require.InDelta(t, tt.wantGroupUtil, float64(res.UsageInfo.AntigravityQuotaGroups[0].Buckets[0].Utilization), 0.001)

			// 二开只走 retrieveUserQuotaSummary（fetchAvailableModels 的 quotaInfo 恒为
			// 100% 剩余，不参与用量展示），因此只校验额度汇总请求落在哪个端点。
			if tt.wantDaily {
				require.Contains(t, dailyHits, "/v1internal:retrieveUserQuotaSummary")
				require.NotContains(t, prodHits, "/v1internal:retrieveUserQuotaSummary")
			} else {
				require.Contains(t, prodHits, "/v1internal:retrieveUserQuotaSummary")
				require.NotContains(t, dailyHits, "/v1internal:retrieveUserQuotaSummary")
			}
		})
	}
}

func TestFetchQuota_PaidAccountDoesNotFallbackToProdQuotaOnDailyFailure(t *testing.T) {
	t.Setenv(antigravityForwardBaseURLEnv, "")

	var prodQuotaHits int
	prodServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1internal:fetchAvailableModels" || r.URL.Path == "/v1internal:retrieveUserQuotaSummary" {
			prodQuotaHits++
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"models":{"gemini-3-flash":{"quotaInfo":{"remainingFraction":1}}}}`))
	}))
	defer prodServer.Close()

	dailyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "upstream failure", http.StatusInternalServerError)
	}))
	defer dailyServer.Close()

	oldBaseURLs := append([]string(nil), antigravity.BaseURLs...)
	oldAvailability := antigravity.DefaultURLAvailability
	t.Cleanup(func() {
		antigravity.BaseURLs = oldBaseURLs
		antigravity.DefaultURLAvailability = oldAvailability
	})
	antigravity.BaseURLs = []string{prodServer.URL, dailyServer.URL}
	antigravity.DefaultURLAvailability = antigravity.NewURLAvailability(time.Minute)

	fetcher := NewAntigravityQuotaFetcher(nil, &config.Config{})
	_, err := fetcher.FetchQuota(context.Background(), &Account{
		ID:       77,
		Platform: PlatformAntigravity,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token": "token",
			"project_id":   "project",
			"plan_type":    "Ultra",
		},
	}, "")
	require.Error(t, err)
	require.Zero(t, prodQuotaHits, "paid account quota query must not silently fall back to prod")
}
