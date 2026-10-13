//go:build unit

package workbuddy

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// userResourcePayload 余额端点样例：两个套餐（周期 + 非周期）。
const userResourcePayload = `{"code":0,"data":{"Response":{"Data":{"Accounts":[
  {"PackageName":"运营裂变包","CycleEndTime":"","CapacityRemain":300,"CapacityUsed":200,"CapacitySize":500,
   "CycleCapacityRemain":120,"CycleCapacityUsed":80,"CycleCapacitySize":200},
  {"PackageName":"拉新权益包","CycleEndTime":"2099-01-01 00:00:00","CapacityRemain":50,"CapacityUsed":0,"CapacitySize":50,
   "CycleCapacityRemain":0,"CycleCapacityUsed":0,"CycleCapacitySize":0}
]}}}}`

// TestUserResourceDetailedWithExpiry 余额聚合：周期套餐优先 + 分桶 + 最早到期。
func TestUserResourceDetailedWithExpiry(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v2/billing/meter/get-user-resource", func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(userResourcePayload))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := newTestClient(srv)

	fixed := time.Unix(1700000000, 0) // 2023-11-14 22:13:20 UTC
	c.now = func() time.Time { return fixed }

	// 个人账号（enterpriseId 非空会分流企业口径端点）。
	cred := cnCred()
	cred.EnterpriseID = ""
	b, err := c.UserResourceDetailedWithExpiry(context.Background(), cred, 24*time.Hour)
	if err != nil {
		t.Fatalf("UserResourceDetailedWithExpiry: %v", err)
	}
	if gotPath != "/v2/billing/meter/get-user-resource" {
		t.Errorf("CN 应走 /v2 路径，got %s", gotPath)
	}
	if gotBody["ProductCode"] != "p_tcaca" || gotBody["PageSize"] != float64(100) {
		t.Errorf("请求体关键字段: %+v", gotBody)
	}
	// 套餐 1：周期字段优先（remain=120,size=200）；套餐 2：Capacity（50/50）。
	if b.Remain != 170 || b.Total != 250 {
		t.Errorf("Remain=%d Total=%d want 170/250", b.Remain, b.Total)
	}
	// 2099-01-01 到期，24h 窗口外 → 不分桶。
	if b.Expiring != 0 {
		t.Errorf("Expiring=%d want 0", b.Expiring)
	}
	if b.Enterprise {
		t.Error("个人账号不应走企业口径")
	}
	if b.EarliestExpiry.IsZero() {
		t.Error("应解析出最早到期时刻")
	}

	// soon 覆盖 2099 之前的全部 → 分桶命中（用超大窗口验证分桶逻辑本身）。
	b2, err := c.UserResourceDetailedWithExpiry(context.Background(), cred, 100*365*24*time.Hour)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if b2.Expiring != 50 {
		t.Errorf("超大窗口 Expiring=%d want 50（仅套餐 2 到期在窗口内）", b2.Expiring)
	}
}

// TestUserResourceGlobalPathFallback global 双路径 fallback：无 /v2 前缀优先，404 再换 /v2。
func TestUserResourceGlobalPathFallback(t *testing.T) {
	var hitsV2Less atomic.Bool
	mux := http.NewServeMux()
	mux.HandleFunc("POST /billing/meter/get-user-resource", func(w http.ResponseWriter, r *http.Request) {
		hitsV2Less.Store(true)
		w.WriteHeader(http.StatusNotFound) // 模拟无 /v2 路径不存在
		_, _ = w.Write([]byte(`{"code":0,"msg":"not found"}`))
	})
	mux.HandleFunc("POST /v2/billing/meter/get-user-resource", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(userResourcePayload))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := newTestClient(srv)

	b, err := c.UserResourceDetailedWithExpiry(context.Background(), globalCred(), 0)
	if err != nil {
		t.Fatalf("global fallback: %v", err)
	}
	if !hitsV2Less.Load() {
		t.Error("global 应先打无 /v2 前缀路径")
	}
	if b.Remain != 170 {
		t.Errorf("fallback 后 Remain=%d want 170", b.Remain)
	}
}

// TestEnterpriseResource 企业口径：credit=已用 / limitNum=额度 / 不限量分支。
func TestEnterpriseResource(t *testing.T) {
	var gotEnt, gotTenant string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/billing/meter/get-enterprise-user-usage" {
			t.Errorf("企业端点路径=%s", r.URL.Path)
		}
		gotEnt = r.Header.Get("X-Enterprise-Id")
		gotTenant = r.Header.Get("X-Tenant-Id")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"credit":812.45,"limitNum":2000,
		  "cycleEndTime":"2099-06-30 23:59:59","cycleResetTime":"2099-07-01 00:00:00"}}`))
	}))
	defer srv.Close()
	c := newTestClient(srv)
	cred := cnCred()
	cred.EnterpriseID = "a6239ec8"

	b, err := c.UserResourceDetailedWithExpiry(context.Background(), cred, 0)
	if err != nil {
		t.Fatalf("enterprise: %v", err)
	}
	if gotEnt != "a6239ec8" || gotTenant != "a6239ec8" {
		t.Errorf("企业头 X-Enterprise-Id=%q X-Tenant-Id=%q", gotEnt, gotTenant)
	}
	// remain = 2000 - round(812.45) = 2000-812 = 1188。
	if b.Remain != 1188 || b.Total != 2000 {
		t.Errorf("企业 Remain=%d Total=%d want 1188/2000", b.Remain, b.Total)
	}
	if !b.Enterprise {
		t.Error("应标记企业口径")
	}
	if b.EarliestExpiry.IsZero() {
		t.Error("周期到期批次应解析")
	}

	// 不限量：limitNum=-1。
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":0,"data":{"credit":10,"limitNum":-1}}`))
	}))
	defer srv2.Close()
	c2 := newTestClient(srv2)
	b2, err := c2.UserResourceDetailedWithExpiry(context.Background(), cred, 0)
	if err != nil {
		t.Fatalf("unlimited: %v", err)
	}
	if !b2.Unlimited || b2.Total != enterpriseUnlimitedTotal || b2.Remain != enterpriseUnlimitedRemain {
		t.Errorf("不限量标记错误: %+v", b2)
	}
}

