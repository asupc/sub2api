-- 250: 移除模型能力测评功能（2026-10-23）
-- 243~249 的建表迁移文件已从仓库删除；此迁移在既有库上清理残留表与迁移记录，
-- 在全新库上全部 IF EXISTS / DELETE 为空操作，幂等安全。

DROP TABLE IF EXISTS model_benchmark_run_items;
DROP TABLE IF EXISTS model_benchmark_runs;
DROP TABLE IF EXISTS model_benchmark_plans;

DELETE FROM schema_migrations WHERE filename IN (
    '243_so_model_capability_benchmark.sql',
    '244_so_model_benchmark_manual_trigger.sql',
    '245_so_model_benchmark_comparison.sql',
    '246_so_model_benchmark_gateway_mode.sql',
    '247_so_model_benchmark_reasoning_level.sql',
    '248_so_model_benchmark_budget_limits.sql',
    '249_so_model_benchmark_difficulty_range.sql'
);
