//go:build unit

package workbuddy

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// shortenGaps 缩短动作节奏常量（避免测试慢），返回恢复函数。
func shortenGaps() func() {
	origReport, origPoll, origPollN, origNight := reportGap, claimPollGap, claimPollAttempts, nightChatGap
	reportGap, claimPollGap, claimPollAttempts, nightChatGap = time.Millisecond, time.Millisecond, 2, time.Millisecond
	return func() {
		reportGap, claimPollGap, claimPollAttempts, nightChatGap = origReport, origPoll, origPollN, origNight
	}
}

// tasksListPayload 成长任务列表样例（progress 对象 + 平铺双形态）。
const tasksListPayload = `{"code":0,"data":{"tasks":[
  {"task_code":"chat_5","title":"每日对话","reward_credit":100,"reward_energy":5,
   "target":5,"current":5,"accept_status":"accepted"},
  {"task_code":"first_buddy","reward_credit":300,
   "progress":{"current":0,"target":1},"accept_status":"not_accepted"},
  {"task_code":"black_cat","title":"夜猫子","reward_credit":200,
   "progress":{"current":1,"target":3},"accept_status":"claimed"},
  {"task_code":"locked_task","locked":true,"target":1,"current":0,"accept_status":"not_accepted"}
]}}`

// TestListTasks 任务列表解析：progress 对象覆盖 + claimable/claimed 推算。
func TestListTasks(t *testing.T) {
	var gotPlatform string
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v2/activity/growth/tasks", func(w http.ResponseWriter, r *http.Request) {
		gotPlatform = r.Header.Get("X-Client-Platform")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(tasksListPayload))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := newTestClient(srv)

	tasks, err := c.ListTasks(context.Background(), cnCred())
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if gotPlatform != "" {
		t.Errorf("默认口径不应带 mp 头，got %q", gotPlatform)
	}
	byCode := map[string]Task{}
	for _, t := range tasks {
		byCode[t.TaskCode] = t
	}
	// 平铺形态：5/5 达标未领 → claimable。
	if !byCode["chat_5"].Claimable || byCode["chat_5"].Claimed {
		t.Errorf("chat_5 应 claimable: %+v", byCode["chat_5"])
	}
	if byCode["chat_5"].Credit != 100 || byCode["chat_5"].Energy != 5 {
		t.Errorf("奖励字段: %+v", byCode["chat_5"])
	}
	// progress 对象形态覆盖平铺（0/1）。
	if byCode["first_buddy"].Target != 1 || byCode["first_buddy"].Current != 0 {
		t.Errorf("progress 对象未生效: %+v", byCode["first_buddy"])
	}
	// claimed 任务不再 claimable。
	if byCode["black_cat"].Claimable || !byCode["black_cat"].Claimed {
		t.Errorf("black_cat 应 claimed: %+v", byCode["black_cat"])
	}
	if byCode["black_cat"].Current != 1 || byCode["black_cat"].Target != 3 {
		t.Errorf("black_cat 进度: %+v", byCode["black_cat"])
	}
}

// TestListTasksMP mp 口径带 X-Client-Platform 头。
func TestListTasksMP(t *testing.T) {
	var gotPlatform string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPlatform = r.Header.Get("X-Client-Platform")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(tasksListPayload))
	}))
	defer srv.Close()
	c := newTestClient(srv)
	if _, err := c.ListTasksMP(context.Background(), cnCred()); err != nil {
		t.Fatalf("ListTasksMP: %v", err)
	}
	if gotPlatform != mpPlatform {
		t.Errorf("mp 口径头=%q want %q", gotPlatform, mpPlatform)
	}
}

