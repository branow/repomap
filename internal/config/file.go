package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"

	"gopkg.in/yaml.v3"
)

// Name is the file every repomap installation reads.
const Name = "config.yml"

// dir is the per-user configuration directory: XDG_CONFIG_HOME, then %AppData%
// on Windows, then ~/.config.
func dir() (string, error) {
	if home := os.Getenv("XDG_CONFIG_HOME"); home != "" {
		return filepath.Join(home, "repomap"), nil
	}
	if runtime.GOOS == "windows" {
		if appData := os.Getenv("AppData"); appData != "" {
			return filepath.Join(appData, "repomap"), nil
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "repomap"), nil
}

// Path reports where config.yml lives; REPOMAP_CONFIG overrides it outright.
func Path() (string, error) {
	if p := os.Getenv("REPOMAP_CONFIG"); p != "" {
		return p, nil
	}
	d, err := dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, Name), nil
}

// Load reads the user's config; a missing file is an empty config, not an
// error.
func Load() (*Config, error) {
	p, err := Path()
	if err != nil {
		return nil, err
	}
	return LoadFrom(p)
}

// LoadFrom reads and validates a config from an explicit path.
func LoadFrom(path string) (*Config, error) {
	c := NewAt(path)
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return c, nil
		}
		return nil, err
	}
	if err := yaml.Unmarshal(raw, c); err != nil {
		return nil, &InvalidError{Field: "config file", Value: path, Reason: err.Error()}
	}
	c.normalise()
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return c, nil
}

// Save writes the file 0600, creating the directory on demand.
func (c *Config) Save() error {
	if c.path == "" {
		return errors.New("config has no path")
	}
	raw, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(c.path), 0o700); err != nil {
		return err
	}
	// A unique name, not path+".tmp": two repomap processes writing at once would
	// otherwise share one scratch file and rename each other's half of it.
	tmp, err := os.CreateTemp(filepath.Dir(c.path), ".config-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)

	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(name, 0o600); err != nil {
		return err
	}
	if err := os.Rename(name, c.path); err != nil {
		return fmt.Errorf("replacing %s: %w", filepath.Base(c.path), err)
	}
	return nil
}

// normalise restores what unmarshalling loses: non-nil maps and an env lookup.
func (c *Config) normalise() {
	if c.Workspaces == nil {
		c.Workspaces = map[string]Workspace{}
	}
	if c.Backends == nil {
		c.Backends = map[string]Backend{}
	}
	if c.Profiles == nil {
		c.Profiles = map[string]Profile{}
	}
	if c.env == nil {
		c.env = os.Getenv
	}
}
