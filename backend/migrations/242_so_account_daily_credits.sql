-- 二开新增：qoder / workbuddy 账号每日积分消耗统计。
--
-- 动机：这两个平台的计费单位是积分（qoder usage.credits / workbuddy usage.credit），
-- 账号 extra 里的 wb_quota / qd_quota 只有余额快照，看不到今日消耗；账号列表的
-- 「今日统计」列是美元口径（usage_logs 聚合），对订阅积分平台无意义。
--
-- 写入：OpenAIGatewayService.RecordUsage 在 usage_log 落库后按请求 UPSERT 累加
--       （仅 qoder / workbuddy 平台且 credits > 0；best-effort，失败仅记日志）。
-- 读取：AccountUsageService.GetTodayStats / GetTodayStatsBatch 附到 WindowStats.Credits，
--       经既有 /admin/accounts/today-stats/batch 下发给账号列表。
-- 日期口径：与 timezone.Today()（全局配置时区，缺省 Asia/Shanghai）同源，跨日自动分桶。
--
-- 命名沿用二开迁移约定 NNN_so_*.sql（与上游顺延编号隔离，避免下次同步撞号）；
-- CREATE TABLE IF NOT EXISTS 保证可重入。

CREATE TABLE IF NOT EXISTS account_daily_credits (
    account_id BIGINT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    day        DATE NOT NULL,
    credits    DECIMAL(20, 10) NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (account_id, day)
);

-- 按日维度清理/统计用（主键已覆盖 per-account 查询，此索引服务跨账号按日扫描）。
CREATE INDEX IF NOT EXISTS idx_account_daily_credits_day ON account_daily_credits (day);

COMMENT ON TABLE account_daily_credits IS '二开：qoder/workbuddy 账号每日积分消耗累计（按北京时间切日）';
COMMENT ON COLUMN account_daily_credits.day IS '统计日（timezone.Today() 口径，缺省 Asia/Shanghai）';
COMMENT ON COLUMN account_daily_credits.credits IS '当日累计消耗积分（上游 usage.credits/credit 之和）';
