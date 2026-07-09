-- 151_add_proxy_subscription_raw_dns_config.sql
-- 保存 Clash/Mihomo 订阅顶层 dns 配置（尤其是 dns.nameserver-policy）。
-- managed proxy runtime 会用它恢复订阅自带的 DNS 策略来解析节点域名，
-- 避免用系统 DNS 把 policy 域名错误解析成内网/loopback 地址而导致导入失败。

ALTER TABLE proxy_subscriptions
    ADD COLUMN IF NOT EXISTS raw_dns_config TEXT;

COMMENT ON COLUMN proxy_subscriptions.raw_dns_config IS 'Raw YAML of subscription top-level dns config (e.g. nameserver-policy); used to build Mihomo runtime DNS.';
