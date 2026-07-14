ALTER TABLE notification_channels DROP CONSTRAINT IF EXISTS notification_channels_kind_check;
ALTER TABLE notification_channels
  ADD CONSTRAINT notification_channels_kind_check
  CHECK (kind IN ('email','webhook'));