// TestAcceptTasks 接受任务请求体。
func TestAcceptTasks(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != tasksAcceptPath {
			t.Errorf("accept 路径=%s", r.URL.Path)
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{}}`))
	}))
	defer srv.Close()
	c := newTestClient(srv)
	if err := c.AcceptTasks(context.Background(), cnCred(), []string{"a", "b"}); err != nil {
		t.Fatalf("AcceptTasks: %v", err)
	}
	if arr, ok := gotBody["task_codes"].([]any); !ok || len(arr) != 2 || arr[0] != "a" {
		t.Errorf("task_codes=%v", gotBody["task_codes"])
	}
}

// TestClaimReward Web 域领奖：路径 + web 平台头 + 幂等。
func TestClaimReward(t *testing.T) {
	var gotPath, gotPlatform, gotReferer string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotPlatform = r.Header.Get("x-client-platform")
		gotReferer = r.Header.Get("Referer")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"already_claimed":false,"credit":300,"energy":8}}`))
	}))
	defer srv.Close()
	c := newTestClient(srv)

	credit, energy, err := c.ClaimReward(context.Background(), cnCred(), "first_buddy")
	if err != nil {
		t.Fatalf("ClaimReward: %v", err)
	}
	if gotPath != "/activity/growth/tasks/first_buddy/claim" {
		t.Errorf("领奖路径=%s", gotPath)
	}
	if gotPlatform != "web" {
		t.Errorf("x-client-platform=%q", gotPlatform)
	}
	if !strings.Contains(gotReferer, "growth-center") {
		t.Errorf("Referer=%q", gotReferer)
	}
	if credit != 300 || energy != 8 {
		t.Errorf("奖励=%d/%d", credit, energy)
	}

	// 已领取：幂等零值。
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":0,"data":{"already_claimed":true,"credit":0,"energy":0}}`))
	}))
	defer srv2.Close()
	c2 := newTestClient(srv2)
	credit, energy, err = c2.ClaimReward(context.Background(), cnCred(), "first_buddy")
	if err != nil || credit != 0 || energy != 0 {
		t.Errorf("已领取应幂等 (0,0,nil)，got (%d,%d,%v)", credit, energy, err)
	}
}

// TestClaimRewardMP chat 域 400 降级 Web 域。
func TestClaimRewardMP(t *testing.T) {
	// chat 域与 web 基址都被重写到同一测试服务器，两次请求路径相同，
	// 靠平台头区分：第一次 mp（chat 域），第二次 web（降级）。
	var platforms []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		platforms = append(platforms, r.Header.Get("x-client-platform"))
		if len(platforms) == 1 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"code":10001,"msg":"task not completed"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"credit":100,"energy":5}}`))
	}))
	defer srv.Close()
	c := newTestClient(srv)
	credit, energy, err := c.ClaimRewardMP(context.Background(), cnCred(), "school_season")
	if err != nil {
		t.Fatalf("ClaimRewardMP 降级: %v", err)
	}
	if len(platforms) != 2 || platforms[0] != mpPlatform || platforms[1] != "web" {
		t.Errorf("两次尝试平台头=%v（第一次 mp、第二次 web）", platforms)
	}
	if credit != 100 || energy != 5 {
		t.Errorf("降级领奖=%d/%d", credit, energy)
	}
}

// TestReportChatActivity 上报事件形状（全字段 + userId 必带）。
func TestReportChatActivity(t *testing.T) {
	var gotPath string
	var events []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		var batch []map[string]any
		_ = json.Unmarshal(raw, &batch)
		events = append(events, batch...)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{}}`))
	}))
	defer srv.Close()
	c := newTestClient(srv)

	if err := c.ReportChatActivity(context.Background(), cnCred(), "wb2api-chat5-1", ""); err != nil {
		t.Fatalf("ReportChatActivity: %v", err)
	}
	if gotPath != "/v2/report" {
		t.Errorf("上报路径=%s", gotPath)
	}
	if len(events) != 1 {
		t.Fatalf("事件数组长度=%d want 1", len(events))
	}
	ev := events[0]
	if ev["eventCode"] != "chat_request_send" || ev["userId"] != "uid-1" {
		t.Errorf("eventCode/userId: %v/%v", ev["eventCode"], ev["userId"])
	}
	if ev["mode"] != "craft" || ev["agentType"] != "conversation" {
		t.Errorf("mode/agentType: %v/%v", ev["mode"], ev["agentType"])
	}
	// requestID 空回落 conversationID。
	if ev["requestId"] != "wb2api-chat5-1" || ev["rootRequestId"] != "wb2api-chat5-1" {
		t.Errorf("requestID 回落: %v", ev["requestId"])
	}
	if ev["requestModelId"] != defaultReportModelID {
		t.Errorf("默认模型: %v", ev["requestModelId"])
	}
	// 指定模型对齐。
	if err := c.ReportChatActivityModel(context.Background(), cnCred(), "c1", "r1", "glm-5.2", "GLM-5.2"); err != nil {
		t.Fatalf("ReportChatActivityModel: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("两次上报 events=%d want 2", len(events))
	}
	if events[1]["requestModelId"] != "glm-5.2" || events[1]["requestId"] != "r1" {
		t.Errorf("模型对齐: %v", events[1])
	}
}

