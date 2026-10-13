-- ============================================================================
-- restore-upstream-schema.sql
-- 用途：把「执行过 sub2api-so 裁剪迁移（238_so/239_so/240_so/241_so）」的数据库
--       恢复为官方 sub2api v0.2.8 的完整结构。被 DROP 表中的历史数据不恢复（全新空表），
--       核心运行数据（users/groups/accounts/api_keys/usage_logs 等）不受影响。
-- 前提：数据库已应用官方 001~238 + so 的 238_so~241_so + 242~244 迁移序列。
-- 执行：psql -h 127.0.0.1 -U sub2api -d <库名> -v ON_ERROR_STOP=1 -f restore-upstream-schema.sql
--       （psql 默认逐条自动提交，内容含 CREATE INDEX CONCURRENTLY，不要包在 BEGIN/COMMIT 里）
-- 幂等：全部语句幂等，可重复执行。
-- 回滚：本脚本只新增表/列/约束/索引/函数，回滚官方二进制即可继续用 so 二进制
--       （so 的 drop 迁移已记录不会重跑；恢复出的对象对 so 代码是"多余但忽略"）。
-- 注意：执行完本脚本之前，禁止把服务切换到官方后端二进制（官方 239 迁移在缺
--       channel_model_pricing 表的库上会启动失败）。
-- 内容来源：官方 backend/migrations 对应文件原文（幂等重放）+ 三段手写补丁。
-- ============================================================================

-- ----------------------------------------------------------------------------
-- 1) proxies 表（官方 001_init.sql 原文节选；accounts.proxy_id 补丁见末尾）
-- ----------------------------------------------------------------------------

-- Sub2API 初始化数据库迁移脚本
-- PostgreSQL 15+

-- 1. proxies 代理IP表（无外键依赖）
CREATE TABLE IF NOT EXISTS proxies (
    id              BIGSERIAL PRIMARY KEY,
    name            VARCHAR(100) NOT NULL,
    protocol        VARCHAR(20) NOT NULL,                 -- http/https/socks5
    host            VARCHAR(255) NOT NULL,
    port            INT NOT NULL,
    username        VARCHAR(100),
    password        VARCHAR(100),
    status          VARCHAR(20) NOT NULL DEFAULT 'active', -- active/disabled
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at      TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_proxies_status ON proxies(status);
CREATE INDEX IF NOT EXISTS idx_proxies_deleted_at ON proxies(deleted_at);

-- ----------------------------------------------------------------------------
-- 2) 官方 032_add_api_key_ip_restriction.sql 原文重放
-- ----------------------------------------------------------------------------

-- Add IP restriction fields to api_keys table
-- ip_whitelist: JSON array of allowed IPs/CIDRs (if set, only these IPs can use the key)
-- ip_blacklist: JSON array of blocked IPs/CIDRs (these IPs are always blocked)

ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS ip_whitelist JSONB DEFAULT NULL;
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS ip_blacklist JSONB DEFAULT NULL;

COMMENT ON COLUMN api_keys.ip_whitelist IS 'JSON array of allowed IPs/CIDRs, e.g. ["192.168.1.100", "10.0.0.0/8"]';
COMMENT ON COLUMN api_keys.ip_blacklist IS 'JSON array of blocked IPs/CIDRs, e.g. ["1.2.3.4", "5.6.0.0/16"]';

-- ----------------------------------------------------------------------------
-- 3) 官方 033_add_promo_codes.sql 原文重放
-- ----------------------------------------------------------------------------

-- 创建注册优惠码表
CREATE TABLE IF NOT EXISTS promo_codes (
    id BIGSERIAL PRIMARY KEY,
    code VARCHAR(32) NOT NULL UNIQUE,
    bonus_amount DECIMAL(20,8) NOT NULL DEFAULT 0,
    max_uses INT NOT NULL DEFAULT 0,
    used_count INT NOT NULL DEFAULT 0,
    status VARCHAR(20) NOT NULL DEFAULT 'active',
    expires_at TIMESTAMPTZ DEFAULT NULL,
    notes TEXT DEFAULT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 创建优惠码使用记录表
CREATE TABLE IF NOT EXISTS promo_code_usages (
    id BIGSERIAL PRIMARY KEY,
    promo_code_id BIGINT NOT NULL REFERENCES promo_codes(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    bonus_amount DECIMAL(20,8) NOT NULL,
    used_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(promo_code_id, user_id)
);

-- 索引
CREATE INDEX IF NOT EXISTS idx_promo_codes_status ON promo_codes(status);
CREATE INDEX IF NOT EXISTS idx_promo_codes_expires_at ON promo_codes(expires_at);
CREATE INDEX IF NOT EXISTS idx_promo_code_usages_promo_code_id ON promo_code_usages(promo_code_id);
CREATE INDEX IF NOT EXISTS idx_promo_code_usages_user_id ON promo_code_usages(user_id);

COMMENT ON TABLE promo_codes IS '注册优惠码';
COMMENT ON TABLE promo_code_usages IS '优惠码使用记录';
COMMENT ON COLUMN promo_codes.max_uses IS '最大使用次数，0表示无限制';
COMMENT ON COLUMN promo_codes.status IS '状态: active, disabled';

-- ----------------------------------------------------------------------------
-- 4) 官方 044_add_user_totp.sql 原文重放
-- ----------------------------------------------------------------------------

-- 为 users 表添加 TOTP 双因素认证字段
ALTER TABLE users
  ADD COLUMN IF NOT EXISTS totp_secret_encrypted TEXT DEFAULT NULL,
  ADD COLUMN IF NOT EXISTS totp_enabled BOOLEAN NOT NULL DEFAULT FALSE,
  ADD COLUMN IF NOT EXISTS totp_enabled_at TIMESTAMPTZ DEFAULT NULL;

COMMENT ON COLUMN users.totp_secret_encrypted IS 'AES-256-GCM 加密的 TOTP 密钥';
COMMENT ON COLUMN users.totp_enabled IS '是否启用 TOTP 双因素认证';
COMMENT ON COLUMN users.totp_enabled_at IS 'TOTP 启用时间';

-- 创建索引以支持快速查询启用 2FA 的用户
CREATE INDEX IF NOT EXISTS idx_users_totp_enabled ON users(totp_enabled) WHERE deleted_at IS NULL AND totp_enabled = true;

-- ----------------------------------------------------------------------------
-- 5) 官方 045_add_announcements.sql 原文重放
-- ----------------------------------------------------------------------------

