package probe

import (
	"encoding/json"
	"fmt"
	"os"
)

// LoadConfig reads and parses the persisted agent config file.
func LoadConfig(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNoConfig
		}
		return nil, fmt.Errorf("probe: read config: %w", err)
	}
	var c Config
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("probe: decode config: %w", err)
	}
	return &c, nil
}

// SaveConfig persists the agent config atomically, mode 0600.
func SaveConfig(path string, c *Config) error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, b, 0o600)
}
