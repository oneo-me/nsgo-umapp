//go:build !js

package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/nsgo-dev/nsgo/core"
)

// prefs keeps the config in config.json under the user's config directory, and on macOS the API key in the keychain.
type prefs struct {
	dir string

	saving   sync.Mutex
	savedKey string
}

func openPrefs(*core.Scope) (*prefs, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	return &prefs{dir: filepath.Join(base, "umapp")}, nil
}

func (p *prefs) load() config {
	cfg := loadConfig(p.dir)
	p.savedKey = cfg.Key
	return cfg
}

// save writes cfg off the UI thread, leaving the keychain alone when the key is the one it holds.
func (p *prefs) save(cfg config) {
	go func() {
		p.saving.Lock()
		defer p.saving.Unlock()
		if cfg.Key == p.savedKey && runtime.GOOS == "darwin" {
			cfg.Key = ""
			if err := writeConfig(p.dir, cfg); err != nil {
				log.Print("config: ", err)
			}
			return
		}
		if err := saveConfig(p.dir, cfg); err != nil {
			log.Print("config: ", err)
			return
		}
		p.savedKey = cfg.Key
	}()
}

// browserZone is the browser's time zone, which only a page has.
func browserZone() string { return "" }

// apiRequest is a GET of u, an Umami endpoint's URL, made straight to the server.
func apiRequest(ctx context.Context, u string) (*http.Request, error) {
	return http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
}

const keychainService = "umapp: Umami API key"

func loadConfig(dir string) config {
	cfg := config{Endpoint: cloudEndpoint, Range: 0}
	if b, err := os.ReadFile(filepath.Join(dir, "config.json")); err == nil {
		_ = json.Unmarshal(b, &cfg)
	}
	if runtime.GOOS == "darwin" && cfg.Key == "" {
		out, err := exec.Command("security", "find-generic-password", "-s", keychainService, "-w").Output()
		if err == nil {
			cfg.Key = strings.TrimSpace(string(out))
		}
	}
	return cfg
}

func saveConfig(dir string, cfg config) error {
	if runtime.GOOS == "darwin" {
		if err := keychainStore(cfg.Key); err == nil {
			cfg.Key = ""
		}
	}
	return writeConfig(dir, cfg)
}

// writeConfig writes the file alone.
func writeConfig(dir string, cfg config) error {
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp := filepath.Join(dir, "config.json.tmp")
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, "config.json"))
}

func keychainStore(key string) error {
	if key == "" {
		err := exec.Command("security", "delete-generic-password", "-s", keychainService).Run()
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return nil // there was none
		}
		return err
	}
	return exec.Command("security", "add-generic-password", "-U", "-a", os.Getenv("USER"),
		"-s", keychainService, "-w", key).Run()
}
