-- 246: 测评计划去账号化——执行改为回环公共网关（/v1/chat/completions），
-- 由网关调度选号，计划不再绑定具体账号；runs 的账号快照列一并移除。
-- plans.account_id 原带 ON DELETE CASCADE 外键（删账号会级联删计划），随列删除消除。

ALTER TABLE model_benchmark_plans DROP COLUMN IF EXISTS account_id;
DROP INDEX IF EXISTS idx_model_benchmark_plans_account;
ALTER TABLE model_benchmark_runs DROP COLUMN IF EXISTS account_id;
