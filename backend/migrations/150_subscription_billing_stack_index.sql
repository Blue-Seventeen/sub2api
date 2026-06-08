-- Optimize stacked subscription billing card selection.
-- Keep this separate from 149 because applied migrations are immutable.

CREATE INDEX IF NOT EXISTS idx_user_subscriptions_billing_stack_active
	ON user_subscriptions(user_id, group_id, status, starts_at, id)
	WHERE deleted_at IS NULL;
