-- 248: 放宽模型能力测评计划的预算约束，与服务层/前端上限对齐。
-- 旧约束 max_output_tokens <= 4096 / timeout_seconds <= 600 低于服务层允许的
-- 32768 / 900，导致合法配置在 ApplyPlanBudgetDefaults / 计划更新时触发 CHECK 违反
-- 而返回 500。此处仅放宽，不改变默认值与既有数据。
ALTER TABLE model_benchmark_plans DROP CONSTRAINT IF EXISTS model_benchmark_plans_max_output_tokens_check;
ALTER TABLE model_benchmark_plans
    ADD CONSTRAINT model_benchmark_plans_max_output_tokens_check
    CHECK (max_output_tokens >= 128 AND max_output_tokens <= 32768);

ALTER TABLE model_benchmark_plans DROP CONSTRAINT IF EXISTS model_benchmark_plans_timeout_seconds_check;
ALTER TABLE model_benchmark_plans
    ADD CONSTRAINT model_benchmark_plans_timeout_seconds_check
    CHECK (timeout_seconds >= 10 AND timeout_seconds <= 900);
