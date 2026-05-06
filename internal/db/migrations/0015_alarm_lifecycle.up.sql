-- =============================================================================
-- 0015_alarm_lifecycle.up.sql — alarm ack + auto-clear + hysteresis window.
-- =============================================================================

-- Per-rule predicate-must-hold-for window (seconds). Alarms only open once a
-- rule has been continuously truthy for this long; mirrors the dampening
-- pattern in monitoring tools like Prometheus' `for: 5m`.
ALTER TABLE alarm_rules
  ADD COLUMN IF NOT EXISTS for_seconds INTEGER NOT NULL DEFAULT 0
    CHECK (for_seconds >= 0);

ALTER TABLE alarm_rules
  ADD COLUMN IF NOT EXISTS clear_for_seconds INTEGER NOT NULL DEFAULT 0
    CHECK (clear_for_seconds >= 0);

-- Per-alarm ack metadata so an operator can mark it seen without closing it.
ALTER TABLE alarms
  ADD COLUMN IF NOT EXISTS acked_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS acked_by UUID,
  ADD COLUMN IF NOT EXISTS cleared_by UUID,
  ADD COLUMN IF NOT EXISTS auto_cleared BOOLEAN NOT NULL DEFAULT FALSE;
