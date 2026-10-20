-- Model capability benchmark reasoning level (user-configurable).
ALTER TABLE model_benchmark_plans ADD COLUMN IF NOT EXISTS reasoning_effort VARCHAR(32) NOT NULL DEFAULT '';
ALTER TABLE model_benchmark_plans ADD COLUMN IF NOT EXISTS reasoning_level VARCHAR(32) NOT NULL DEFAULT '';
ALTER TABLE model_benchmark_runs ADD COLUMN IF NOT EXISTS reasoning_effort VARCHAR(32) NOT NULL DEFAULT '';
ALTER TABLE model_benchmark_runs ADD COLUMN IF NOT EXISTS reasoning_level VARCHAR(32) NOT NULL DEFAULT '';