-- 创建公告表
CREATE TABLE IF NOT EXISTS announcements (
    id BIGSERIAL PRIMARY KEY,
    title VARCHAR(200) NOT NULL,
    content TEXT NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'draft',
    targeting JSONB NOT NULL DEFAULT '{}'::jsonb,
    starts_at TIMESTAMPTZ DEFAULT NULL,
    ends_at TIMESTAMPTZ DEFAULT NULL,
    created_by BIGINT DEFAULT NULL REFERENCES users(id) ON DELETE SET NULL,
    updated_by BIGINT DEFAULT NULL REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 公告已读表
CREATE TABLE IF NOT EXISTS announcement_reads (
    id BIGSERIAL PRIMARY KEY,
    announcement_id BIGINT NOT NULL REFERENCES announcements(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    read_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(announcement_id, user_id)
);

-- 索引
CREATE INDEX IF NOT EXISTS idx_announcements_status ON announcements(status);
CREATE INDEX IF NOT EXISTS idx_announcements_starts_at ON announcements(starts_at);
CREATE INDEX IF NOT EXISTS idx_announcements_ends_at ON announcements(ends_at);
CREATE INDEX IF NOT EXISTS idx_announcements_created_at ON announcements(created_at);

CREATE INDEX IF NOT EXISTS idx_announcement_reads_announcement_id ON announcement_reads(announcement_id);
CREATE INDEX IF NOT EXISTS idx_announcement_reads_user_id ON announcement_reads(user_id);
CREATE INDEX IF NOT EXISTS idx_announcement_reads_read_at ON announcement_reads(read_at);

COMMENT ON TABLE announcements IS '系统公告';
COMMENT ON COLUMN announcements.status IS '状态: draft, active, archived';
COMMENT ON COLUMN announcements.targeting IS '展示条件（JSON 规则）';
COMMENT ON COLUMN announcements.starts_at IS '开始展示时间（为空表示立即生效）';
COMMENT ON COLUMN announcements.ends_at IS '结束展示时间（为空表示永久生效）';

COMMENT ON TABLE announcement_reads IS '公告已读记录';
COMMENT ON COLUMN announcement_reads.read_at IS '用户首次已读时间';

-- ----------------------------------------------------------------------------
-- 6) 官方 068_add_announcement_notify_mode.sql 原文重放
-- ----------------------------------------------------------------------------

ALTER TABLE announcements ADD COLUMN IF NOT EXISTS notify_mode VARCHAR(20) NOT NULL DEFAULT 'silent';

-- ----------------------------------------------------------------------------
-- 7) 官方 081_create_channels.sql 原文重放
-- ----------------------------------------------------------------------------

-- Create channels table for managing pricing channels.
-- A channel groups multiple groups together and provides custom model pricing.

SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '10min';

-- 渠道表
CREATE TABLE IF NOT EXISTS channels (
    id          BIGSERIAL    PRIMARY KEY,
    name        VARCHAR(100) NOT NULL,
    description TEXT         DEFAULT '',
    status      VARCHAR(20)  NOT NULL DEFAULT 'active',
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

-- 渠道名称唯一索引
CREATE UNIQUE INDEX IF NOT EXISTS idx_channels_name ON channels (name);
CREATE INDEX IF NOT EXISTS idx_channels_status ON channels (status);

-- 渠道-分组关联表（每个分组只能属于一个渠道）
CREATE TABLE IF NOT EXISTS channel_groups (
    id          BIGSERIAL    PRIMARY KEY,
    channel_id  BIGINT       NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    group_id    BIGINT       NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_channel_groups_group_id ON channel_groups (group_id);
CREATE INDEX IF NOT EXISTS idx_channel_groups_channel_id ON channel_groups (channel_id);

-- 渠道模型定价表（一条定价可绑定多个模型）
CREATE TABLE IF NOT EXISTS channel_model_pricing (
    id                 BIGSERIAL      PRIMARY KEY,
    channel_id         BIGINT         NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    models             JSONB          NOT NULL DEFAULT '[]',
    input_price        NUMERIC(20,12),
    output_price       NUMERIC(20,12),
    cache_write_price  NUMERIC(20,12),
    cache_read_price   NUMERIC(20,12),
    image_output_price NUMERIC(20,8),
    created_at         TIMESTAMPTZ    NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ    NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_channel_model_pricing_channel_id ON channel_model_pricing (channel_id);

COMMENT ON TABLE channels IS '渠道管理：关联多个分组，提供自定义模型定价';
COMMENT ON TABLE channel_groups IS '渠道-分组关联表：每个分组最多属于一个渠道';
COMMENT ON TABLE channel_model_pricing IS '渠道模型定价：一条定价可绑定多个模型，价格一致';
COMMENT ON COLUMN channel_model_pricing.models IS '绑定的模型列表，JSON 数组，如 ["claude-opus-4-6","claude-opus-4-6-thinking"]';
COMMENT ON COLUMN channel_model_pricing.input_price IS '每 token 输入价格（USD），NULL 表示使用默认';
COMMENT ON COLUMN channel_model_pricing.output_price IS '每 token 输出价格（USD），NULL 表示使用默认';
COMMENT ON COLUMN channel_model_pricing.cache_write_price IS '缓存写入每 token 价格，NULL 表示使用默认';
COMMENT ON COLUMN channel_model_pricing.cache_read_price IS '缓存读取每 token 价格，NULL 表示使用默认';
COMMENT ON COLUMN channel_model_pricing.image_output_price IS '图片输出价格（Gemini Image 等），NULL 表示使用默认';

-- ----------------------------------------------------------------------------
-- 8) 官方 082_refactor_channel_pricing.sql 原文重放
-- ----------------------------------------------------------------------------

-- Extend channel_model_pricing with billing_mode and add context-interval child table.
-- Supports three billing modes: token (per-token with context intervals),
-- per_request (per-request with context-size tiers), and image (per-image).

SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '10min';

-- 1. 为 channel_model_pricing 添加 billing_mode 列
ALTER TABLE channel_model_pricing
    ADD COLUMN IF NOT EXISTS billing_mode VARCHAR(20) NOT NULL DEFAULT 'token';

COMMENT ON COLUMN channel_model_pricing.billing_mode IS '计费模式：token（按 token 区间计费）、per_request（按次计费）、image（图片计费）';

-- 2. 创建区间定价子表
CREATE TABLE IF NOT EXISTS channel_pricing_intervals (
    id                BIGSERIAL      PRIMARY KEY,
    pricing_id        BIGINT         NOT NULL REFERENCES channel_model_pricing(id) ON DELETE CASCADE,
    min_tokens        INT            NOT NULL DEFAULT 0,
    max_tokens        INT,
    tier_label        VARCHAR(50),
    input_price       NUMERIC(20,12),
    output_price      NUMERIC(20,12),
    cache_write_price NUMERIC(20,12),
    cache_read_price  NUMERIC(20,12),
    per_request_price NUMERIC(20,12),
    sort_order        INT            NOT NULL DEFAULT 0,
    created_at        TIMESTAMPTZ    NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ    NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_channel_pricing_intervals_pricing_id
    ON channel_pricing_intervals (pricing_id);

COMMENT ON TABLE channel_pricing_intervals IS '渠道定价区间：支持按 token 区间、按次分层、图片分辨率分层';
COMMENT ON COLUMN channel_pricing_intervals.min_tokens IS '区间下界（含），token 模式使用';
COMMENT ON COLUMN channel_pricing_intervals.max_tokens IS '区间上界（不含），NULL 表示无上限';
COMMENT ON COLUMN channel_pricing_intervals.tier_label IS '层级标签，按次/图片模式使用（如 1K、2K、4K、HD）';
COMMENT ON COLUMN channel_pricing_intervals.input_price IS 'token 模式：每 token 输入价';
COMMENT ON COLUMN channel_pricing_intervals.output_price IS 'token 模式：每 token 输出价';
COMMENT ON COLUMN channel_pricing_intervals.cache_write_price IS 'token 模式：缓存写入价';
COMMENT ON COLUMN channel_pricing_intervals.cache_read_price IS 'token 模式：缓存读取价';
COMMENT ON COLUMN channel_pricing_intervals.per_request_price IS '按次/图片模式：每次请求价格';

-- 3. 迁移现有 flat 定价为单区间 [0, +inf)
-- 仅迁移有明确定价（至少一个价格字段非 NULL）的条目
INSERT INTO channel_pricing_intervals (pricing_id, min_tokens, max_tokens, input_price, output_price, cache_write_price, cache_read_price, sort_order)
SELECT
    cmp.id,
    0,
    NULL,
    cmp.input_price,
    cmp.output_price,
    cmp.cache_write_price,
    cmp.cache_read_price,
    0
FROM channel_model_pricing cmp
WHERE cmp.billing_mode = 'token'
  AND (cmp.input_price IS NOT NULL OR cmp.output_price IS NOT NULL
       OR cmp.cache_write_price IS NOT NULL OR cmp.cache_read_price IS NOT NULL)
  AND NOT EXISTS (
      SELECT 1 FROM channel_pricing_intervals cpi WHERE cpi.pricing_id = cmp.id
  );

-- 4. 迁移 image_output_price 为 image 模式的区间条目
-- 将有 image_output_price 的现有条目复制为 billing_mode='image' 的独立条目
-- 注意：这里不改变原条目的 billing_mode，而是将 image_output_price 作为向后兼容字段保留
-- 实际的 image 计费在未来由独立的 billing_mode='image' 条目处理

-- ----------------------------------------------------------------------------
-- 9) 官方 083_channel_model_mapping.sql 原文重放
-- ----------------------------------------------------------------------------

SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '10min';

ALTER TABLE channels ADD COLUMN IF NOT EXISTS model_mapping JSONB DEFAULT '{}';
COMMENT ON COLUMN channels.model_mapping IS '渠道级模型映射，在账号映射之前执行。格式：{"source_model": "target_model"}';

-- ----------------------------------------------------------------------------
-- 10) 官方 084_channel_billing_model_source.sql 原文重放
-- ----------------------------------------------------------------------------

-- Add billing_model_source to channels (controls whether billing uses requested or upstream model)
ALTER TABLE channels ADD COLUMN IF NOT EXISTS billing_model_source VARCHAR(20) DEFAULT 'requested';

-- Add channel tracking fields to usage_logs
ALTER TABLE usage_logs ADD COLUMN IF NOT EXISTS channel_id BIGINT;
ALTER TABLE usage_logs ADD COLUMN IF NOT EXISTS model_mapping_chain VARCHAR(500);
ALTER TABLE usage_logs ADD COLUMN IF NOT EXISTS billing_tier VARCHAR(50);

-- ----------------------------------------------------------------------------
-- 11) 官方 085_channel_restrict_and_per_request_price.sql 原文重放
-- ----------------------------------------------------------------------------

