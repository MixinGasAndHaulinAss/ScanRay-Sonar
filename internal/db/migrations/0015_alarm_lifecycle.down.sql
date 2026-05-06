ALTER TABLE alarms
  DROP COLUMN IF EXISTS acked_at,
  DROP COLUMN IF EXISTS acked_by,
  DROP COLUMN IF EXISTS cleared_by,
  DROP COLUMN IF EXISTS auto_cleared;

ALTER TABLE alarm_rules
  DROP COLUMN IF EXISTS for_seconds,
  DROP COLUMN IF EXISTS clear_for_seconds;
