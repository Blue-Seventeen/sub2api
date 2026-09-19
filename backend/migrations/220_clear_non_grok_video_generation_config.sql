-- Custom groups may retain video prices regardless of their current platform.
-- Keep this upstream migration slot as a no-op: upgrades must not erase prices.
SELECT 1;