-- Add model restriction switch to channels
ALTER TABLE channels ADD COLUMN IF NOT EXISTS restrict_models BOOLEAN DEFAULT false;

-- Add default per_request_price to channel_model_pricing (fallback when no tier matches)
ALTER TABLE channel_model_pricing ADD COLUMN IF NOT EXISTS per_request_price NUMERIC(20,10);

-- ----------------------------------------------------------------------------
-- 12) 官方 086_channel_platform_pricing.sql 原文重放
-- ----------------------------------------------------------------------------

-- 086_channel_platform_pricing.sql
-- 渠道按平台维度：model_pricing 加 platform 列，model_mapping 改为嵌套格式

-- 1. channel_model_pricing 加 platform 列
ALTER TABLE channel_model_pricing
    ADD COLUMN IF NOT EXISTS platform VARCHAR(50) NOT NULL DEFAULT 'anthropic';

CREATE INDEX IF NOT EXISTS idx_channel_model_pricing_platform
    ON channel_model_pricing (platform);

-- 2. model_mapping: 从扁平 {"src":"dst"} 迁移为嵌套 {"anthropic":{"src":"dst"}}
-- 仅迁移非空、非 '{}' 的旧格式数据（通过检查第一个 value 是否为字符串来判断是否为旧格式）
UPDATE channels
SET model_mapping = jsonb_build_object('anthropic', model_mapping)
WHERE model_mapping IS NOT NULL
  AND model_mapping::text NOT IN ('{}', 'null', '')
  AND NOT EXISTS (
      SELECT 1 FROM jsonb_each(model_mapping) AS kv
      WHERE jsonb_typeof(kv.value) = 'object'
      LIMIT 1
  );

-- ----------------------------------------------------------------------------
-- 13) 官方 088_channel_billing_model_source_channel_mapped.sql 原文重放
-- ----------------------------------------------------------------------------

-- Change default billing_model_source for new channels to 'channel_mapped'
-- Existing channels keep their current setting (no UPDATE on existing rows)
ALTER TABLE channels ALTER COLUMN billing_model_source SET DEFAULT 'channel_mapped';

-- ----------------------------------------------------------------------------
-- 14) 官方 092_payment_orders.sql 原文重放
-- ----------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS payment_orders (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL,
    user_email VARCHAR(255) NOT NULL DEFAULT '',
    user_name VARCHAR(100) NOT NULL DEFAULT '',
    user_notes TEXT,
    amount DECIMAL(20,2) NOT NULL,
    pay_amount DECIMAL(20,2) NOT NULL,
    fee_rate DECIMAL(10,4) NOT NULL DEFAULT 0,
    recharge_code VARCHAR(64) NOT NULL DEFAULT '',
    payment_type VARCHAR(30) NOT NULL DEFAULT '',
    payment_trade_no VARCHAR(128) NOT NULL DEFAULT '',
    pay_url TEXT,
    qr_code TEXT,
    qr_code_img TEXT,
    order_type VARCHAR(20) NOT NULL DEFAULT 'balance',
    plan_id BIGINT,
    subscription_group_id BIGINT,
    subscription_days INT,
    provider_instance_id VARCHAR(64),
    status VARCHAR(30) NOT NULL DEFAULT 'PENDING',
    refund_amount DECIMAL(20,2) NOT NULL DEFAULT 0,
    refund_reason TEXT,
    refund_at TIMESTAMPTZ,
    force_refund BOOLEAN NOT NULL DEFAULT FALSE,
    refund_requested_at TIMESTAMPTZ,
    refund_request_reason TEXT,
    refund_requested_by VARCHAR(20),
    expires_at TIMESTAMPTZ NOT NULL,
    paid_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    failed_at TIMESTAMPTZ,
    failed_reason TEXT,
    client_ip VARCHAR(50) NOT NULL DEFAULT '',
    src_host VARCHAR(255) NOT NULL DEFAULT '',
    src_url TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
-- Indexes
CREATE INDEX IF NOT EXISTS idx_payment_orders_user_id ON payment_orders(user_id);
CREATE INDEX IF NOT EXISTS idx_payment_orders_status ON payment_orders(status);
CREATE INDEX IF NOT EXISTS idx_payment_orders_expires_at ON payment_orders(expires_at);
CREATE INDEX IF NOT EXISTS idx_payment_orders_created_at ON payment_orders(created_at);
CREATE INDEX IF NOT EXISTS idx_payment_orders_paid_at ON payment_orders(paid_at);
CREATE INDEX IF NOT EXISTS idx_payment_orders_type_paid ON payment_orders(payment_type, paid_at);
CREATE INDEX IF NOT EXISTS idx_payment_orders_order_type ON payment_orders(order_type);

-- ----------------------------------------------------------------------------
-- 15) 官方 093_payment_audit_logs.sql 原文重放
-- ----------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS payment_audit_logs (
    id BIGSERIAL PRIMARY KEY,
    order_id VARCHAR(64) NOT NULL,
    action VARCHAR(50) NOT NULL,
    detail TEXT NOT NULL DEFAULT '',
    operator VARCHAR(100) NOT NULL DEFAULT 'system',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_payment_audit_logs_order_id ON payment_audit_logs(order_id);

-- ----------------------------------------------------------------------------
-- 16) 官方 095_subscription_plans.sql 原文重放
-- ----------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS subscription_plans (
    id BIGSERIAL PRIMARY KEY,
    group_id BIGINT NOT NULL,
    name VARCHAR(100) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    price DECIMAL(20,2) NOT NULL,
    original_price DECIMAL(20,2),
    validity_days INT NOT NULL DEFAULT 30,
    validity_unit VARCHAR(10) NOT NULL DEFAULT 'day',
    features TEXT NOT NULL DEFAULT '',
    product_name VARCHAR(100) NOT NULL DEFAULT '',
    for_sale BOOLEAN NOT NULL DEFAULT TRUE,
    sort_order INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_subscription_plans_group_id ON subscription_plans(group_id);
CREATE INDEX IF NOT EXISTS idx_subscription_plans_for_sale ON subscription_plans(for_sale);

-- ----------------------------------------------------------------------------
-- 17) 官方 095_channel_features.sql 原文重放
-- ----------------------------------------------------------------------------

