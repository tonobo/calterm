package config

import (
	"os"
	"path/filepath"
)

// DefaultConfigPath returns $XDG_CONFIG_HOME/calterm/config.toml.
func DefaultConfigPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "calterm", "config.toml"), nil
}

// CacheDir returns $XDG_CACHE_HOME/calterm, creating it if needed.
func CacheDir() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, "calterm")
	if err := os.MkdirAll(path, 0o700); err != nil {
		return "", err
	}
	return path, nil
}
