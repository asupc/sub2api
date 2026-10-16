-- 244: 模型能力测评计划支持“停用状态下手动执行一次”
-- 语义：manual_trigger_at 非空表示存在一次性手动排队请求；runner 领取后清空。
-- enabled 保持原义（是否按 cron 周期调度），停用计划不会被 cron 触发。

ALTER TABLE model_benchmark_plans
    ADD COLUMN IF NOT EXISTS manual_trigger_at TIMESTAMPTZ;

-- 领取扫描同时覆盖 cron due 与手动排队；索引需按新条件放宽。
DROP INDEX IF EXISTS idx_model_benchmark_plans_due;
CREATE INDEX IF NOT EXISTS idx_model_benchmark_plans_due
    ON model_benchmark_plans(next_run_at)
    WHERE running_status = 'idle';
CREATE INDEX IF NOT EXISTS idx_model_benchmark_plans_manual
    ON model_benchmark_plans(manual_trigger_at)
    WHERE manual_trigger_at IS NOT NULL;

-- 租约恢复（ReleaseExpiredClaims）不区分 enabled：停用计划被手动触发后崩溃同样
-- 需要恢复，故恢复索引也不能带 enabled 条件。
DROP INDEX IF EXISTS idx_model_benchmark_plans_claim_recovery;
CREATE INDEX IF NOT EXISTS idx_model_benchmark_plans_claim_recovery
    ON model_benchmark_plans(claim_expires_at)
    WHERE running_status = 'running';