ALTER TABLE channels ADD COLUMN IF NOT EXISTS features TEXT NOT NULL DEFAULT '';
COMMENT ON COLUMN channels.features IS '渠道特性描述，JSON 数组格式，用于支付页面展示';

-- ----------------------------------------------------------------------------
-- 18) 官方 096_payment_provider_instances.sql 原文重放
-- ----------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS payment_provider_instances (
    id BIGSERIAL PRIMARY KEY,
    provider_key VARCHAR(30) NOT NULL,
    name VARCHAR(100) NOT NULL DEFAULT '',
    config TEXT NOT NULL,
    supported_types VARCHAR(200) NOT NULL DEFAULT '',
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    sort_order INT NOT NULL DEFAULT 0,
    limits TEXT NOT NULL DEFAULT '',
    refund_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_payment_provider_instances_provider_key ON payment_provider_instances(provider_key);
CREATE INDEX IF NOT EXISTS idx_payment_provider_instances_enabled ON payment_provider_instances(enabled);

-- ----------------------------------------------------------------------------
-- 19) 官方 101_add_account_stats_pricing.sql 原文重放
-- ----------------------------------------------------------------------------

-- Account statistics pricing: allow channels to configure custom pricing for account cost tracking.

-- 1. Channel-level toggle
ALTER TABLE channels ADD COLUMN IF NOT EXISTS apply_pricing_to_account_stats BOOLEAN NOT NULL DEFAULT FALSE;

-- 2. Account stats pricing rules (ordered list per channel)
CREATE TABLE IF NOT EXISTS channel_account_stats_pricing_rules (
    id BIGSERIAL PRIMARY KEY,
    channel_id BIGINT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    name VARCHAR(100) NOT NULL DEFAULT '',
    group_ids BIGINT[] NOT NULL DEFAULT '{}',
    account_ids BIGINT[] NOT NULL DEFAULT '{}',
    sort_order INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_cas_pricing_rules_channel_id ON channel_account_stats_pricing_rules(channel_id);

-- 3. Model pricing for each rule (same structure as channel_model_pricing)
CREATE TABLE IF NOT EXISTS channel_account_stats_model_pricing (
    id BIGSERIAL PRIMARY KEY,
    rule_id BIGINT NOT NULL REFERENCES channel_account_stats_pricing_rules(id) ON DELETE CASCADE,
    platform VARCHAR(50) NOT NULL DEFAULT '',
    models JSONB NOT NULL DEFAULT '[]',
    billing_mode VARCHAR(20) NOT NULL DEFAULT 'token',
    input_price NUMERIC(20,10),
    output_price NUMERIC(20,10),
    cache_write_price NUMERIC(20,10),
    cache_read_price NUMERIC(20,10),
    image_output_price NUMERIC(20,10),
    per_request_price NUMERIC(20,10),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_cas_model_pricing_rule_id ON channel_account_stats_model_pricing(rule_id);

-- 4. Usage logs: pre-computed account stats cost (NULL = use default formula)
ALTER TABLE usage_logs ADD COLUMN IF NOT EXISTS account_stats_cost NUMERIC(20,10);

-- ----------------------------------------------------------------------------
-- 20) 官方 101_add_channel_features_config.sql 原文重放
-- ----------------------------------------------------------------------------

ALTER TABLE channels ADD COLUMN IF NOT EXISTS features_config JSONB NOT NULL DEFAULT '{}';
COMMENT ON COLUMN channels.features_config IS '渠道特性配置（如 web_search_emulation），JSON 对象格式';

-- ----------------------------------------------------------------------------
-- 21) 官方 101_add_payment_mode.sql 原文重放
-- ----------------------------------------------------------------------------

-- Add payment_mode field to payment_provider_instances
-- Values: 'redirect' (hosted page redirect), 'api' (API call for QR/payurl), '' (default/N/A)
ALTER TABLE payment_provider_instances ADD COLUMN IF NOT EXISTS payment_mode VARCHAR(20) NOT NULL DEFAULT '';

-- Migrate existing data: easypay instances with 'easypay' in supported_types → redirect mode
-- Remove 'easypay' from supported_types and set payment_mode = 'redirect'
UPDATE payment_provider_instances
SET payment_mode = 'redirect',
    supported_types = TRIM(BOTH ',' FROM REPLACE(REPLACE(REPLACE(
      supported_types, 'easypay,', ''), ',easypay', ''), 'easypay', ''))
WHERE provider_key = 'easypay' AND supported_types LIKE '%easypay%';

-- EasyPay instances without 'easypay' in supported_types → api mode
UPDATE payment_provider_instances
SET payment_mode = 'api'
WHERE provider_key = 'easypay' AND payment_mode = '';

-- ----------------------------------------------------------------------------
-- 22) 官方 102_add_out_trade_no_to_payment_orders.sql 原文重放
-- ----------------------------------------------------------------------------

-- 100_add_out_trade_no_to_payment_orders.sql
-- Adds out_trade_no column for external order ID used with payment providers.
-- Allows webhook handlers to look up orders by external ID instead of embedding DB ID.

ALTER TABLE payment_orders ADD COLUMN IF NOT EXISTS out_trade_no VARCHAR(64) NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS paymentorder_out_trade_no ON payment_orders (out_trade_no);

-- ----------------------------------------------------------------------------
-- 23) 官方 103_add_allow_user_refund.sql 原文重放
-- ----------------------------------------------------------------------------

ALTER TABLE payment_provider_instances ADD COLUMN IF NOT EXISTS allow_user_refund BOOLEAN NOT NULL DEFAULT false;

-- ----------------------------------------------------------------------------
-- 24) 官方 106_add_account_stats_pricing_intervals.sql 原文重放
-- ----------------------------------------------------------------------------

