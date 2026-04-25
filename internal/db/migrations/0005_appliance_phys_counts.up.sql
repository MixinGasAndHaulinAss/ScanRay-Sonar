-- =============================================================================
-- 0005_appliance_phys_counts.up.sql
-- Add denormalized counts that distinguish *physical* ports from the
-- noisy total ifTable count (which on a Cisco access switch can balloon
-- to 100+ entries because of SVIs, port-channels, loopbacks, AppGigE,
-- BDIs, etc.).
--
-- We also track an uplink count so the appliance list can show "24-port
-- access switch with 2 uplinks" at a glance.
-- =============================================================================

ALTER TABLE appliances
  ADD COLUMN phys_total_count INTEGER,
  ADD COLUMN phys_up_count    INTEGER,
  ADD COLUMN uplink_count     INTEGER;