// TestRunTravelRound 旅行状态机：arrived→领奖 / idle→派出 / traveling→跳过。
func TestRunTravelRound(t *testing.T) {
	var state string
	var departBody, claimBody map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("GET /activity/growth/buddy/info", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"buddy":{"id":7,"name":"小橘"}}}`))
	})
	mux.HandleFunc("GET /activity/growth/buddy/travel/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"state":"` + state + `","daily_limit_reached":false,"record_id":42,"reward_credit":30}}`))
	})
	mux.HandleFunc("POST /activity/growth/buddy/travel/depart", func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &departBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{}}`))
	})
	mux.HandleFunc("POST /activity/growth/buddy/travel/claim", func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &claimBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"reward_credit":30}}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := newTestClient(srv)
	defer shortenGaps()()

	state = TravelStateArrived
	msg, err := c.RunTravelRound(context.Background(), cnCred())
	if err != nil || !strings.Contains(msg, "claim ok") {
		t.Fatalf("arrived 应领奖: %v %q", err, msg)
	}
	if claimBody["record_id"] != float64(42) {
		t.Errorf("claim 应带 record_id: %v", claimBody)
	}

	state = TravelStateIdle
	msg, err = c.RunTravelRound(context.Background(), cnCred())
	if err != nil || !strings.Contains(msg, "depart ok") {
		t.Fatalf("idle 应派出: %v %q", err, msg)
	}
	if departBody["location_id"] != float64(TravelLocationID) {
		t.Errorf("派出地点=%v want %d", departBody["location_id"], TravelLocationID)
	}

	state = TravelStateTraveling
	msg, err = c.RunTravelRound(context.Background(), cnCred())
	if err != nil || !strings.Contains(msg, "skip") {
		t.Fatalf("traveling 应跳过: %v %q", err, msg)
	}
}

// TestRunTravelRoundAdopt 无猫领养链：report → agreement → first。
func TestRunTravelRoundAdopt(t *testing.T) {
	var reportSeen, agreementSeen, firstSeen bool
	mux := http.NewServeMux()
	mux.HandleFunc("GET /activity/growth/buddy/info", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":0,"data":{"buddy":null}}`))
	})
	mux.HandleFunc("POST /v2/report", func(w http.ResponseWriter, r *http.Request) {
		reportSeen = true
		_, _ = w.Write([]byte(`{"code":0,"data":{}}`))
	})
	mux.HandleFunc("POST /activity/growth/buddy/agreement", func(w http.ResponseWriter, r *http.Request) {
		agreementSeen = true
		_, _ = w.Write([]byte(`{"code":0,"data":{}}`))
	})
	mux.HandleFunc("POST /activity/growth/buddy/first", func(w http.ResponseWriter, r *http.Request) {
		firstSeen = true
		_, _ = w.Write([]byte(`{"code":0,"data":{}}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := newTestClient(srv)
	defer shortenGaps()()

	msg, err := c.RunTravelRound(context.Background(), cnCred())
	if err != nil || !strings.Contains(msg, "adopt ok") {
		t.Fatalf("无猫应领养: %v %q", err, msg)
	}
	if !reportSeen || !agreementSeen || !firstSeen {
		t.Errorf("领养链三步: report=%v agreement=%v first=%v", reportSeen, agreementSeen, firstSeen)
	}
}

// TestInNightWindow 夜猫子窗口判定。
func TestInNightWindow(t *testing.T) {
	cases := []struct {
		hour int
		want bool
	}{{22, false}, {23, true}, {0, true}, {7, true}, {8, false}, {12, false}}
	for _, c := range cases {
		now := time.Date(2026, 10, 12, c.hour, 0, 0, 0, time.Local)
		if got := InNightWindow(now); got != c.want {
			t.Errorf("hour=%d InNightWindow=%v want %v", c.hour, got, c.want)
		}
	}
}

// TestBlackcatNeed 夜猫子差额。
func TestBlackcatNeed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(tasksListPayload))
	}))
	defer srv.Close()
	c := newTestClient(srv)
	// black_cat 1/3 已 claimed → 0。
	need, err := c.BlackcatNeed(context.Background(), cnCred())
	if err != nil || need != 0 {
		t.Errorf("claimed 任务 need=%d err=%v want 0", need, err)
	}
}

// TestCompleteChatReportTask chat_5 按差额补报。
func TestCompleteChatReportTask(t *testing.T) {
	defer shortenGaps()()
	var reports int
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v2/activity/growth/tasks", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":0,"data":{"tasks":[
		  {"task_code":"chat_5","target":5,"current":2,"accept_status":"accepted"}]}}`))
	})
	mux.HandleFunc("POST /v2/report", func(w http.ResponseWriter, r *http.Request) {
		reports++
		_, _ = w.Write([]byte(`{"code":0,"data":{}}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := newTestClient(srv)

	n, err := c.CompleteChatReportTask(context.Background(), cnCred())
	if err != nil {
		t.Fatalf("CompleteChatReportTask: %v", err)
	}
	if n != 3 || reports != 3 {
		t.Errorf("差额补报 n=%d reports=%d want 3/3", n, reports)
	}
}

// TestAcceptPendingGrowthTasks 批量接受（跳过 accepted/claimed/locked + mp 补充）。
func TestAcceptPendingGrowthTasks(t *testing.T) {
	var acceptedDefault, acceptedMP []string
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v2/activity/growth/tasks", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Client-Platform") != "" {
			// mp 口径下发 mp 专属任务。
			_, _ = w.Write([]byte(`{"code":0,"data":{"tasks":[
			  {"task_code":"Sequential_Tasks_1","target":1,"current":0,"accept_status":"not_accepted"},
			  {"task_code":"chat_5","target":5,"current":5,"accept_status":"accepted"}]}}`))
			return
		}
		_, _ = w.Write([]byte(tasksListPayload))
	})
	mux.HandleFunc("POST /v2/activity/growth/tasks/accept", func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		for _, v := range body["task_codes"].([]any) {
			if r.Header.Get("X-Client-Platform") != "" {
				acceptedMP = append(acceptedMP, v.(string))
			} else {
				acceptedDefault = append(acceptedDefault, v.(string))
			}
		}
		_, _ = w.Write([]byte(`{"code":0,"data":{}}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := newTestClient(srv)

	n, err := c.AcceptPendingGrowthTasks(context.Background(), cnCred())
	if err != nil {
		t.Fatalf("AcceptPendingGrowthTasks: %v", err)
	}
	// 默认口径：first_buddy（not_accepted）；跳过 chat_5（accepted）、black_cat（claimed）、locked_task。
	if len(acceptedDefault) != 1 || acceptedDefault[0] != "first_buddy" {
		t.Errorf("默认口径接受=%v", acceptedDefault)
	}
	// mp 口径：Sequential_Tasks_1；跳过 mp 列表里的 chat_5（已在默认去重集合）。
	if len(acceptedMP) != 1 || acceptedMP[0] != "Sequential_Tasks_1" {
		t.Errorf("mp 口径接受=%v", acceptedMP)
	}
	if n != 2 {
		t.Errorf("接受总数=%d want 2", n)
	}
}

// TestGrowthStreak 连登天数解析。
func TestGrowthStreak(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != streakPath {
			t.Errorf("streak 路径=%s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"code":0,"data":{"streak":{"days":12},"makeup_cards":{"balance":1,"max":3}}}`))
	}))
	defer srv.Close()
	c := newTestClient(srv)
	days, err := c.GrowthStreak(context.Background(), cnCred())
	if err != nil || days != 12 {
		t.Fatalf("GrowthStreak=%d err=%v want 12", days, err)
	}
	full, err := c.GrowthStreakFull(context.Background(), cnCred())
	if err != nil || full.Streak.Days != 12 || full.MakeupCards.Balance != 1 {
		t.Fatalf("GrowthStreakFull: %+v err=%v", full, err)
	}
}

// TestRunGrowthAction 成长任务闭环：动作 → 回读 → 自动领奖。
func TestRunGrowthAction(t *testing.T) {
	defer shortenGaps()()
	var current int64 = 2
	var claimed bool
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v2/activity/growth/tasks", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"tasks":[
		  {"task_code":"chat_5","target":3,"current":` + itoa(current) + `,"accept_status":"accepted"}]}}`))
	})
	mux.HandleFunc("POST /v2/report", func(w http.ResponseWriter, r *http.Request) {
		current++
		_, _ = w.Write([]byte(`{"code":0,"data":{}}`))
	})
	mux.HandleFunc("POST /activity/growth/tasks/chat_5/claim", func(w http.ResponseWriter, r *http.Request) {
		claimed = true
		_, _ = w.Write([]byte(`{"code":0,"data":{"already_claimed":false,"credit":100,"energy":5}}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := newTestClient(srv)

	res, err := c.RunGrowthAction(context.Background(), cnCred(), "chat_5", false)
	if err != nil {
		t.Fatalf("RunGrowthAction: %v", err)
	}
	if !res.Claimed || res.Credit != 100 || res.Energy != 5 {
		t.Errorf("自动领奖结果: %+v", res)
	}
	if !claimed {
		t.Error("应触发领奖端点")
	}
	if !strings.Contains(res.Message, "3/3") {
		t.Errorf("回读进度应达标: %q", res.Message)
	}

	// 不存在的任务安全跳过。
	res, err = c.RunGrowthAction(context.Background(), cnCred(), "no_such_task", false)
	if err != nil || res == nil || !strings.Contains(res.Message, "无此任务") {
		t.Fatalf("未知任务应安全跳过: %+v err=%v", res, err)
	}
}