-- Add intervals table for account stats pricing rules (mirrors channel_pricing_intervals).
CREATE TABLE IF NOT EXISTS channel_account_stats_pricing_intervals (
    id                BIGSERIAL      PRIMARY KEY,
    pricing_id        BIGINT         NOT NULL REFERENCES channel_account_stats_model_pricing(id) ON DELETE CASCADE,
    min_tokens        INT            NOT NULL DEFAULT 0,
    max_tokens        INT,
    tier_label        VARCHAR(50),
    input_price       NUMERIC(20,12),
    output_price      NUMERIC(20,12),
    cache_write_price NUMERIC(20,12),
    cache_read_price  NUMERIC(20,12),
    per_request_price NUMERIC(20,12),
    sort_order        INT            NOT NULL DEFAULT 0,
    created_at        TIMESTAMPTZ    NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ    NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_account_stats_pricing_intervals_pricing_id
    ON channel_account_stats_pricing_intervals (pricing_id);

-- ----------------------------------------------------------------------------
-- 25) 官方 112_add_payment_order_provider_key_snapshot.sql 原文重放
-- ----------------------------------------------------------------------------

ALTER TABLE payment_orders ADD COLUMN IF NOT EXISTS provider_key VARCHAR(30);

UPDATE payment_orders
SET provider_key = (
    SELECT provider_key
    FROM payment_provider_instances
    WHERE CAST(id AS TEXT) = payment_orders.provider_instance_id
)
WHERE provider_key IS NULL
  AND provider_instance_id IS NOT NULL;

-- ----------------------------------------------------------------------------
-- 26) 官方 117_add_payment_order_provider_snapshot.sql 原文重放
-- ----------------------------------------------------------------------------

ALTER TABLE payment_orders
ADD COLUMN IF NOT EXISTS provider_snapshot JSONB;

-- ----------------------------------------------------------------------------
-- 27) 官方 119_enforce_payment_orders_out_trade_no_unique.sql 原文重放
-- ----------------------------------------------------------------------------

-- Intentionally left as a no-op.
-- The online index rollout lives in 120_enforce_payment_orders_out_trade_no_unique_notx.sql
DO $$
BEGIN
    NULL;
END $$;

-- ----------------------------------------------------------------------------
-- 28) 官方 120_enforce_payment_orders_out_trade_no_unique_notx.sql 原文重放
-- ----------------------------------------------------------------------------

-- Build the payment order uniqueness guarantee online.
-- The migration runner performs an explicit duplicate out_trade_no precheck and
-- drops any stale invalid paymentorder_out_trade_no_unique index before retrying.
-- Create the new partial unique index concurrently first so writes keep flowing,
-- then remove the legacy index name once the replacement is ready.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS paymentorder_out_trade_no_unique
    ON payment_orders (out_trade_no)
    WHERE out_trade_no <> '';

DROP INDEX CONCURRENTLY IF EXISTS paymentorder_out_trade_no;

-- ----------------------------------------------------------------------------
-- 28a) 官方 120a_align_payment_orders_out_trade_no_index_name.sql 原文重放
--      （把 120 建出的 paymentorder_out_trade_no_unique 改名回官方终态名
--       paymentorder_out_trade_no；自带存在性守卫，幂等）
-- ----------------------------------------------------------------------------
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM pg_indexes
        WHERE schemaname = 'public'
          AND tablename = 'payment_orders'
          AND indexname = 'paymentorder_out_trade_no_unique'
    ) THEN
        IF EXISTS (
            SELECT 1
            FROM pg_indexes
            WHERE schemaname = 'public'
              AND tablename = 'payment_orders'
              AND indexname = 'paymentorder_out_trade_no'
        ) THEN
            EXECUTE 'DROP INDEX IF EXISTS paymentorder_out_trade_no';
        END IF;

        EXECUTE 'ALTER INDEX paymentorder_out_trade_no_unique RENAME TO paymentorder_out_trade_no';
    END IF;
END $$;

-- ----------------------------------------------------------------------------
-- 29) 官方 149_proxy_expiry_fallback.sql 原文重放
-- ----------------------------------------------------------------------------

-- proxies: 有效期 + 失败回退
ALTER TABLE proxies ADD COLUMN IF NOT EXISTS expires_at timestamptz;
ALTER TABLE proxies ADD COLUMN IF NOT EXISTS fallback_mode varchar(20) NOT NULL DEFAULT 'none';
ALTER TABLE proxies ADD COLUMN IF NOT EXISTS backup_proxy_id BIGINT REFERENCES proxies(id) ON DELETE SET NULL;
ALTER TABLE proxies ADD COLUMN IF NOT EXISTS expiry_warn_days INT NOT NULL DEFAULT 7;
CREATE INDEX IF NOT EXISTS proxies_expires_at_idx ON proxies (expires_at);
CREATE INDEX IF NOT EXISTS proxies_backup_proxy_id_idx ON proxies (backup_proxy_id);

-- accounts: fallback 来源（手动回切用）
ALTER TABLE accounts ADD COLUMN IF NOT EXISTS proxy_fallback_origin_id BIGINT;
CREATE INDEX IF NOT EXISTS accounts_proxy_fallback_origin_id_idx ON accounts (proxy_fallback_origin_id);

-- ----------------------------------------------------------------------------
-- 30) 官方 177_add_subscription_plan_currency.sql 原文重放
-- ----------------------------------------------------------------------------

-- Display-only ISO 4217 currency label for subscription plan prices; empty
-- keeps existing plans rendering without any label.
ALTER TABLE subscription_plans
    ADD COLUMN IF NOT EXISTS currency VARCHAR(3) NOT NULL DEFAULT '';

-- ----------------------------------------------------------------------------
-- 31) 官方 178_channel_image_input_price.sql 原文重放
-- ----------------------------------------------------------------------------

-- 178_channel_image_input_price.sql
-- 渠道自定义定价（token 模式）新增图片输入 token 单价列。
-- 用于 gpt-image-2 等模型的图片编辑/图生图请求：上游 usage 的
-- input_tokens_details.image_tokens 需按独立单价计费，区别于文本 input_price。
-- 未配置（NULL）时计费回退到文本输入价，保持向后兼容。

ALTER TABLE channel_model_pricing
    ADD COLUMN IF NOT EXISTS image_input_price NUMERIC(20,12);

-- ----------------------------------------------------------------------------
-- 32) 官方 191_passkey_credentials.sql 原文重放
-- ----------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS passkey_user_handles (
    user_id BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    user_handle BYTEA NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS passkey_credentials (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    credential_id BYTEA NOT NULL UNIQUE,
    name VARCHAR(100) NOT NULL DEFAULT 'Passkey',
    credential_data JSONB NOT NULL,
    last_used_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS passkey_credentials_user_id_idx
    ON passkey_credentials (user_id);

CREATE INDEX IF NOT EXISTS passkey_credentials_last_used_at_idx
    ON passkey_credentials (last_used_at);

-- ----------------------------------------------------------------------------
-- 33) 官方 225_channel_model_time_pricing.sql 原文重放
-- ----------------------------------------------------------------------------

ALTER TABLE channel_model_pricing
    ADD COLUMN IF NOT EXISTS time_pricing JSONB NULL;

COMMENT ON COLUMN channel_model_pricing.time_pricing IS
    'Optional IANA timezone and recurring daily multiplier periods for channel token pricing';

-- ----------------------------------------------------------------------------
-- 34) 官方 228_channel_pricing_multipliers.sql 原文重放
-- ----------------------------------------------------------------------------

