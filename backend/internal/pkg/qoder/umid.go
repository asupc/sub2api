package qoder

// umid.go 二开新增：Qoder 官方原生风控身份桥（runtime-info 组件）的 Go 移植。
//
// 背景（对齐 qoder2api-hub issue #10/#18 的实测结论）：国际版活动平台
// （/sash/api/v1/me/campaigns）按**真实机器身份**过滤活动列表——派生的
// cosy-machine* 假头会被服务端判定为非官方客户端，把 CLAIMABLE 的
// 「每日领取 100 Credits」整条过滤；不发机器头时国际版活动可能整列不可见。
// 官方桌面端通过原生组件取真身份：
//
//	runtime-info prod --account-stdin   stdin: {"account":"<uid>"}
//	stdout: {"machineToken","machineType","machineCode","vmInfo",...}
//
// 组件来源（部署侧准备，代码只负责发现与调用）：
//   - Windows：官方桌面客户端 <install>/resources/umid/runtime-info.exe；
//   - Linux/Docker：qoder2api-hub 的 _install_umid.py 从 npm 包
//     @qoder-ai/qodercli 提取的 runtime-info（ELF），放置到 QD_UMID_DIR。
//
// 身份稳定性（对齐 Python machine_identity 缓存纪律，裁剪投票）：
//   - 组件输出由本机种子文件 $HOME/.config/.locale_cfg 决定，同种子下重复
//     调用返回同一身份，删种子才换新（Python #18 实测）；
//   - 落盘缓存**优先**于组件调用（Docker 重建后身份不变），组件失败时回退
//     落盘缓存（哪怕过期）；
//   - 落盘只在「无可用缓存时的新采样」与「自愈刷新」两处写入，其余路径
//     不写，避免身份被自己刷掉；
//   - Python 的首次 3 轮多数表决未移植（等价 QD_MACHINE_IDENTITY_VOTE=0）：
//     表决只防 VM 判定抖动的 1~2% 少数派，自愈路径已兜底，先保持简单。
//
// 已知限制（服务端策略，代码无法绕过）：机器身份是机器级的，国际版按设备
// 去重——每台设备每天只有 1 个国际版账号能领到活动奖励。

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// nativeIdentityTTL 进程内身份缓存时长（Python NATIVE_IDENTITY_TTL）。
const nativeIdentityTTL = 30 * time.Minute

// runtimeInfoTimeout 单次组件调用预算（Python run_runtime_info timeout=25）。
const runtimeInfoTimeout = 25 * time.Second

// identitySeedPath 组件的身份种子文件（Python _purge_identity_seed：删除后
// 组件下次调用必换新身份；缺失即无种子）。
func identitySeedPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".config", ".locale_cfg")
}

// MachineIdentity 原生机器身份（活动平台 desktop 头的六头真值来源）。
type MachineIdentity struct {
	MachineToken string
	MachineType  string
	MachineCode  string
}

// valid 三元组齐全才算可用身份。
func (m MachineIdentity) valid() bool {
	return m.MachineToken != "" && m.MachineType != "" && m.MachineCode != ""
}

// UmidDir 返回组件与身份缓存的根目录：$QD_UMID_DIR，否则工作目录下 ./umid。
func UmidDir() string {
	if dir := strings.TrimSpace(os.Getenv("QD_UMID_DIR")); dir != "" {
		return dir
	}
	return filepath.Join(".", "umid")
}

// nativeIdentityDisabled 运维开关 QD_NATIVE_IDENTITY=0（对齐 Python：测试/
// 受限环境不希望拉起客户端二进制时关闭整条原生身份链路）。
func nativeIdentityDisabled() bool {
	switch strings.TrimSpace(strings.ToLower(os.Getenv("QD_NATIVE_IDENTITY"))) {
	case "0", "false", "no":
		return true
	}
	return false
}

