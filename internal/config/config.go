// Package config handles loading and providing application configuration.
package config

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config holds the application configuration
type Config struct {
	SearchPaths        []string `yaml:"search_paths"`
	MaxDepth           int      `yaml:"max_depth"`
	ExcludedDirs       []string `yaml:"excluded_dirs"`
	DefaultSessionName string   `yaml:"default_session_name"`
	ContainerTimeout   int      `yaml:"container_timeout_seconds"`
}

// DefaultExcludedDirs returns the default directories to exclude from scanning
func DefaultExcludedDirs() []string {
	return []string{
		"node_modules",
		"vendor",
		".git",
		"__pycache__",
		"venv",
		".venv",
		"dist",
		"build",
		".cache",
	}
}

// DefaultConfig returns the default configuration
func DefaultConfig() *Config {
	homeDir, _ := os.UserHomeDir()
	return &Config{
		SearchPaths:        []string{homeDir},
		MaxDepth:           3,
		ExcludedDirs:       DefaultExcludedDirs(),
		DefaultSessionName: "main",
		ContainerTimeout:   300,
	}
}

// configPath returns the path to the config file
func configPath() string {
	configDir, err := os.UserConfigDir()
	if err != nil {
		homeDir, _ := os.UserHomeDir()
		configDir = filepath.Join(homeDir, ".config")
	}
	return filepath.Join(configDir, "claude-quick", "config.yaml")
}

// Load reads the configuration from the config file
// Falls back to defaults if the file doesn't exist
func Load() (*Config, error) {
	cfg := DefaultConfig()

	path := configPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, err
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	// Expand ~ in paths
	for i, p := range cfg.SearchPaths {
		cfg.SearchPaths[i] = expandPath(p)
	}

	// Ensure reasonable defaults
	if cfg.MaxDepth <= 0 {
		cfg.MaxDepth = 3
	}

	// Use default excluded dirs if none specified
	if len(cfg.ExcludedDirs) == 0 {
		cfg.ExcludedDirs = DefaultExcludedDirs()
	}

	// Ensure default session name
	if cfg.DefaultSessionName == "" {
		cfg.DefaultSessionName = "main"
	}

	// Ensure reasonable timeout (minimum 30 seconds, max 30 minutes)
	if cfg.ContainerTimeout <= 0 {
		cfg.ContainerTimeout = 300
	} else if cfg.ContainerTimeout < 30 {
		cfg.ContainerTimeout = 30
	} else if cfg.ContainerTimeout > 1800 {
		cfg.ContainerTimeout = 1800
	}

	return cfg, nil
}

// expandPath expands ~ to the user's home directory
func expandPath(path string) string {
	if len(path) == 0 {
		return path
	}
	if path[0] == '~' {
		homeDir, _ := os.UserHomeDir()
		return filepath.Join(homeDir, path[1:])
	}
	return path
}

// ConfigPath returns the path where the config file should be located
func ConfigPath() string {
	return configPath()
}