ALTER TABLE channel_model_pricing
    ADD COLUMN IF NOT EXISTS fast_multiplier NUMERIC(12,6),
    ADD COLUMN IF NOT EXISTS flex_multiplier NUMERIC(12,6);

ALTER TABLE channel_pricing_intervals
    ADD COLUMN IF NOT EXISTS input_multiplier NUMERIC(12,6),
    ADD COLUMN IF NOT EXISTS output_multiplier NUMERIC(12,6),
    ADD COLUMN IF NOT EXISTS cache_write_multiplier NUMERIC(12,6),
    ADD COLUMN IF NOT EXISTS cache_read_multiplier NUMERIC(12,6);

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'channel_model_pricing_fast_multiplier_positive' AND conrelid = 'channel_model_pricing'::regclass) THEN
        ALTER TABLE channel_model_pricing
            ADD CONSTRAINT channel_model_pricing_fast_multiplier_positive
            CHECK (fast_multiplier IS NULL OR fast_multiplier > 0);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'channel_model_pricing_flex_multiplier_positive' AND conrelid = 'channel_model_pricing'::regclass) THEN
        ALTER TABLE channel_model_pricing
            ADD CONSTRAINT channel_model_pricing_flex_multiplier_positive
            CHECK (flex_multiplier IS NULL OR flex_multiplier > 0);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'channel_pricing_intervals_input_multiplier_positive' AND conrelid = 'channel_pricing_intervals'::regclass) THEN
        ALTER TABLE channel_pricing_intervals
            ADD CONSTRAINT channel_pricing_intervals_input_multiplier_positive
            CHECK (input_multiplier IS NULL OR input_multiplier > 0);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'channel_pricing_intervals_output_multiplier_positive' AND conrelid = 'channel_pricing_intervals'::regclass) THEN
        ALTER TABLE channel_pricing_intervals
            ADD CONSTRAINT channel_pricing_intervals_output_multiplier_positive
            CHECK (output_multiplier IS NULL OR output_multiplier > 0);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'channel_pricing_intervals_cache_write_multiplier_positive' AND conrelid = 'channel_pricing_intervals'::regclass) THEN
        ALTER TABLE channel_pricing_intervals
            ADD CONSTRAINT channel_pricing_intervals_cache_write_multiplier_positive
            CHECK (cache_write_multiplier IS NULL OR cache_write_multiplier > 0);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'channel_pricing_intervals_cache_read_multiplier_positive' AND conrelid = 'channel_pricing_intervals'::regclass) THEN
        ALTER TABLE channel_pricing_intervals
            ADD CONSTRAINT channel_pricing_intervals_cache_read_multiplier_positive
            CHECK (cache_read_multiplier IS NULL OR cache_read_multiplier > 0);
    END IF;
END $$;

COMMENT ON COLUMN channel_model_pricing.fast_multiplier IS
    'Fast/priority service tier multiplier applied to the selected standard channel price';
COMMENT ON COLUMN channel_model_pricing.flex_multiplier IS
    'Flex service tier multiplier applied to the selected standard channel price';
COMMENT ON COLUMN channel_pricing_intervals.input_multiplier IS
    'Interval input multiplier applied to the channel base input price when input_price is NULL';
COMMENT ON COLUMN channel_pricing_intervals.output_multiplier IS
    'Interval output multiplier applied to the channel base output price when output_price is NULL';
COMMENT ON COLUMN channel_pricing_intervals.cache_write_multiplier IS
    'Interval cache-write multiplier applied to the channel base cache-write price when cache_write_price is NULL';
COMMENT ON COLUMN channel_pricing_intervals.cache_read_multiplier IS
    'Interval cache-read multiplier applied to the channel base cache-read price when cache_read_price is NULL';

-- ----------------------------------------------------------------------------
-- 35) 官方 232_channel_cache_write_1h_pricing.sql 原文重放
-- ----------------------------------------------------------------------------

ALTER TABLE channel_model_pricing
    ADD COLUMN IF NOT EXISTS cache_write_1h_price NUMERIC(20,12);

ALTER TABLE channel_pricing_intervals
    ADD COLUMN IF NOT EXISTS cache_write_1h_price NUMERIC(20,12);

ALTER TABLE channel_account_stats_model_pricing
    ADD COLUMN IF NOT EXISTS cache_write_1h_price NUMERIC(20,12);

ALTER TABLE channel_account_stats_pricing_intervals
    ADD COLUMN IF NOT EXISTS cache_write_1h_price NUMERIC(20,12);

COMMENT ON COLUMN channel_model_pricing.cache_write_1h_price IS
    '1h cache write price per token; NULL preserves legacy cache_write_price behavior';
COMMENT ON COLUMN channel_pricing_intervals.cache_write_1h_price IS
    'Interval-specific 1h cache write price per token';
COMMENT ON COLUMN channel_account_stats_model_pricing.cache_write_1h_price IS
    '1h cache write price per token for account stats pricing';
COMMENT ON COLUMN channel_account_stats_pricing_intervals.cache_write_1h_price IS
    'Interval-specific 1h cache write price per token for account stats pricing';

-- ----------------------------------------------------------------------------
-- 36) 官方 234_channel_max_reasoning_effort_multiplier.sql 原文重放
-- ----------------------------------------------------------------------------

ALTER TABLE channel_model_pricing
    ADD COLUMN IF NOT EXISTS max_reasoning_effort_multiplier NUMERIC(10,4);

COMMENT ON COLUMN channel_model_pricing.max_reasoning_effort_multiplier IS
    'Billing/quota multiplier applied when the forwarded reasoning effort is max';

ALTER TABLE channel_model_pricing
    DROP CONSTRAINT IF EXISTS chk_channel_model_pricing_max_reasoning_effort_multiplier_positive;
ALTER TABLE channel_model_pricing
    ADD CONSTRAINT chk_channel_model_pricing_max_reasoning_effort_multiplier_positive
    CHECK (max_reasoning_effort_multiplier IS NULL OR max_reasoning_effort_multiplier > 0);

-- ----------------------------------------------------------------------------
-- ----------------------------------------------------------------------------
-- 36b) 官方 108_auth_identity_foundation_core.sql 原文重放
--      （auth_identities / auth_identity_channels / pending_auth_sessions /
--        identity_adoption_decisions 四表；116 为一次性数据修复，空表无需）
-- ----------------------------------------------------------------------------
ALTER TABLE users
ADD COLUMN IF NOT EXISTS signup_source VARCHAR(20) NOT NULL DEFAULT 'email',
ADD COLUMN IF NOT EXISTS last_login_at TIMESTAMPTZ NULL,
ADD COLUMN IF NOT EXISTS last_active_at TIMESTAMPTZ NULL;

UPDATE users
SET signup_source = 'email'
WHERE signup_source IS NULL OR signup_source = '';

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'users_signup_source_check'
    ) THEN
        ALTER TABLE users
            ADD CONSTRAINT users_signup_source_check
            CHECK (signup_source IN ('email', 'linuxdo', 'wechat', 'oidc'));
    END IF;
END $$;

