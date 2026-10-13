// billing.go billing 域：余额查询（含快过期分桶 / 企业口径）、逐包积分构成、每日签到。
//
// 移植自 workbuddy2api-panel internal/upstream/client.go 的
// UserResourceDetailedWithExpiry / CreditPackages / DailyCheckin 族。
package workbuddy

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"time"
)

// Balance 账号积分余额聚合（所有套餐求和）。
type Balance struct {
	// Remain 剩余积分（负值钳 0）。
	Remain int64
	// Total 总额度（与 Remain 同源；上游缺 size 的套餐按 remain 兜底，保证不超 100%）。
	Total int64
	// Expiring 快过期积分子集（Remain 的一部分）：soon > 0 且套餐到期时刻 ≤ now+soon
	// 的余额计入——调度侧据此优先消耗，避免官方活动赠送的奖励积分到期作废。
	Expiring int64
	// EarliestExpiry 最早的可用到期时刻（无有效批次为零值）。
	EarliestExpiry time.Time
	// EarliestExpiringRemain 与 EarliestExpiry 同一时刻所有正余额包的剩余量之和。
	EarliestExpiringRemain int64
	// Enterprise 账号是否走了企业口径端点（get-user-resource 对企业号恒返回空）。
	Enterprise bool
	// Unlimited 企业版不限量（limitNum == -1）：Remain 用大占位值、Total 记 -1，
	// 两者均不构成「余额不足」限制。
	Unlimited bool
}

// enterpriseUnlimitedRemain / enterpriseUnlimitedTotal 企业版「不限量」的等价表示。
const (
	enterpriseUnlimitedRemain int64 = 1 << 40
	enterpriseUnlimitedTotal  int64 = -1
)

// packageEndLayout 上游套餐到期时间的墙钟格式（UTC+8，与 softRateResetLoc 同口径）。
const packageEndLayout = "2006-01-02 15:04:05"

