-- Compatibility guardrail for fork upgrades and upstream-to-fork database switches.
-- Ensure every auth provider check constraint includes DingTalk after GitHub/Google
-- OAuth and DingTalk OAuth migrations have both been absorbed.

ALTER TABLE IF EXISTS users
    DROP CONSTRAINT IF EXISTS users_signup_source_check;

ALTER TABLE IF EXISTS users
    ADD CONSTRAINT users_signup_source_check
    CHECK (signup_source IN ('email', 'linuxdo', 'wechat', 'oidc', 'github', 'google', 'dingtalk'));

ALTER TABLE IF EXISTS auth_identities
    DROP CONSTRAINT IF EXISTS auth_identities_provider_type_check;

ALTER TABLE IF EXISTS auth_identities
    ADD CONSTRAINT auth_identities_provider_type_check
    CHECK (provider_type IN ('email', 'linuxdo', 'wechat', 'oidc', 'github', 'google', 'dingtalk'));

ALTER TABLE IF EXISTS auth_identity_channels
    DROP CONSTRAINT IF EXISTS auth_identity_channels_provider_type_check;

ALTER TABLE IF EXISTS auth_identity_channels
    ADD CONSTRAINT auth_identity_channels_provider_type_check
    CHECK (provider_type IN ('email', 'linuxdo', 'wechat', 'oidc', 'github', 'google', 'dingtalk'));

ALTER TABLE IF EXISTS pending_auth_sessions
    DROP CONSTRAINT IF EXISTS pending_auth_sessions_provider_type_check;

ALTER TABLE IF EXISTS pending_auth_sessions
    ADD CONSTRAINT pending_auth_sessions_provider_type_check
    CHECK (provider_type IN ('email', 'linuxdo', 'wechat', 'oidc', 'github', 'google', 'dingtalk'));

ALTER TABLE IF EXISTS user_provider_default_grants
    DROP CONSTRAINT IF EXISTS user_provider_default_grants_provider_type_check;

ALTER TABLE IF EXISTS user_provider_default_grants
    ADD CONSTRAINT user_provider_default_grants_provider_type_check
    CHECK (provider_type IN ('email', 'linuxdo', 'wechat', 'oidc', 'github', 'google', 'dingtalk'));
