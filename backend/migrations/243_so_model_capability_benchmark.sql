-- 243: Model capability benchmark plans, runs, and item-level evidence
-- The feature is disabled by default; creating a plan does not trigger upstream traffic.

CREATE TABLE IF NOT EXISTS model_benchmark_plans (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(120) NOT NULL,
    suite_id VARCHAR(60) NOT NULL,
    suite_version VARCHAR(30) NOT NULL,
    scoring_version VARCHAR(30) NOT NULL,
    model_id VARCHAR(100) NOT NULL,
    account_id BIGINT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    cron_expression VARCHAR(100) NOT NULL DEFAULT '0 4 * * 1',
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    sample_count INT NOT NULL DEFAULT 1 CHECK (sample_count BETWEEN 1 AND 3),
    max_output_tokens INT NOT NULL DEFAULT 768 CHECK (max_output_tokens BETWEEN 128 AND 4096),
    timeout_seconds INT NOT NULL DEFAULT 90 CHECK (timeout_seconds BETWEEN 10 AND 600),
    max_results INT NOT NULL DEFAULT 50 CHECK (max_results BETWEEN 1 AND 500),
    running_status VARCHAR(20) NOT NULL DEFAULT 'idle' CHECK (running_status IN ('idle', 'running')),
    claim_expires_at TIMESTAMPTZ,
    last_run_at TIMESTAMPTZ,
    next_run_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_model_benchmark_plans_account ON model_benchmark_plans(account_id);
CREATE INDEX IF NOT EXISTS idx_model_benchmark_plans_due
    ON model_benchmark_plans(next_run_at)
    WHERE enabled = TRUE AND running_status = 'idle';
CREATE INDEX IF NOT EXISTS idx_model_benchmark_plans_claim_recovery
    ON model_benchmark_plans(claim_expires_at)
    WHERE enabled = TRUE AND running_status = 'running';

CREATE TABLE IF NOT EXISTS model_benchmark_runs (
    id BIGSERIAL PRIMARY KEY,
    plan_id BIGINT NOT NULL REFERENCES model_benchmark_plans(id) ON DELETE CASCADE,
    suite_id VARCHAR(60) NOT NULL,
    suite_version VARCHAR(30) NOT NULL,
    scoring_version VARCHAR(30) NOT NULL,
    model_id VARCHAR(100) NOT NULL,
    upstream_model_id VARCHAR(100) NOT NULL DEFAULT '',
    account_id BIGINT NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'running' CHECK (status IN ('running', 'success', 'partial', 'failed', 'canceled')),
    intelligence_score NUMERIC(6,2),
    efficiency_score NUMERIC(6,2),
    composite_score NUMERIC(6,2),
    category_scores JSONB NOT NULL DEFAULT '{}'::jsonb,
    item_count INT NOT NULL DEFAULT 0,
    answered_count INT NOT NULL DEFAULT 0,
    correct_count INT NOT NULL DEFAULT 0,
    input_tokens BIGINT,
    output_tokens BIGINT,
    total_tokens BIGINT,
    estimated_cost NUMERIC(18,6),
    avg_latency_ms BIGINT,
    p95_latency_ms BIGINT,
    usage_source VARCHAR(20) NOT NULL DEFAULT 'missing' CHECK (usage_source IN ('upstream', 'missing')),
    usage_field_coverage JSONB NOT NULL DEFAULT '{}'::jsonb,
    error_message TEXT NOT NULL DEFAULT '',
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_model_benchmark_runs_plan_created ON model_benchmark_runs(plan_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_model_benchmark_runs_version_model_created
    ON model_benchmark_runs(suite_id, suite_version, scoring_version, model_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_model_benchmark_runs_status_created ON model_benchmark_runs(status, created_at DESC);

CREATE TABLE IF NOT EXISTS model_benchmark_run_items (
    id BIGSERIAL PRIMARY KEY,
    run_id BIGINT NOT NULL REFERENCES model_benchmark_runs(id) ON DELETE CASCADE,
    item_id VARCHAR(80) NOT NULL,
    family_id VARCHAR(60) NOT NULL,
    category VARCHAR(40) NOT NULL,
    difficulty INT NOT NULL CHECK (difficulty BETWEEN 3 AND 5),
    weight NUMERIC(8,4) NOT NULL,
    prompt_hash VARCHAR(64) NOT NULL,
    prompt_snapshot TEXT NOT NULL DEFAULT '',
    response_text TEXT NOT NULL DEFAULT '',
    status VARCHAR(20) NOT NULL CHECK (status IN ('correct', 'partial', 'incorrect', 'format_error', 'execution_failed')),
    score NUMERIC(6,4) NOT NULL DEFAULT 0 CHECK (score >= 0 AND score <= 1),
    earned_points NUMERIC(8,4) NOT NULL DEFAULT 0,
    possible_points NUMERIC(8,4) NOT NULL DEFAULT 1,
    checker_type VARCHAR(30) NOT NULL,
    failure_reason TEXT NOT NULL DEFAULT '',
    input_tokens BIGINT,
    output_tokens BIGINT,
    reasoning_tokens BIGINT,
    cache_read_tokens BIGINT,
    cache_write_tokens BIGINT,
    total_tokens BIGINT,
    estimated_cost NUMERIC(18,6),
    usage_source VARCHAR(20) NOT NULL DEFAULT 'missing' CHECK (usage_source IN ('upstream', 'missing')),
    usage_field_coverage JSONB NOT NULL DEFAULT '{}'::jsonb,
    pricing_source VARCHAR(20) NOT NULL DEFAULT 'missing' CHECK (pricing_source IN ('pricing', 'missing')),
    latency_ms BIGINT,
    first_token_ms BIGINT,
    error_message TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_model_benchmark_items_run_item ON model_benchmark_run_items(run_id, item_id);
CREATE INDEX IF NOT EXISTS idx_model_benchmark_items_run_category ON model_benchmark_run_items(run_id, category);
