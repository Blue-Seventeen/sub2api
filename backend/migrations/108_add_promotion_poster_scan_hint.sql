ALTER TABLE promotion_settings
ADD COLUMN IF NOT EXISTS poster_scan_hint TEXT;

UPDATE promotion_settings
SET poster_scan_hint = COALESCE(NULLIF(poster_scan_hint, ''), '扫码快速注册');
