-- Public comparison forms and measurement evidence. Historical runs retain legacy identity.
ALTER TABLE model_benchmark_plans ADD COLUMN IF NOT EXISTS comparison_key VARCHAR(64) NOT NULL DEFAULT 'baseline-v2';
ALTER TABLE model_benchmark_runs ADD COLUMN IF NOT EXISTS comparison_key VARCHAR(64) NOT NULL DEFAULT 'legacy';
ALTER TABLE model_benchmark_runs ADD COLUMN IF NOT EXISTS max_output_tokens INTEGER NOT NULL DEFAULT 0;
ALTER TABLE model_benchmark_runs ADD COLUMN IF NOT EXISTS timeout_seconds INTEGER NOT NULL DEFAULT 0;
ALTER TABLE model_benchmark_runs ADD COLUMN IF NOT EXISTS sample_count INTEGER NOT NULL DEFAULT 1;
ALTER TABLE model_benchmark_runs ADD COLUMN IF NOT EXISTS quality JSONB NOT NULL DEFAULT '{}'::jsonb;
CREATE INDEX IF NOT EXISTS idx_model_benchmark_runs_comparison
    ON model_benchmark_runs(suite_id,suite_version,scoring_version,comparison_key,model_id,created_at DESC);
-- Normalized per-domain weights need more than the original four decimals.
ALTER TABLE model_benchmark_run_items ALTER COLUMN weight TYPE NUMERIC(16,10);
ALTER TABLE model_benchmark_run_items ALTER COLUMN earned_points TYPE NUMERIC(16,10);
ALTER TABLE model_benchmark_run_items ALTER COLUMN possible_points TYPE NUMERIC(16,10);
