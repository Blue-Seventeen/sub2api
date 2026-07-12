-- 173_add_proxy_subscription_raw_dns_config.sql
-- Store the top-level Clash/Mihomo dns config from proxy subscriptions,
-- especially dns.nameserver-policy used to resolve managed proxy node domains.

ALTER TABLE proxy_subscriptions
    ADD COLUMN IF NOT EXISTS raw_dns_config TEXT;

COMMENT ON COLUMN proxy_subscriptions.raw_dns_config IS 'Raw YAML of subscription top-level dns config (e.g. nameserver-policy); used to build Mihomo runtime DNS.';
