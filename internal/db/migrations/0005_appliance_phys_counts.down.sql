-- 0005_appliance_phys_counts.down.sql
ALTER TABLE appliances
  DROP COLUMN IF EXISTS phys_total_count,
  DROP COLUMN IF EXISTS phys_up_count,
  DROP COLUMN IF EXISTS uplink_count;
