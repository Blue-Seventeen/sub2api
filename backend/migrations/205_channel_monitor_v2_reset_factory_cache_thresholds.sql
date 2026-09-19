-- Migration 203 preserves migration 198's zero cache thresholds in this fork.
-- Existing nonzero thresholds may be deliberate; do not infer operator intent.
SELECT 1;