CREATE TABLE IF NOT EXISTS auth_identities (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider_type VARCHAR(20) NOT NULL,
    provider_key TEXT NOT NULL,
    provider_subject TEXT NOT NULL,
    verified_at TIMESTAMPTZ NULL,
    issuer TEXT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT auth_identities_provider_type_check
        CHECK (provider_type IN ('email', 'linuxdo', 'wechat', 'oidc'))
);

CREATE UNIQUE INDEX IF NOT EXISTS auth_identities_provider_subject_key
    ON auth_identities (provider_type, provider_key, provider_subject);

CREATE INDEX IF NOT EXISTS auth_identities_user_id_idx
    ON auth_identities (user_id);

CREATE INDEX IF NOT EXISTS auth_identities_user_provider_idx
    ON auth_identities (user_id, provider_type);

CREATE TABLE IF NOT EXISTS auth_identity_channels (
    id BIGSERIAL PRIMARY KEY,
    identity_id BIGINT NOT NULL REFERENCES auth_identities(id) ON DELETE CASCADE,
    provider_type VARCHAR(20) NOT NULL,
    provider_key TEXT NOT NULL,
    channel VARCHAR(20) NOT NULL,
    channel_app_id TEXT NOT NULL,
    channel_subject TEXT NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT auth_identity_channels_provider_type_check
        CHECK (provider_type IN ('email', 'linuxdo', 'wechat', 'oidc'))
);

CREATE UNIQUE INDEX IF NOT EXISTS auth_identity_channels_channel_key
    ON auth_identity_channels (provider_type, provider_key, channel, channel_app_id, channel_subject);

CREATE INDEX IF NOT EXISTS auth_identity_channels_identity_id_idx
    ON auth_identity_channels (identity_id);

CREATE TABLE IF NOT EXISTS pending_auth_sessions (
    id BIGSERIAL PRIMARY KEY,
    session_token VARCHAR(255) NOT NULL,
    intent VARCHAR(40) NOT NULL,
    provider_type VARCHAR(20) NOT NULL,
    provider_key TEXT NOT NULL,
    provider_subject TEXT NOT NULL,
    target_user_id BIGINT NULL REFERENCES users(id) ON DELETE SET NULL,
    redirect_to TEXT NOT NULL DEFAULT '',
    resolved_email TEXT NOT NULL DEFAULT '',
    registration_password_hash TEXT NOT NULL DEFAULT '',
    upstream_identity_claims JSONB NOT NULL DEFAULT '{}'::jsonb,
    local_flow_state JSONB NOT NULL DEFAULT '{}'::jsonb,
    browser_session_key TEXT NOT NULL DEFAULT '',
    completion_code_hash TEXT NOT NULL DEFAULT '',
    completion_code_expires_at TIMESTAMPTZ NULL,
    email_verified_at TIMESTAMPTZ NULL,
    password_verified_at TIMESTAMPTZ NULL,
    totp_verified_at TIMESTAMPTZ NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT pending_auth_sessions_intent_check
        CHECK (intent IN ('login', 'bind_current_user', 'adopt_existing_user_by_email')),
    CONSTRAINT pending_auth_sessions_provider_type_check
        CHECK (provider_type IN ('email', 'linuxdo', 'wechat', 'oidc'))
);

CREATE UNIQUE INDEX IF NOT EXISTS pending_auth_sessions_session_token_key
    ON pending_auth_sessions (session_token);

CREATE INDEX IF NOT EXISTS pending_auth_sessions_target_user_id_idx
    ON pending_auth_sessions (target_user_id);

CREATE INDEX IF NOT EXISTS pending_auth_sessions_expires_at_idx
    ON pending_auth_sessions (expires_at);

CREATE INDEX IF NOT EXISTS pending_auth_sessions_provider_idx
    ON pending_auth_sessions (provider_type, provider_key, provider_subject);

CREATE INDEX IF NOT EXISTS pending_auth_sessions_completion_code_idx
    ON pending_auth_sessions (completion_code_hash);

CREATE TABLE IF NOT EXISTS identity_adoption_decisions (
    id BIGSERIAL PRIMARY KEY,
    pending_auth_session_id BIGINT NOT NULL REFERENCES pending_auth_sessions(id) ON DELETE CASCADE,
    identity_id BIGINT NULL REFERENCES auth_identities(id) ON DELETE SET NULL,
    adopt_display_name BOOLEAN NOT NULL DEFAULT FALSE,
    adopt_avatar BOOLEAN NOT NULL DEFAULT FALSE,
    decided_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS identity_adoption_decisions_pending_auth_session_id_key
    ON identity_adoption_decisions (pending_auth_session_id);

CREATE INDEX IF NOT EXISTS identity_adoption_decisions_identity_id_idx
    ON identity_adoption_decisions (identity_id);

CREATE TABLE IF NOT EXISTS auth_identity_migration_reports (
    id BIGSERIAL PRIMARY KEY,
    report_type VARCHAR(40) NOT NULL,
    report_key TEXT NOT NULL,
    details JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS auth_identity_migration_reports_type_idx
    ON auth_identity_migration_reports (report_type);

CREATE UNIQUE INDEX IF NOT EXISTS auth_identity_migration_reports_type_key
    ON auth_identity_migration_reports (report_type, report_key);

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'auth_identities_metadata_is_object_check'
    ) THEN
        ALTER TABLE auth_identities
            ADD CONSTRAINT auth_identities_metadata_is_object_check
            CHECK (jsonb_typeof(metadata) = 'object');
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'auth_identity_channels_metadata_is_object_check'
    ) THEN
        ALTER TABLE auth_identity_channels
            ADD CONSTRAINT auth_identity_channels_metadata_is_object_check
            CHECK (jsonb_typeof(metadata) = 'object');
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'auth_identity_migration_reports_details_is_object_check'
    ) THEN
        ALTER TABLE auth_identity_migration_reports
            ADD CONSTRAINT auth_identity_migration_reports_details_is_object_check
            CHECK (jsonb_typeof(details) = 'object');
    END IF;
END $$;

-- 37) 官方 134_affiliate_ledger_audit_snapshots.sql 索引段节选
--     （source_order_id 列仍在，仅恢复被 239_so 删除的索引；FK 见末尾补丁 b）
-- ----------------------------------------------------------------------------
CREATE INDEX IF NOT EXISTS idx_user_affiliate_ledger_source_order_id
    ON user_affiliate_ledger(source_order_id)
    WHERE source_order_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_user_affiliate_ledger_rebate_lookup
    ON user_affiliate_ledger(action, source_order_id, user_id, source_user_id, created_at)
    WHERE action = 'accrue';

-- 37a) 官方 131_affiliate_rebate_hardening.sql 段 3 节选
--      （payment_audit_logs 的 (order_id, action) 唯一索引；建索引前先按官方
--       原文去重历史行，保证唯一索引可建。段 1/4 为数据操作，so 序列已执行过
--       官方 131 原文，不重放）
WITH ranked AS (
    SELECT id,
           ROW_NUMBER() OVER (PARTITION BY order_id, action ORDER BY id) AS rn
    FROM payment_audit_logs
)
DELETE FROM payment_audit_logs p
USING ranked r
WHERE p.id = r.id
  AND r.rn > 1;

CREATE UNIQUE INDEX IF NOT EXISTS idx_payment_audit_logs_order_action_uniq
ON payment_audit_logs(order_id, action);

