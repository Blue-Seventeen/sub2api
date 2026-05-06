package service

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/google/uuid"
)

const displayCurrencySymbolLocalConfigFile = "display_currency_symbol.local.json"

var displayCurrencySymbolLocalConfigMu sync.RWMutex

type displayCurrencySymbolLocalConfig struct {
	LocalOnly bool   `json:"local_only"`
	Symbol    string `json:"symbol"`
}

func defaultDisplayCurrencySymbolLocalConfigPath() string {
	if dataDir := strings.TrimSpace(os.Getenv("DATA_DIR")); dataDir != "" {
		return filepath.Join(dataDir, displayCurrencySymbolLocalConfigFile)
	}
	if info, err := os.Stat("/app/data"); err == nil && info.IsDir() {
		return filepath.Join("/app/data", displayCurrencySymbolLocalConfigFile)
	}
	return filepath.Join(".", displayCurrencySymbolLocalConfigFile)
}

func (s *SettingService) getDisplayCurrencySymbolLocalConfigPath() string {
	path := strings.TrimSpace(s.displayCurrencySymbolLocalConfigPath)
	if path == "" {
		path = defaultDisplayCurrencySymbolLocalConfigPath()
		s.displayCurrencySymbolLocalConfigPath = path
	}
	return path
}

func (s *SettingService) effectiveDisplayCurrencySymbol(sharedSymbol string, sharedConfigured bool) (string, bool) {
	localCfg := s.loadDisplayCurrencySymbolLocalConfig(sharedSymbol, sharedConfigured)
	if localCfg.LocalOnly {
		return safeDisplayCurrencySymbol(localCfg.Symbol), true
	}
	return safeDisplayCurrencySymbol(sharedSymbol), false
}

func (s *SettingService) loadDisplayCurrencySymbolLocalConfig(sharedSymbol string, sharedConfigured bool) displayCurrencySymbolLocalConfig {
	path := s.getDisplayCurrencySymbolLocalConfigPath()
	displayCurrencySymbolLocalConfigMu.RLock()
	data, err := os.ReadFile(path)
	displayCurrencySymbolLocalConfigMu.RUnlock()
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Warn("failed to read display currency symbol local config", "path", path, "error", err)
			return displayCurrencySymbolLocalConfig{
				LocalOnly: true,
				Symbol:    defaultDisplayCurrencySymbol,
			}
		}

		symbol := defaultDisplayCurrencySymbol
		if sharedConfigured {
			symbol = safeDisplayCurrencySymbol(sharedSymbol)
			if err := s.saveDisplayCurrencySymbolLocalConfig(true, symbol); err != nil {
				slog.Warn("failed to bootstrap display currency symbol local config", "path", path, "error", err)
			}
		}
		return displayCurrencySymbolLocalConfig{
			LocalOnly: true,
			Symbol:    symbol,
		}
	}

	var cfg displayCurrencySymbolLocalConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		slog.Warn("failed to parse display currency symbol local config", "path", path, "error", err)
		return displayCurrencySymbolLocalConfig{
			LocalOnly: true,
			Symbol:    defaultDisplayCurrencySymbol,
		}
	}
	cfg.Symbol = safeDisplayCurrencySymbol(cfg.Symbol)
	return cfg
}

func (s *SettingService) prepareDisplayCurrencySymbolLocalConfig(localOnly bool, symbol string) (func() error, func(), error) {
	path := s.getDisplayCurrencySymbolLocalConfigPath()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, nil, fmt.Errorf("create display currency symbol local config dir: %w", err)
	}

	normalizedSymbol, err := normalizeDisplayCurrencySymbol(symbol)
	if err != nil {
		return nil, nil, err
	}
	data, err := json.MarshalIndent(displayCurrencySymbolLocalConfig{
		LocalOnly: localOnly,
		Symbol:    normalizedSymbol,
	}, "", "  ")
	if err != nil {
		return nil, nil, fmt.Errorf("marshal display currency symbol local config: %w", err)
	}
	data = append(data, '\n')

	tmpFile, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-")
	if err != nil {
		return nil, nil, fmt.Errorf("create display currency symbol local config temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	cleanup := func() {
		_ = os.Remove(tmpPath)
	}
	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		cleanup()
		return nil, nil, fmt.Errorf("write display currency symbol local config temp file: %w", err)
	}
	if err := chmodLocalConfigTempFile(tmpFile, 0o600, "display currency symbol local config"); err != nil {
		_ = tmpFile.Close()
		cleanup()
		return nil, nil, err
	}
	if err := tmpFile.Close(); err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("close display currency symbol local config temp file: %w", err)
	}

	commit := func() error {
		displayCurrencySymbolLocalConfigMu.Lock()
		defer displayCurrencySymbolLocalConfigMu.Unlock()

		if err := os.Rename(tmpPath, path); err != nil {
			if _, statErr := os.Stat(path); statErr != nil {
				return fmt.Errorf("save display currency symbol local config: %w", err)
			}
			backupPath := path + ".bak-" + uuid.NewString()
			if backupErr := os.Rename(path, backupPath); backupErr != nil {
				return fmt.Errorf("backup existing display currency symbol local config: %w", backupErr)
			}
			committed := false
			defer func() {
				if committed {
					_ = os.Remove(backupPath)
					return
				}
				_ = os.Rename(backupPath, path)
			}()
			if retryErr := os.Rename(tmpPath, path); retryErr != nil {
				return fmt.Errorf("save display currency symbol local config: %w", retryErr)
			}
			committed = true
		}
		return nil
	}
	return commit, cleanup, nil
}

func (s *SettingService) saveDisplayCurrencySymbolLocalConfig(localOnly bool, symbol string) error {
	commit, cleanup, err := s.prepareDisplayCurrencySymbolLocalConfig(localOnly, symbol)
	if err != nil {
		return err
	}
	defer cleanup()
	return commit()
}

func (s *SettingService) snapshotDisplayCurrencySymbolLocalConfig() func() {
	path := s.getDisplayCurrencySymbolLocalConfigPath()
	displayCurrencySymbolLocalConfigMu.RLock()
	data, err := os.ReadFile(path)
	displayCurrencySymbolLocalConfigMu.RUnlock()
	if err != nil {
		if os.IsNotExist(err) {
			return func() {
				displayCurrencySymbolLocalConfigMu.Lock()
				defer displayCurrencySymbolLocalConfigMu.Unlock()
				_ = os.Remove(path)
			}
		}
		slog.Warn("failed to snapshot display currency symbol local config", "path", path, "error", err)
		return func() {}
	}
	snapshot := append([]byte(nil), data...)
	return func() {
		displayCurrencySymbolLocalConfigMu.Lock()
		defer displayCurrencySymbolLocalConfigMu.Unlock()
		if err := os.WriteFile(path, snapshot, 0o600); err != nil {
			slog.Warn("failed to restore display currency symbol local config", "path", path, "error", err)
		}
	}
}
