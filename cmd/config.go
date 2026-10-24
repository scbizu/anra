package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var agentID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

type fileConfig struct {
	DefaultAgent string                 `toml:"default-agent"`
	Defaults     agentConfig            `toml:"defaults"`
	Agents       map[string]agentConfig `toml:"agents"`
}

// Pointers distinguish an omitted setting from an explicitly empty value.
// Keys mirror the command flags; credentials continue to come from the environment.
type agentConfig struct {
	DataDir  *string `toml:"data-dir"`
	Session  *string `toml:"session"`
	Provider *string `toml:"provider"`
	APIBase  *string `toml:"api-base"`
	Model    *string `toml:"model"`
	System   *string `toml:"system"`
	Listen   *string `toml:"listen"`
	Timeout  *string `toml:"timeout"`
	Registry *string `toml:"registry"`
}

func (c agentConfig) settings() map[string]*string {
	return map[string]*string{
		"data-dir": c.DataDir, "session": c.Session, "provider": c.Provider, "api-base": c.APIBase,
		"model": c.Model, "system": c.System, "listen": c.Listen,
		"timeout":  c.Timeout,
		"registry": c.Registry,
	}
}

// applyConfig binds flags and environment, then loads the selected TOML profile
// into Viper. Commands and agent construction read the resolved settings directly.
func applyConfig(command *cobra.Command, settings *viper.Viper) error {
	if err := settings.BindPFlags(command.Flags()); err != nil {
		return err
	}
	names := []string{"config", "agent", "registry", "data-dir", "session", "provider", "api-base", "model", "system", "listen", "to", "timeout", "lease-ttl"}
	for _, name := range names {
		if command.Flag(name) != nil {
			settings.MustBindEnv(name)
		}
	}
	if path := settings.GetString("config"); path != "" {
		if err := loadAgentConfig(settings, path); err != nil {
			return err
		}
	}
	if !agentID.MatchString(settings.GetString("agent")) {
		return errors.New("--agent must start with a letter or digit and contain only letters, digits, '.', '_' or '-'")
	}
	if strings.TrimSpace(settings.GetString("data-dir")) == "" || strings.TrimSpace(settings.GetString("session")) == "" {
		return errors.New("--data-dir and --session must not be empty")
	}
	for _, name := range []string{"timeout", "lease-ttl"} {
		if command.Flag(name) == nil {
			continue
		}
		duration, err := time.ParseDuration(settings.GetString(name))
		if err != nil {
			return fmt.Errorf("setting %s: %w", name, err)
		}
		if name == "timeout" && duration <= 0 {
			return errors.New("--timeout must be positive")
		}
	}

	return nil
}

func loadAgentConfig(settings *viper.Viper, path string) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("read config %q: %w", path, err)
	}
	defer file.Close()
	var config fileConfig
	if err := toml.NewDecoder(file).DisallowUnknownFields().Decode(&config); err != nil {
		return fmt.Errorf("parse config %q: %w", path, err)
	}
	if len(config.Agents) == 0 {
		return fmt.Errorf("config %q must define at least one agent", path)
	}
	for id := range config.Agents {
		if !agentID.MatchString(id) {
			return fmt.Errorf("config %q has invalid agent name %q", path, id)
		}
	}
	if config.DefaultAgent != "" {
		if _, ok := config.Agents[config.DefaultAgent]; !ok {
			return fmt.Errorf("config %q: default-agent %q is not configured", path, config.DefaultAgent)
		}
		settings.SetDefault("agent", config.DefaultAgent)
	}
	id := settings.GetString("agent")
	selected, ok := config.Agents[id]
	if !ok {
		return fmt.Errorf("agent %q is not configured in %q", id, path)
	}
	merged := config.Defaults.settings()
	for name, value := range selected.settings() {
		if value != nil {
			merged[name] = value
		}
	}
	values := make(map[string]any)
	for name, value := range merged {
		if value == nil {
			continue
		}
		text := *value
		if name == "data-dir" && text != "" && !filepath.IsAbs(text) {
			text = filepath.Join(filepath.Dir(path), text)
		}
		values[name] = text
	}
	return settings.MergeConfigMap(values)
}
