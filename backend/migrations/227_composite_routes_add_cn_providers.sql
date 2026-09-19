-- Preserve every custom route platform while adding the new CN providers.
-- A narrower intermediate constraint would fail before migration 237 can run.
ALTER TABLE composite_model_routes
    DROP CONSTRAINT IF EXISTS composite_model_routes_target_platform_check;

ALTER TABLE composite_model_routes
    ADD CONSTRAINT composite_model_routes_target_platform_check
    CHECK (target_platform IN ('anthropic', 'openai', 'gemini', 'antigravity', 'grok',
                               'kimi', 'zhipu', 'deepseek', 'minimax', 'volcengine',
                               'ali', 'moonshot', 'perplexity', 'mistral', 'siliconflow',
                               'openrouter', 'suno', 'kling', 'midjourney'));
