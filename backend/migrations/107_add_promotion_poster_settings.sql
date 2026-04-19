ALTER TABLE promotion_settings
ADD COLUMN IF NOT EXISTS invite_base_url TEXT;

ALTER TABLE promotion_settings
ADD COLUMN IF NOT EXISTS poster_logo_url TEXT;

ALTER TABLE promotion_settings
ADD COLUMN IF NOT EXISTS poster_title TEXT;

ALTER TABLE promotion_settings
ADD COLUMN IF NOT EXISTS poster_headline TEXT;

ALTER TABLE promotion_settings
ADD COLUMN IF NOT EXISTS poster_description TEXT;

ALTER TABLE promotion_settings
ADD COLUMN IF NOT EXISTS poster_tags_json TEXT;

UPDATE promotion_settings
SET invite_base_url = COALESCE(NULLIF(invite_base_url, ''), ''),
    poster_logo_url = COALESCE(NULLIF(poster_logo_url, ''), ''),
    poster_title = COALESCE(NULLIF(poster_title, ''), 'Sub2API'),
    poster_headline = COALESCE(NULLIF(poster_headline, ''), '邀请好友，一起把消费返佣赚回来'),
    poster_description = COALESCE(NULLIF(poster_description, ''), '一级返利 + 二级返利 + 激活奖励，统一结算到真实余额。'),
    poster_tags_json = COALESCE(NULLIF(poster_tags_json, ''), '["真实消费返佣","次日结算","唯一推广码"]');
