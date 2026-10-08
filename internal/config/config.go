// Package config resolves configuration without creating directories or accessing a library.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
)

const maxConfigBytes = 64 << 10

type Config struct {
	Library string `json:"library,omitempty"`
	Review  string `json:"review,omitempty"`
}

type Paths struct {
	Config string `json:"config"`
	State  string `json:"state"`
	Cache  string `json:"cache"`
}

// PlatformPaths is explicit about platform and environment so path policy is testable.
func PlatformPaths(platform, home string, env func(string) string) (Paths, error) {
	var p Paths
	switch platform {
	case "linux":
		p = Paths{xdg(env("XDG_CONFIG_HOME"), filepath.Join(home, ".config")), xdg(env("XDG_STATE_HOME"), filepath.Join(home, ".local", "state")), xdg(env("XDG_CACHE_HOME"), filepath.Join(home, ".cache"))}
	case "darwin":
		p = Paths{filepath.Join(home, "Library", "Application Support"), filepath.Join(home, "Library", "Application Support"), filepath.Join(home, "Library", "Caches")}
	case "windows":
		p = Paths{env("APPDATA"), env("LOCALAPPDATA"), env("LOCALAPPDATA")}
	default:
		return p, fmt.Errorf("unsupported platform %q", platform)
	}
	for _, path := range []string{home, p.Config, p.State, p.Cache} {
		if !filepath.IsAbs(path) {
			return Paths{}, errors.New("home and platform directories must be absolute")
		}
	}
	return Paths{filepath.Join(p.Config, "nemalo", "config.json"), filepath.Join(p.State, "nemalo", "state"), filepath.Join(p.Cache, "nemalo")}, nil
}

func xdg(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func DefaultPaths() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, err
	}
	return PlatformPaths(runtime.GOOS, home, os.Getenv)
}

// Load applies file < environment < flags. An explicitly named missing file is an error.
func Load(path string, explicit bool, env func(string) string, overrides Config) (Config, error) {
	var c Config
	f, err := os.Open(path)
	if err == nil {
		defer f.Close()
		info, statErr := f.Stat()
		if statErr != nil {
			return c, statErr
		}
		if !info.Mode().IsRegular() || info.Size() > maxConfigBytes {
			return c, errors.New("configuration must be a regular file no larger than 64 KiB")
		}
		dec := json.NewDecoder(io.LimitReader(f, maxConfigBytes+1))
		dec.DisallowUnknownFields()
		var object *Config
		if err := dec.Decode(&object); err != nil {
			return c, fmt.Errorf("decode configuration: %w", err)
		}
		if object == nil {
			return c, errors.New("configuration must be a JSON object")
		}
		c = *object
		if err := dec.Decode(new(any)); err != io.EOF {
			return c, errors.New("configuration must contain exactly one JSON object")
		}
	} else if explicit || !errors.Is(err, os.ErrNotExist) {
		return c, fmt.Errorf("open configuration: %w", err)
	}
	if v := env("NEMALO_LIBRARY"); v != "" {
		c.Library = v
	}
	if v := env("NEMALO_REVIEW"); v != "" {
		c.Review = v
	}
	if overrides.Library != "" {
		c.Library = overrides.Library
	}
	if overrides.Review != "" {
		c.Review = overrides.Review
	}
	for _, path := range []string{c.Library, c.Review} {
		if path != "" && !filepath.IsAbs(path) {
			return c, errors.New("library and review paths must be absolute")
		}
	}
	if c.Library != "" && c.Review != "" && (within(c.Library, c.Review) || within(c.Review, c.Library)) {
		return c, errors.New("library and review directories must not overlap")
	}
	return c, nil
}

func within(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	return err == nil && filepath.IsLocal(rel)
}