// UserResourceDetailedWithExpiry 查询账号积分余额与总额度。
//
// 企业版（enterpriseId 非空）分流到企业口径端点 /v2/billing/meter/get-enterprise-user-usage
// （get-user-resource* 对企业号恒返回空 Accounts，实测 code 0 且 Accounts null）。
// soon ≤ 0 时 Expiring 恒 0（禁用分桶）。偶发上游 5xx 做有界重试（见 retryBillingTransient）。
func (c *Client) UserResourceDetailedWithExpiry(ctx context.Context, cred Credentials, soon time.Duration) (*Balance, error) {
	if cred.IsEnterprise() {
		return c.enterpriseResource(ctx, cred, soon)
	}
	now := c.Now()
	body := map[string]any{
		"PageNumber":               1,
		"PageSize":                 100,
		"ProductCode":              "p_tcaca",
		"Status":                   []int{0, 3},
		"PackageEndTimeRangeBegin": now.Format(packageEndLayout),
		"PackageEndTimeRangeEnd":   now.Add(365 * 101 * 24 * time.Hour).Format(packageEndLayout),
	}
	var data json.RawMessage
	err := c.retryBillingTransient(ctx, func() error {
		var e error
		data, e = c.billingMeterJSON(ctx, cred, billingMeterPaths(cred), http.MethodPost, body)
		return e
	})
	if err != nil {
		return nil, err
	}
	// 注意层级：doJSON 已解过 apiEnvelope 并返回 env.Data，这里从 Response 开始解析
	//——不能再套一层 Code/Data，否则 Accounts 恒为空（实测踩过）。
	var resp struct {
		Response struct {
			Data struct {
				Accounts []struct {
					PackageName         string `json:"PackageName"`
					CycleEndTime        string `json:"CycleEndTime"` // "2006-01-02 15:04:05"，缺省/空 = 无到期
					CapacitySize        int64  `json:"CapacitySize"`
					CapacityRemain      int64  `json:"CapacityRemain"`
					CapacityUsed        int64  `json:"CapacityUsed"`
					CycleCapacitySize   int64  `json:"CycleCapacitySize"`
					CycleCapacityRemain int64  `json:"CycleCapacityRemain"`
					CycleCapacityUsed   int64  `json:"CycleCapacityUsed"`
				} `json:"Accounts"`
			} `json:"Data"`
		} `json:"Response"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("resource parse: %w", err)
	}
	out := &Balance{}
	for _, acct := range resp.Response.Data.Accounts {
		r, _, size := packageRemainUsed(respAccount{
			CapacityRemain:      acct.CapacityRemain,
			CapacityUsed:        acct.CapacityUsed,
			CapacitySize:        acct.CapacitySize,
			CycleCapacityRemain: acct.CycleCapacityRemain,
			CycleCapacityUsed:   acct.CycleCapacityUsed,
			CycleCapacitySize:   acct.CycleCapacitySize,
		})
		if r < 0 {
			r = 0
		}
		if size < r {
			size = r
		}
		out.Remain += r
		out.Total += size
		if r <= 0 {
			continue
		}
		end, ok := parsePackageEndTime(acct.CycleEndTime)
		if !ok || !end.After(now) {
			continue
		}
		if out.EarliestExpiry.IsZero() || end.Before(out.EarliestExpiry) {
			out.EarliestExpiry, out.EarliestExpiringRemain = end, r
		} else if end.Equal(out.EarliestExpiry) {
			out.EarliestExpiringRemain += r
		}
		// 分桶：仅 soon>0 且确实在窗口内 → Expiring。
		if soon > 0 && !end.After(now.Add(soon)) {
			out.Expiring += r
		}
	}
	return out, nil
}

// UserResource 简化版余额查询（不分桶）。
func (c *Client) UserResource(ctx context.Context, cred Credentials) (remain, total int64, err error) {
	b, err := c.UserResourceDetailedWithExpiry(ctx, cred, 0)
	if err != nil {
		return 0, 0, err
	}
	return b.Remain, b.Total, nil
}

// enterpriseResource 企业版额度口径。上游（实测 2026-10-07）：
//
//	POST /v2/billing/meter/get-enterprise-user-usage   （X-Enterprise-Id 由 BillingHeaders 注入）
//	{"credit":812.45,"limitNum":2000,
//	 "cycleStartTime":"2026-09-27 00:00:00","cycleEndTime":"2026-10-26 23:59:59",
//	 "cycleResetTime":"2026-10-27 00:00:00"}
//
// credit = 本周期已用（与个人口径 CapacityRemain「剩余」语义相反），limitNum =
// 分配给本账号的额度。故 remain = limitNum - credit、total = limitNum。
// 企业池总额度成员账号无权查询（实测所有池端点 403 not_authorized）。
// 兼容上游两套字段命名（camelCase 与 snake_case）。
func (c *Client) enterpriseResource(ctx context.Context, cred Credentials, soon time.Duration) (*Balance, error) {
	var data json.RawMessage
	err := c.retryBillingTransient(ctx, func() error {
		var e error
		data, e = c.billingJSON(ctx, cred, http.MethodPost, "/v2/billing/meter/get-enterprise-user-usage", map[string]any{})
		return e
	})
	if err != nil {
		return nil, err
	}
	var resp struct {
		Credit         float64 `json:"credit"`
		LimitNum       int64   `json:"limitNum"`
		LimitNumSnake  int64   `json:"limit_num"`
		UsedNum        float64 `json:"used_num"`
		CycleEndTime   string  `json:"cycleEndTime"`
		CycleResetTime string  `json:"cycleResetTime"`
	}
	if uerr := json.Unmarshal(data, &resp); uerr != nil {
		return nil, fmt.Errorf("enterprise resource parse: %w", uerr)
	}
	limit := resp.LimitNum
	if limit == 0 && resp.LimitNumSnake != 0 {
		limit = resp.LimitNumSnake
	}
	used := resp.Credit
	if used == 0 && resp.UsedNum != 0 {
		used = resp.UsedNum
	}
	if limit < 0 { // 不限量
		return &Balance{Remain: enterpriseUnlimitedRemain, Total: enterpriseUnlimitedTotal, Enterprise: true, Unlimited: true}, nil
	}
	// credit 是浮点，四舍五入到最近整数（向下取整会低报剩余额度）。
	usedInt := int64(used)
	if used-float64(usedInt) >= 0.5 {
		usedInt++
	}
	remain := limit - usedInt
	if remain < 0 {
		remain = 0
	}
	out := &Balance{Remain: remain, Total: limit, Enterprise: true}
	// 周期到期批次：cycleEndTime 优先，缺失回落 cycleResetTime。
	now := c.Now()
	end, ok := parsePackageEndTime(resp.CycleEndTime)
	if !ok {
		end, ok = parsePackageEndTime(resp.CycleResetTime)
	}
	if ok && remain > 0 && end.After(now) {
		out.EarliestExpiry, out.EarliestExpiringRemain = end, remain
		if soon > 0 && !end.After(now.Add(soon)) {
			out.Expiring = remain
		}
	}
	return out, nil
}

// respAccount 供 packageRemainUsed 解析的套餐字段。
type respAccount struct {
	CapacityRemain      int64
	CapacityUsed        int64
	CapacitySize        int64
	CycleCapacityRemain int64
	CycleCapacityUsed   int64
	CycleCapacitySize   int64
}

// packageRemainUsed 聚合单套餐的 remain/used/size（单一事实来源）。Cycle 期套餐
// 优先：用 CycleCapacity 三字段，used 取 CycleUsed 与 size-remain 的较大者；
// 否则回退 Capacity 三字段。
func packageRemainUsed(a respAccount) (remain, used, size int64) {
	if a.CycleCapacitySize > 0 {
		remain = a.CycleCapacityRemain
		size = a.CycleCapacitySize
		if remain < 0 {
			remain = 0
		}
		if remain > size {
			remain = size
		}
		used = size - remain
		if a.CycleCapacityUsed > used {
			used = a.CycleCapacityUsed
			if size >= used {
				remain = size - used
			}
		}
		return remain, used, size
	}
	remain = a.CapacityRemain
	used = a.CapacityUsed
	size = a.CapacitySize
	if used == 0 && size > remain {
		used = size - remain
	}
	return remain, used, size
}

// parsePackageEndTime 统一解析上游套餐到期时间。空值、格式异常返回 false，
// 调用方据此保守地不把该包计入最早到期路由。
func parsePackageEndTime(raw string) (time.Time, bool) {
	if raw == "" {
		return time.Time{}, false
	}
	t, err := time.ParseInLocation(packageEndLayout, raw, softRateResetLoc)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// CreditPackage 单个积分包的构成明细（面板「积分构成」用）。
//
// 两个账号即使任务完成度完全一致，余额也可能相差上千——差别藏在包的面额与来源里
// （「国内运营裂变包」「拉新权益包」按次发放，面额 6~1500 不等）。
type CreditPackage struct {
	Name   string `json:"name"`
	Remain int64  `json:"remain"`
	Used   int64  `json:"used"`
	Size   int64  `json:"size"`
	// EndTime 该包的失效时刻：优先 DeductionEndTime（可抵扣窗口结束，真「用不完
	// 就没了」），缺失依次回落 ExpiredTime / PackageEndTime / CycleEndTime（周期
	// 边界，仅兜底）。RFC3339 或上游墙钟字符串。
	EndTime string `json:"end_time,omitempty"`
	// ExpiresAt 与 EndTime 同源的 Unix 毫秒时间戳（按精确剩余天数聚合用）。
	ExpiresAt int64 `json:"expires_at,omitempty"`
	// CreatedAt 发放时刻，RFC3339。这是区分「首登赠送」与「活动奖励」的唯一依据：
	// 两类包的 PackageName 与 PackageCode 完全相同，只有时间能说明它是不是账号
	// 首次授权那刻发的。
	CreatedAt string `json:"created_at,omitempty"`
	// PackageCode / SubProductCode 上游的包类型标识。同 Name 不同 Code 的包可能是
	// 不同来源；同 Code 不同面额则是同来源分批发放。
	PackageCode    string `json:"package_code,omitempty"`
	SubProductCode string `json:"sub_product_code,omitempty"`
	SubProductName string `json:"sub_product_name,omitempty"`
	// Cycle 为 true 表示按周期发放的包（读 Cycle* 字段），否则读 Capacity*。
	Cycle bool `json:"cycle,omitempty"`
}

// CreditPackages 返回账号当前的逐包构成（remain/size 为各包求和）。
// 字段选择与余额聚合口径一致：CycleCapacitySize > 0 时按周期字段算，否则按
// Capacity 字段算——两条路径不能混，否则同一个包会被算两次。
func (c *Client) CreditPackages(ctx context.Context, cred Credentials) ([]CreditPackage, int64, int64, error) {
	now := c.Now()
	body := map[string]any{
		"PageNumber":               1,
		"PageSize":                 100,
		"ProductCode":              "p_tcaca",
		"Status":                   []int{0, 3},
		"PackageEndTimeRangeBegin": now.Format(packageEndLayout),
		"PackageEndTimeRangeEnd":   now.Add(365 * 101 * 24 * time.Hour).Format(packageEndLayout),
	}
	data, err := c.billingMeterJSON(ctx, cred, billingMeterPaths(cred), http.MethodPost, body)
	if err != nil {
		return nil, 0, 0, err
	}
	var resp struct {
		Response struct {
			Data struct {
				Accounts []struct {
					PackageName         string `json:"PackageName"`
					CapacityRemain      int64  `json:"CapacityRemain"`
					CapacityUsed        int64  `json:"CapacityUsed"`
					CapacitySize        int64  `json:"CapacitySize"`
					CycleCapacityRemain int64  `json:"CycleCapacityRemain"`
					CycleCapacityUsed   int64  `json:"CycleCapacityUsed"`
					CycleCapacitySize   int64  `json:"CycleCapacitySize"`
					// 到期时间字段名在上游存在三种口径：ExpiredTime / PackageEndTime
					// 在 CN/global 实测字段全集里均恒 miss，真实下发的是 CycleEndTime
					//——三者都读，谁有值用谁。
					ExpiredTime    string `json:"ExpiredTime"`
					PackageEndTime string `json:"PackageEndTime"`
					CycleEndTime   string `json:"CycleEndTime"`
					// DeductionEndTime 可抵扣窗口结束（epoch 毫秒）——「这个包什么时候
					// 不能再花」的真失效时刻（判「用不完就没了」以本字段为准）。
					DeductionEndTime int64 `json:"DeductionEndTime"`
					// CreateTime 发放时刻（epoch 毫秒）。
					CreateTime     int64  `json:"CreateTime"`
					PackageCode    string `json:"PackageCode"`
					SubProductCode string `json:"SubProductCode"`
					SubProductName string `json:"SubProductName"`
				} `json:"Accounts"`
			} `json:"Data"`
		} `json:"Response"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, 0, 0, fmt.Errorf("packages parse: %w", err)
	}
	packs := resp.Response.Data.Accounts
	out := make([]CreditPackage, 0, len(packs))
	var sumRemain, sumSize int64
	for _, p := range packs {
		cp := CreditPackage{
			Name:           p.PackageName,
			PackageCode:    p.PackageCode,
			SubProductCode: p.SubProductCode,
			SubProductName: p.SubProductName,
		}
		switch {
		case p.DeductionEndTime > 0:
			// 真失效时刻（epoch 毫秒 → RFC3339，与 CycleEndTime 字符串口径共存）。
			// ExpiresAt 直接用原始毫秒——RFC3339 不是 packageEndLayout 形态。
			cp.EndTime = time.UnixMilli(p.DeductionEndTime).Format(time.RFC3339)
			cp.ExpiresAt = p.DeductionEndTime
		case p.ExpiredTime != "":
			cp.EndTime = p.ExpiredTime
		case p.PackageEndTime != "":
			cp.EndTime = p.PackageEndTime
		default:
			cp.EndTime = p.CycleEndTime
		}
		if cp.ExpiresAt == 0 && cp.EndTime != "" {
			if end, perr := time.ParseInLocation(packageEndLayout, cp.EndTime, softRateResetLoc); perr == nil {
				cp.ExpiresAt = end.UnixMilli()
			}
		}
		// CreateTime 是 epoch 毫秒；0 表示上游没给，留空而不是伪造 1970。
		if p.CreateTime > 0 {
			cp.CreatedAt = time.UnixMilli(p.CreateTime).Format(time.RFC3339)
		}
		if p.CycleCapacitySize > 0 {
			cp.Cycle = true
			cp.Remain, cp.Size = p.CycleCapacityRemain, p.CycleCapacitySize
			cp.Used = cp.Size - cp.Remain
			if p.CycleCapacityUsed > cp.Used {
				cp.Used = p.CycleCapacityUsed
				cp.Remain = cp.Size - cp.Used
			}
			if cp.Remain < 0 {
				cp.Remain = 0
			}
		} else {
			cp.Remain, cp.Used, cp.Size = p.CapacityRemain, p.CapacityUsed, p.CapacitySize
			if cp.Used == 0 && cp.Size > cp.Remain {
				cp.Used = cp.Size - cp.Remain
			}
		}
		sumRemain += cp.Remain
		sumSize += cp.Size
		out = append(out, cp)
	}
	// 面额降序：大包一眼可见，正是差异最可能出现的地方。
	sort.SliceStable(out, func(i, j int) bool { return out[i].Size > out[j].Size })
	return out, sumRemain, sumSize, nil
}

// DailyCheckin 执行每日签到。已签到（业务 code 非 0）也返回错误，调用方用
// IsAlreadyCheckin 区分。偶发上游 5xx 做有界重试（单次抖动不再让该账号整天漏签）；
// 「已签到」等业务错误不重试。
func (c *Client) DailyCheckin(ctx context.Context, cred Credentials) error {
	return c.retryBillingTransient(ctx, func() error {
		_, err := c.billingMeterJSON(ctx, cred, checkinMeterPaths(cred), http.MethodPost, map[string]any{})
		return err
	})
}
