-- 增量兼容迁移：
-- 1. users 新增统一倍率字段
-- 2. usage_logs 新增真实消费与统一倍率快照字段
-- 3. dashboard 预聚合表新增真实消费字段
-- 注意：仅新增字段，不修改/删除任何既有字段，便于后续跟随上游升级

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS unified_rate_enabled BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS unified_rate_multiplier DECIMAL(10, 4) NOT NULL DEFAULT 1;

COMMENT ON COLUMN users.unified_rate_enabled IS '是否启用用户专属统一倍率';
COMMENT ON COLUMN users.unified_rate_multiplier IS '用户专属统一倍率（允许 0；关闭时按 1 处理）';

ALTER TABLE usage_logs
    ADD COLUMN IF NOT EXISTS real_actual_cost DECIMAL(20, 10) NOT NULL DEFAULT 0;

ALTER TABLE usage_logs
    ADD COLUMN IF NOT EXISTS unified_rate_multiplier DECIMAL(10, 4) NOT NULL DEFAULT 1;

COMMENT ON COLUMN usage_logs.real_actual_cost IS '管理员真实消费口径（不受统一倍率放大影响）';
COMMENT ON COLUMN usage_logs.unified_rate_multiplier IS '写入日志时的用户统一倍率快照';

UPDATE usage_logs
SET real_actual_cost = actual_cost
WHERE COALESCE(real_actual_cost, 0) = 0
  AND COALESCE(actual_cost, 0) <> 0;

ALTER TABLE usage_dashboard_hourly
    ADD COLUMN IF NOT EXISTS real_actual_cost DECIMAL(20, 10) NOT NULL DEFAULT 0;

ALTER TABLE usage_dashboard_daily
    ADD COLUMN IF NOT EXISTS real_actual_cost DECIMAL(20, 10) NOT NULL DEFAULT 0;

COMMENT ON COLUMN usage_dashboard_hourly.real_actual_cost IS '管理员真实消费口径小时聚合';
COMMENT ON COLUMN usage_dashboard_daily.real_actual_cost IS '管理员真实消费口径日聚合';

UPDATE usage_dashboard_hourly
SET real_actual_cost = actual_cost
WHERE COALESCE(real_actual_cost, 0) = 0
  AND COALESCE(actual_cost, 0) <> 0;

UPDATE usage_dashboard_daily
SET real_actual_cost = actual_cost
WHERE COALESCE(real_actual_cost, 0) = 0
  AND COALESCE(actual_cost, 0) <> 0;
