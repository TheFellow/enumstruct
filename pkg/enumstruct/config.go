package enumstruct

import (
	"os"
	"path/filepath"

	"github.com/goccy/go-yaml"
)

// Config represents the .enumstruct.yml configuration file.
type Config struct {
	// Types lists fully-qualified type names for enum structs declared in
	// external or generated packages where //enumstruct:decl cannot be added.
	// Format: "import/path.TypeName"
	Types []string `yaml:"types"`

	// DefaultMode controls whether a default: clause satisfies exhaustiveness.
	// "strict"  (default) — default: does NOT exempt missing cases.
	// "lenient" — default: exempts the switch from exhaustiveness checks.
	DefaultMode string `yaml:"default_mode"`

	// CheckGenerated controls whether generated files are linted.
	// Default: true.
	CheckGenerated *bool `yaml:"check_generated"`

	// ExcludeFields lists field names to globally exclude from exhaustiveness.
	// Keyed by fully-qualified type name. Example:
	//   exclude_fields:
	//     "pkg/model.Foo": ["DeprecatedField"]
	ExcludeFields map[string][]string `yaml:"exclude_fields"`
}

func defaultConfig() Config {
	cfg := Config{}
	applyDefaults(&cfg)
	return cfg
}

func applyDefaults(cfg *Config) {
	if cfg.DefaultMode == "" {
		cfg.DefaultMode = "strict"
	}
	if cfg.CheckGenerated == nil {
		t := true
		cfg.CheckGenerated = &t
	}
}

// loadConfig reads .enumstruct.yml from the given directory, or returns a
// zero-value Config if no file exists.
func loadConfig(dir string) (Config, error) {
	configPath, found, err := resolveConfigPath(dir)
	if err != nil {
		return Config{}, err
	}
	if !found {
		return defaultConfig(), nil
	}
	return loadConfigFromPath(configPath)
}

func loadConfigFromPath(configPath string) (Config, error) {
	cfg := Config{}
	data, err := os.ReadFile(configPath)
	if err != nil {
		return Config{}, err
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	applyDefaults(&cfg)
	return cfg, nil
}

func resolveConfigPath(dir string) (string, bool, error) {
	current := dir
	for {
		configPath := filepath.Join(current, ".enumstruct.yml")
		_, err := os.Stat(configPath)
		if err == nil {
			return configPath, true, nil
		}
		if !os.IsNotExist(err) {
			return "", false, err
		}

		parent := filepath.Dir(current)
		if parent == current {
			return "", false, nil
		}
		current = parent
	}
}
