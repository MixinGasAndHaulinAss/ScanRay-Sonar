DROP TABLE IF EXISTS health_check_samples;
DROP TABLE IF EXISTS device_config_backups;
DROP TABLE IF EXISTS discovered_devices;
DROP TRIGGER IF EXISTS trg_site_discovery_settings_updated_at ON site_discovery_settings;
DROP TABLE IF EXISTS site_discovery_settings;
DROP TABLE IF EXISTS site_credentials;
