package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type displayCurrencySymbolRepoStub struct {
	values         map[string]string
	updates        map[string]string
	setMultipleErr error
}

func (s *displayCurrencySymbolRepoStub) Get(ctx context.Context, key string) (*Setting, error) {
	panic("unexpected Get call")
}

func (s *displayCurrencySymbolRepoStub) GetValue(ctx context.Context, key string) (string, error) {
	panic("unexpected GetValue call")
}

func (s *displayCurrencySymbolRepoStub) Set(ctx context.Context, key, value string) error {
	panic("unexpected Set call")
}

func (s *displayCurrencySymbolRepoStub) GetMultiple(ctx context.Context, keys []string) (map[string]string, error) {
	out := make(map[string]string, len(keys))
	for _, key := range keys {
		if value, ok := s.values[key]; ok {
			out[key] = value
		}
	}
	return out, nil
}

func (s *displayCurrencySymbolRepoStub) SetMultiple(ctx context.Context, settings map[string]string) error {
	if s.setMultipleErr != nil {
		return s.setMultipleErr
	}
	s.updates = make(map[string]string, len(settings))
	for key, value := range settings {
		s.updates[key] = value
	}
	return nil
}

func (s *displayCurrencySymbolRepoStub) GetAll(ctx context.Context) (map[string]string, error) {
	out := make(map[string]string, len(s.values))
	for key, value := range s.values {
		out[key] = value
	}
	return out, nil
}

func (s *displayCurrencySymbolRepoStub) Delete(ctx context.Context, key string) error {
	panic("unexpected Delete call")
}

func newDisplayCurrencySymbolTestService(t *testing.T, repo *displayCurrencySymbolRepoStub) (*SettingService, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), displayCurrencySymbolLocalConfigFile)
	svc := NewSettingService(repo, &config.Config{})
	svc.displayCurrencySymbolLocalConfigPath = path
	return svc, path
}

func readDisplayCurrencySymbolLocalConfig(t *testing.T, path string) displayCurrencySymbolLocalConfig {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)

	var cfg displayCurrencySymbolLocalConfig
	require.NoError(t, json.Unmarshal(data, &cfg))
	return cfg
}

func TestSettingService_DisplayCurrencySymbolBootstrapsLocalConfigFromSharedSetting(t *testing.T) {
	repo := &displayCurrencySymbolRepoStub{values: map[string]string{
		SettingKeyDisplayCurrencySymbol: " ¥ ",
	}}
	svc, path := newDisplayCurrencySymbolTestService(t, repo)

	settings, err := svc.GetPublicSettings(context.Background())
	require.NoError(t, err)
	require.Equal(t, "¥", settings.DisplayCurrencySymbol)

	cfg := readDisplayCurrencySymbolLocalConfig(t, path)
	require.True(t, cfg.LocalOnly)
	require.Equal(t, "¥", cfg.Symbol)
}

func TestSettingService_UpdateSettings_DisplayCurrencySymbolLocalOnlyDoesNotUpdateSharedSetting(t *testing.T) {
	repo := &displayCurrencySymbolRepoStub{values: map[string]string{}}
	svc, path := newDisplayCurrencySymbolTestService(t, repo)

	err := svc.UpdateSettings(context.Background(), &SystemSettings{
		DisplayCurrencySymbol:          " RMB ",
		DisplayCurrencySymbolLocalOnly: true,
	})
	require.NoError(t, err)
	require.NotContains(t, repo.updates, SettingKeyDisplayCurrencySymbol)

	cfg := readDisplayCurrencySymbolLocalConfig(t, path)
	require.True(t, cfg.LocalOnly)
	require.Equal(t, "RMB", cfg.Symbol)
}

func TestSettingService_UpdateSettings_DisplayCurrencySymbolSharedUpdatesDatabaseAndLocalMode(t *testing.T) {
	repo := &displayCurrencySymbolRepoStub{values: map[string]string{}}
	svc, path := newDisplayCurrencySymbolTestService(t, repo)

	err := svc.UpdateSettings(context.Background(), &SystemSettings{
		DisplayCurrencySymbol:          "¥",
		DisplayCurrencySymbolLocalOnly: false,
	})
	require.NoError(t, err)
	require.Equal(t, "¥", repo.updates[SettingKeyDisplayCurrencySymbol])

	cfg := readDisplayCurrencySymbolLocalConfig(t, path)
	require.False(t, cfg.LocalOnly)
	require.Equal(t, "¥", cfg.Symbol)
}

func TestSettingService_UpdateSettings_RestoresDisplayCurrencyLocalConfigWhenDatabaseWriteFails(t *testing.T) {
	repo := &displayCurrencySymbolRepoStub{
		values:         map[string]string{SettingKeyDisplayCurrencySymbol: "$"},
		setMultipleErr: errors.New("database unavailable"),
	}
	svc, path := newDisplayCurrencySymbolTestService(t, repo)
	require.NoError(t, svc.saveDisplayCurrencySymbolLocalConfig(true, "RMB"))

	err := svc.UpdateSettings(context.Background(), &SystemSettings{
		DisplayCurrencySymbol:          "¥",
		DisplayCurrencySymbolLocalOnly: false,
	})
	require.Error(t, err)

	cfg := readDisplayCurrencySymbolLocalConfig(t, path)
	require.True(t, cfg.LocalOnly)
	require.Equal(t, "RMB", cfg.Symbol)
}

func TestSettingService_DisplayCurrencySymbolInvalidLocalConfigFallsBackSafely(t *testing.T) {
	repo := &displayCurrencySymbolRepoStub{values: map[string]string{
		SettingKeyDisplayCurrencySymbol: "¥",
	}}
	svc, path := newDisplayCurrencySymbolTestService(t, repo)
	require.NoError(t, os.WriteFile(path, []byte(`{"local_only":true,"symbol":"bad\nsymbol"}`), 0o600))

	settings, err := svc.GetPublicSettings(context.Background())
	require.NoError(t, err)
	require.Equal(t, "$", settings.DisplayCurrencySymbol)
}