// runtimeInfoPath 发现 runtime-info 组件（Windows 先 .exe，POSIX 裸名）；
// 找不到返回空串（调用方静默降级为不发机器头）。
func runtimeInfoPath() string {
	if nativeIdentityDisabled() {
		return ""
	}
	dir := UmidDir()
	names := []string{"runtime-info"}
	if runtime.GOOS == "windows" {
		names = []string{"runtime-info.exe", "runtime-info"}
	}
	for _, name := range names {
		p := filepath.Join(dir, name)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

// umidWarnOnce 「组件在但执行失败」按错误类别进程内只提示一次（Python
// _runtime_info_warn：alpine 缺 glibc loader 这类失败被静默吞掉最易误诊）。
var (
	umidWarnMu   sync.Mutex
	umidWarnSeen = map[string]bool{}
)

func umidWarnOnce(category, message string) {
	umidWarnMu.Lock()
	defer umidWarnMu.Unlock()
	if umidWarnSeen[category] {
		return
	}
	umidWarnSeen[category] = true
	log.Printf("[qoder-umid] %s: %s", category, message)
}

// runRuntimeInfoFn 组件调用入口（包级变量：单测注入 stub，Windows 环境无法
// 伪造 ELF 组件）。
var runRuntimeInfoFn = runRuntimeInfo

// runRuntimeInfo 调一次组件并解析输出（Python run_runtime_info）。任何失败
// 返回零值；「组件不存在」静默，「组件在但跑不起来/输出异常」按类别提示一次。
func runRuntimeInfo(uid string) MachineIdentity {
	exe := runtimeInfoPath()
	if exe == "" {
		return MachineIdentity{}
	}
	ctx, cancel := context.WithTimeout(context.Background(), runtimeInfoTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "prod", "--account-stdin")
	cmd.Dir = filepath.Dir(exe)
	cmd.Stdin = strings.NewReader(fmt.Sprintf(`{"account":%q} `, uid))
	out, err := cmd.Output()
	if err != nil {
		umidWarnOnce("exec", fmt.Sprintf("无法执行 %s: %v（组件存在但跑不起来；alpine 需 gcompat/libstdc++ 兼容层）", exe, err))
		return MachineIdentity{}
	}
	line := strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
	if line == "" {
		umidWarnOnce("empty-output", fmt.Sprintf("%s 运行结束但没有输出（exit=%d），身份退化为不发机器头", exe, cmd.ProcessState.ExitCode()))
		return MachineIdentity{}
	}
	var data struct {
		MachineToken string `json:"machineToken"`
		MachineType  string `json:"machineType"`
		MachineCode  string `json:"machineCode"`
	}
	if err := json.Unmarshal([]byte(line), &data); err != nil {
		umidWarnOnce("bad-output", fmt.Sprintf("%s 输出不是 JSON: %v", exe, err))
		return MachineIdentity{}
	}
	return MachineIdentity{
		MachineToken: strings.TrimSpace(data.MachineToken),
		MachineType:  strings.TrimSpace(data.MachineType),
		MachineCode:  strings.TrimSpace(data.MachineCode),
	}
}

// ---------------------------------------------------------------------------
// 身份缓存：进程内 TTL + 落盘（cache 优先，组件失败回退落盘哪怕过期）。
// ---------------------------------------------------------------------------

type umidCacheEntry struct {
	At    time.Time
	ident MachineIdentity
}

type umidDiskDoc struct {
	Version int                      `json:"version"`
	Realms  map[string]umidDiskEntry `json:"realm"`
}

type umidDiskEntry struct {
	MachineToken string    `json:"machineToken"`
	MachineType  string    `json:"machineType"`
	MachineCode  string    `json:"machineCode"`
	CachedAt     time.Time `json:"cached_at"`
}

func umidCachePath() string {
	return filepath.Join(UmidDir(), "machine_identity.json")
}

var umidMu sync.Mutex
var umidMemCache = map[string]umidCacheEntry{}

// loadUmidDisk 读落盘缓存；损坏按无缓存处理（只提示一次）。
func loadUmidDisk(realm string) (MachineIdentity, bool) {
	raw, err := os.ReadFile(umidCachePath())
	if err != nil {
		return MachineIdentity{}, false
	}
	var doc umidDiskDoc
	if err := json.Unmarshal(raw, &doc); err != nil || doc.Realms == nil {
		umidWarnOnce("cache-corrupt", fmt.Sprintf("身份缓存损坏，按无缓存重建: %v", err))
		return MachineIdentity{}, false
	}
	entry, ok := doc.Realms[realm]
	if !ok {
		return MachineIdentity{}, false
	}
	ident := MachineIdentity{
		MachineToken: strings.TrimSpace(entry.MachineToken),
		MachineType:  strings.TrimSpace(entry.MachineType),
		MachineCode:  strings.TrimSpace(entry.MachineCode),
	}
	return ident, ident.valid()
}

// saveUmidDisk 读-改-写落盘缓存（仅「新采样采纳」与「自愈覆盖」两处调用）。
func saveUmidDisk(realm string, ident MachineIdentity) {
	raw, err := os.ReadFile(umidCachePath())
	doc := umidDiskDoc{Version: 1, Realms: map[string]umidDiskEntry{}}
	if err == nil {
		_ = json.Unmarshal(raw, &doc)
		if doc.Realms == nil {
			doc.Realms = map[string]umidDiskEntry{}
		}
	}
	doc.Version = 1
	doc.Realms[realm] = umidDiskEntry{
		MachineToken: ident.MachineToken,
		MachineType:  ident.MachineType,
		MachineCode:  ident.MachineCode,
		CachedAt:     time.Now(),
	}
	blob, merr := json.MarshalIndent(doc, "", "  ")
	if merr != nil {
		return
	}
	_ = os.MkdirAll(UmidDir(), 0o755)
	tmp := umidCachePath() + ".tmp"
	if werr := os.WriteFile(tmp, blob, 0o600); werr == nil {
		_ = os.Rename(tmp, umidCachePath())
	}
}

// NativeMachineIdentity 取该区域的真实机器身份（机器级，跨账号相同）。
// 顺序：进程内 TTL 缓存 → 落盘缓存（命中不调组件）→ 组件采样 → 组件失败
// 回退落盘（哪怕过期）。任何失败返回零值（调用方降级为不发机器头）。
func NativeMachineIdentity(realm, uid string) MachineIdentity {
	umidMu.Lock()
	if hit, ok := umidMemCache[realm]; ok && time.Since(hit.At) < nativeIdentityTTL {
		umidMu.Unlock()
		return hit.ident
	}
	umidMu.Unlock()

	if ident, ok := loadUmidDisk(realm); ok {
		umidMu.Lock()
		umidMemCache[realm] = umidCacheEntry{At: time.Now(), ident: ident}
		umidMu.Unlock()
		return ident
	}
	if ident := runRuntimeInfoFn(uid); ident.valid() {
		saveUmidDisk(realm, ident)
		umidMu.Lock()
		umidMemCache[realm] = umidCacheEntry{At: time.Now(), ident: ident}
		umidMu.Unlock()
		return ident
	}
	// 组件失败：回退落盘缓存（哪怕过期）——比不发机器头好。
	if ident, ok := loadUmidDisk(realm); ok {
		umidMu.Lock()
		umidMemCache[realm] = umidCacheEntry{At: time.Now(), ident: ident}
		umidMu.Unlock()
		return ident
	}
	// 彻底拿不到：空身份也进内存缓存（Python 同款），组件恢复前不再
	// 每次请求都重复尝试 spawn。
	umidMu.Lock()
	umidMemCache[realm] = umidCacheEntry{At: time.Now()}
	umidMu.Unlock()
	return MachineIdentity{}
}

// RefreshNativeIdentity 自愈路径（对齐 Python campaigns() 的 force 刷新）：
// 服务端明确不认当前身份（showCampaign=false）时调用——**必须换种子**再采样
// （同种子下组件只会返回同一个被拒身份），并覆盖落盘缓存。失败返回零值
// （调用方沿用旧结果）。
func RefreshNativeIdentity(realm, uid string) MachineIdentity {
	if seed := identitySeedPath(); seed != "" {
		_ = os.Remove(seed)
	}
	ident := runRuntimeInfoFn(uid)
	if !ident.valid() {
		return MachineIdentity{}
	}
	saveUmidDisk(realm, ident)
	umidMu.Lock()
	umidMemCache[realm] = umidCacheEntry{At: time.Now(), ident: ident}
	umidMu.Unlock()
	return ident
}