-- ----------------------------------------------------------------------------
-- 38) 官方 184_auth_cache_invalidation_outbox.sql 的 api_keys 触发器函数原文
--     （240_so 曾以 CREATE OR REPLACE 删除 ip_whitelist/ip_blacklist 两行比较，
--      此处还原官方函数体；触发器 trg_api_keys_auth_cache_invalidation 仍存在，
--      函数替换后立即生效，无需重建触发器）
-- ----------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION enqueue_api_key_auth_cache_invalidation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        PERFORM enqueue_auth_cache_invalidation(OLD.key);
        RETURN OLD;
    END IF;

    IF OLD.key IS DISTINCT FROM NEW.key
       OR OLD.status IS DISTINCT FROM NEW.status
       OR OLD.deleted_at IS DISTINCT FROM NEW.deleted_at
       OR OLD.user_id IS DISTINCT FROM NEW.user_id
       OR OLD.group_id IS DISTINCT FROM NEW.group_id
       OR OLD.ip_whitelist IS DISTINCT FROM NEW.ip_whitelist
       OR OLD.ip_blacklist IS DISTINCT FROM NEW.ip_blacklist
       OR OLD.expires_at IS DISTINCT FROM NEW.expires_at THEN
        PERFORM enqueue_auth_cache_invalidation(OLD.key);
        IF NEW.deleted_at IS NULL AND NEW.key IS DISTINCT FROM OLD.key THEN
            PERFORM enqueue_auth_cache_invalidation(NEW.key);
        END IF;
    END IF;
    RETURN NEW;
END;
$$;

-- ----------------------------------------------------------------------------
-- 39) 手写补丁 a：accounts.proxy_id（官方 001 内嵌于 CREATE TABLE accounts，
--     表已存在导致整语句被跳过，必须显式 ALTER 恢复）
-- ----------------------------------------------------------------------------
ALTER TABLE accounts ADD COLUMN IF NOT EXISTS proxy_id BIGINT REFERENCES proxies(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_accounts_proxy_id ON accounts(proxy_id);

-- ----------------------------------------------------------------------------
-- 40) 手写补丁 b：user_affiliate_ledger 的 source_order_id 外键
--     （官方 134 以 ADD COLUMN ... REFERENCES 一并建 FK，列已存在会被 IF NOT EXISTS
--      跳过导致 FK 缺失，需显式恢复约束）
-- ----------------------------------------------------------------------------
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'user_affiliate_ledger_source_order_id_fkey'
          AND conrelid = 'user_affiliate_ledger'::regclass
    ) THEN
        ALTER TABLE user_affiliate_ledger
            ADD CONSTRAINT user_affiliate_ledger_source_order_id_fkey
            FOREIGN KEY (source_order_id) REFERENCES payment_orders(id) ON DELETE SET NULL;
    END IF;
END $$;

-- ----------------------------------------------------------------------------
-- 41) 官方 239_channel_reasoning_effort_multipliers.sql 原文重放
--     （渠道定价表 reasoning_effort_multipliers 列 + groups.model_pricing legacy 键转换，
--       提前重放使恢复后 schema 即与官方完整一致；官方二进制首启重放时幂等跳过）
-- ----------------------------------------------------------------------------
-- Migrate only explicitly configured legacy max pricing. There is no model-specific default.
-- Run the backfill only when introducing the column: replaying this migration must
-- preserve current settings, including maps that have deliberately been cleared.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_attribute
        WHERE attrelid = 'channel_model_pricing'::regclass
          AND attname = 'reasoning_effort_multipliers'
          AND NOT attisdropped
    ) THEN
        ALTER TABLE channel_model_pricing
            ADD COLUMN IF NOT EXISTS reasoning_effort_multipliers JSONB NOT NULL DEFAULT '{}'::jsonb;

        UPDATE channel_model_pricing
        SET reasoning_effort_multipliers = jsonb_build_object('max', max_reasoning_effort_multiplier)
        WHERE max_reasoning_effort_multiplier IS NOT NULL;
    END IF;
END $$;

ALTER TABLE channel_account_stats_model_pricing
    ADD COLUMN IF NOT EXISTS reasoning_effort_multipliers JSONB NOT NULL DEFAULT '{}'::jsonb;

-- Group pricing stores the same entries as JSON. An existing generic map wins,
-- even when empty. Remove the consumed legacy key so replay cannot revive it.
UPDATE groups AS g
SET model_pricing = (
    SELECT jsonb_agg(
        CASE
            WHEN jsonb_typeof(entry) = 'object' AND entry ? 'max_reasoning_effort_multiplier' THEN
                (entry - 'max_reasoning_effort_multiplier') ||
                CASE
                    WHEN NOT (entry ? 'reasoning_effort_multipliers')
                         AND jsonb_typeof(entry->'max_reasoning_effort_multiplier') = 'number' THEN
                        jsonb_build_object('reasoning_effort_multipliers',
                            jsonb_build_object('max', entry->'max_reasoning_effort_multiplier'))
                    ELSE '{}'::jsonb
                END
            ELSE entry
        END ORDER BY ordinal
    )
    FROM jsonb_array_elements(g.model_pricing) WITH ORDINALITY AS pricing(entry, ordinal)
)
WHERE jsonb_typeof(g.model_pricing) = 'array'
  AND EXISTS (
      SELECT 1
      FROM jsonb_array_elements(
          CASE WHEN jsonb_typeof(g.model_pricing) = 'array' THEN g.model_pricing ELSE '[]'::jsonb END
      ) AS pricing(entry)
      WHERE jsonb_typeof(entry) = 'object' AND entry ? 'max_reasoning_effort_multiplier'
  );

COMMENT ON COLUMN channel_model_pricing.reasoning_effort_multipliers IS
    'Custom billing multipliers by reasoning effort; omitted efforts use 1x';
COMMENT ON COLUMN channel_account_stats_model_pricing.reasoning_effort_multipliers IS
    'Custom account statistics billing multipliers by reasoning effort; omitted efforts use 1x';

-- ----------------------------------------------------------------------------
-- 41a) 官方 238b_content_moderation_engine_meta.sql 原文重放
--      （审计溯源列；老行保持 NULL）
-- ----------------------------------------------------------------------------
ALTER TABLE content_moderation_logs ADD COLUMN IF NOT EXISTS engine_meta JSONB;

-- ----------------------------------------------------------------------------
-- 41b) 官方 240_affiliate_ledger_operation_id.sql 原文重放
--      （管理员登记线下提现的幂等标识列 + 部分唯一索引）
-- ----------------------------------------------------------------------------
ALTER TABLE user_affiliate_ledger
    ADD COLUMN IF NOT EXISTS operation_id VARCHAR(64);

COMMENT ON COLUMN user_affiliate_ledger.operation_id IS '管理员登记线下提现的幂等标识（由 Idempotency-Key 派生的 SHA-256 十六进制串）；其他流水为 NULL';

CREATE UNIQUE INDEX IF NOT EXISTS idx_user_affiliate_ledger_operation_id
    ON user_affiliate_ledger (operation_id)
    WHERE operation_id IS NOT NULL;

-- ============================================================================
-- 恢复完成。官方 239（已含于段 41）之后的官方迁移均已在上方重放到位；
-- 官方二进制首次启动时 schema_migrations 按文件名判重跳过 001–238，
-- 238b/239/240 无记录将重放（IF NOT EXISTS / DO 守卫，幂等无害）。
-- ============================================================================