// TestCreditPackages 逐包构成：请求体 + 解析 + DeductionEndTime 优先。
func TestCreditPackages(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"Response":{"Data":{"Accounts":[
		  {"PackageName":"国内运营裂变包","CapacityRemain":1500,"CapacityUsed":0,"CapacitySize":1500,
		   "CycleCapacityRemain":0,"CycleCapacityUsed":0,"CycleCapacitySize":0,
		   "DeductionEndTime":1789999999000,"CreateTime":1700000000000,
		   "PackageCode":"TCACA_code_007_x","SubProductCode":"sp1","SubProductName":"裂变"},
		  {"PackageName":"周期包","CapacityRemain":0,"CapacityUsed":0,"CapacitySize":0,
		   "CycleCapacityRemain":80,"CycleCapacityUsed":20,"CycleCapacitySize":100,
		   "CycleEndTime":"2099-03-01 00:00:00"}
		]}}}}`))
	}))
	defer srv.Close()
	c := newTestClient(srv)

	packs, sumRemain, sumSize, err := c.CreditPackages(context.Background(), cnCred())
	if err != nil {
		t.Fatalf("CreditPackages: %v", err)
	}
	if gotBody["ProductCode"] != "p_tcaca" {
		t.Errorf("请求体 ProductCode=%v", gotBody["ProductCode"])
	}
	if len(packs) != 2 {
		t.Fatalf("packs=%d want 2", len(packs))
	}
	// 面额降序：大包在前。
	if packs[0].Size < packs[1].Size {
		t.Errorf("应面额降序: %d before %d", packs[0].Size, packs[1].Size)
	}
	if packs[0].Name != "国内运营裂变包" || packs[0].Remain != 1500 || packs[0].PackageCode != "TCACA_code_007_x" {
		t.Errorf("裂变包字段: %+v", packs[0])
	}
	if packs[0].ExpiresAt != 1789999999000 {
		t.Errorf("DeductionEndTime 应优先: %d", packs[0].ExpiresAt)
	}
	if !packs[1].Cycle || packs[1].Remain != 80 || packs[1].Size != 100 {
		t.Errorf("周期包字段: %+v", packs[1])
	}
	if sumRemain != 1580 || sumSize != 1600 {
		t.Errorf("sum=%d/%d want 1580/1600", sumRemain, sumSize)
	}
}

// TestDailyCheckinPaths 签到路径（CN 单路径 / global fallback）与已签到识别。
func TestDailyCheckinPaths(t *testing.T) {
	var cnPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cnPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{}}`))
	}))
	defer srv.Close()
	c := newTestClient(srv)
	if err := c.DailyCheckin(context.Background(), cnCred()); err != nil {
		t.Fatalf("DailyCheckin: %v", err)
	}
	if cnPath != "/v2/billing/meter/daily-checkin" {
		t.Errorf("CN 签到路径=%s", cnPath)
	}

	// 已签到：业务 code 非 0 + 文案 → IsAlreadyCheckin 命中。
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":10001,"msg":"今天已签到"}`))
	}))
	defer srv2.Close()
	c2 := newTestClient(srv2)
	err := c2.DailyCheckin(context.Background(), cnCred())
	if err == nil {
		t.Fatal("已签到应返回错误")
	}
	if !IsAlreadyCheckin(err) {
		t.Errorf("应识别为已签到: %v", err)
	}

	// global：无前缀 404 → /v2 fallback 成功。
	var globalTried [2]bool
	mux := http.NewServeMux()
	mux.HandleFunc("POST /billing/meter/daily-checkin", func(w http.ResponseWriter, r *http.Request) {
		globalTried[0] = true
		w.WriteHeader(http.StatusNotFound)
	})
	mux.HandleFunc("POST /v2/billing/meter/daily-checkin", func(w http.ResponseWriter, r *http.Request) {
		globalTried[1] = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{}}`))
	})
	srv3 := httptest.NewServer(mux)
	defer srv3.Close()
	c3 := newTestClient(srv3)
	if err := c3.DailyCheckin(context.Background(), globalCred()); err != nil {
		t.Fatalf("global checkin fallback: %v", err)
	}
	if !globalTried[0] || !globalTried[1] {
		t.Errorf("global fallback 双路径尝试: %v", globalTried)
	}
}

// TestDailyCheckinTransientRetry 偶发 5xx 有界重试（缩短间隔避免测试慢）。
func TestDailyCheckinTransientRetry(t *testing.T) {
	orig := billingRetryDelay
	billingRetryDelay = time.Millisecond
	defer func() { billingRetryDelay = orig }()

	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"code":10000,"msg":"API request failed with status code: 500"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{}}`))
	}))
	defer srv.Close()
	c := newTestClient(srv)
	if err := c.DailyCheckin(context.Background(), cnCred()); err != nil {
		t.Fatalf("重试后应成功: %v", err)
	}
	if calls != 2 {
		t.Errorf("calls=%d want 2", calls)
	}
}
