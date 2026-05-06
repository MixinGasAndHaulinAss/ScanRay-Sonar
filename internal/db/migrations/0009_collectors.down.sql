DROP INDEX IF EXISTS agents_collector_idx;
ALTER TABLE agents DROP COLUMN IF EXISTS collector_id;

DROP INDEX IF EXISTS appliances_collector_idx;
ALTER TABLE appliances DROP COLUMN IF EXISTS collector_id;

DROP TRIGGER IF EXISTS trg_collectors_updated_at ON collectors;
DROP TABLE IF EXISTS collectors;

DROP TABLE IF EXISTS collector_enrollment_tokens;
