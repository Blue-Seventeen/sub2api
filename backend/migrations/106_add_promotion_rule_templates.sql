-- Add editable rule templates for promotion config

ALTER TABLE promotion_settings
    ADD COLUMN IF NOT EXISTS rule_activation_template TEXT;

ALTER TABLE promotion_settings
    ADD COLUMN IF NOT EXISTS rule_direct_template TEXT;

ALTER TABLE promotion_settings
    ADD COLUMN IF NOT EXISTS rule_indirect_template TEXT;

ALTER TABLE promotion_settings
    ADD COLUMN IF NOT EXISTS rule_level_summary_template TEXT;

UPDATE promotion_settings
SET rule_activation_template = COALESCE(NULLIF(rule_activation_template, ''), '激活奖励（每邀请 1 人激活，你可获得 ${{ACTIVATION_BONUS}} 激活奖励（激活条件：被邀请人消耗 > {{ACTIVATION_THRESHOLD}}$））'),
    rule_direct_template = COALESCE(NULLIF(rule_direct_template, ''), '一级返利（你邀请的人每次消费，你可获得其消费金额对应百分比的返利（次日 {{SETTLEMENT_TIME}} 结算，随等级提升而提升，当前等级：{{CURRENT_DIRECT_RATE}}%））'),
    rule_indirect_template = COALESCE(NULLIF(rule_indirect_template, ''), '二级返利（你邀请的人邀请的二级代理，你可以获得二级代理消费的相应金额的百分比返利（次日 {{SETTLEMENT_TIME}} 结算，随等级提升而提升，当前等级：{{CURRENT_INDIRECT_RATE}}%））'),
    rule_level_summary_template = COALESCE(NULLIF(rule_level_summary_template, ''), '等级提成比例：{{LEVEL_RATE_SUMMARY}}')
WHERE id = 1;
