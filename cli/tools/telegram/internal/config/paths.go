package config

import (
	"errors"
	"path/filepath"
	"strings"
)

// Env looks up an environment variable. It is injected so tests never read the real environment.
type Env func(name string) (string, bool)

// Platform bases follow contract §13.1. os.UserConfigDir is not used: on macOS it ignores
// XDG_CONFIG_HOME, which the contract says to honour when set.

func homeDir(env Env, goos string) (string, error) {
	name := "HOME"
	if goos == "windows" {
		name = "USERPROFILE"
	}
	home, _ := env(name)
	if home == "" || !filepath.IsAbs(home) {
		return "", errors.New("the home directory cannot be determined (" + name + " is unset or not absolute)")
	}
	return home, nil
}

func xdg(env Env, name string) string {
	value, _ := env(name)
	if value != "" && filepath.IsAbs(value) {
		return value
	}
	return ""
}

// ConfigBase returns the platform's user configuration directory.
func ConfigBase(env Env, goos string) (string, error) {
	switch goos {
	case "windows":
		if value := xdg(env, "APPDATA"); value != "" {
			return value, nil
		}
		return "", errors.New("the configuration directory cannot be determined (APPDATA is unset)")
	case "darwin":
		if value := xdg(env, "XDG_CONFIG_HOME"); value != "" {
			return value, nil
		}
		home, err := homeDir(env, goos)
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "Library", "Application Support"), nil
	default:
		if value := xdg(env, "XDG_CONFIG_HOME"); value != "" {
			return value, nil
		}
		home, err := homeDir(env, goos)
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".config"), nil
	}
}

// CacheBase returns the platform's user cache directory.
func CacheBase(env Env, goos string) (string, error) {
	switch goos {
	case "windows":
		if value := xdg(env, "LOCALAPPDATA"); value != "" {
			return value, nil
		}
		return "", errors.New("the cache directory cannot be determined (LOCALAPPDATA is unset)")
	case "darwin":
		if value := xdg(env, "XDG_CACHE_HOME"); value != "" {
			return value, nil
		}
		home, err := homeDir(env, goos)
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "Library", "Caches"), nil
	default:
		if value := xdg(env, "XDG_CACHE_HOME"); value != "" {
			return value, nil
		}
		home, err := homeDir(env, goos)
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".cache"), nil
	}
}

// ExpandHome replaces a leading "~" with the home directory, on every platform.
func ExpandHome(path string, env Env, goos string) (string, error) {
	if path != "~" && !strings.HasPrefix(path, "~/") && !strings.HasPrefix(path, `~\`) {
		return path, nil
	}
	home, err := homeDir(env, goos)
	if err != nil {
		return "", err
	}
	return filepath.Join(home, path[1:]), nil
}
