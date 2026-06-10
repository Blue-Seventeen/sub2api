-- Ops retry/replay storage is no longer used by current code, but older fork
-- installs may already contain these columns/tables from migrations 033 and
-- 038. Keep this migration data-preserving so low-version upgrades do not
-- destroy historical request/retry evidence.

COMMENT ON TABLE ops_error_logs IS 'Ops error logs (vNext). Stores sanitized error details; legacy retry/replay columns are preserved when present for upgrade compatibility.';
