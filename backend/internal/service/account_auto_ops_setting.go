package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

func (s *SettingService) GetAccountAutoOpsConfig(ctx context.Context) (*AccountAutoOpsConfig, bool, error) {
	if s == nil || s.settingRepo == nil {
		return DefaultAccountAutoOpsConfig(), false, nil
	}

	value, err := s.settingRepo.GetValue(ctx, SettingKeyAccountAutoOpsConfig)
	if err != nil {
		if errors.Is(err, ErrSettingNotFound) {
			return DefaultAccountAutoOpsConfig(), false, nil
		}
		return nil, false, fmt.Errorf("get account auto ops config: %w", err)
	}
	if value == "" {
		return DefaultAccountAutoOpsConfig(), false, nil
	}

	var cfg AccountAutoOpsConfig
	if err := json.Unmarshal([]byte(value), &cfg); err != nil {
		return nil, false, fmt.Errorf("parse account auto ops config: %w", err)
	}
	normalized := NormalizeAccountAutoOpsConfig(&cfg)
	normalized.Configured = true
	return normalized, true, nil
}

func (s *SettingService) SetAccountAutoOpsConfig(ctx context.Context, cfg *AccountAutoOpsConfig) error {
	if s == nil || s.settingRepo == nil {
		return fmt.Errorf("setting service is not ready")
	}
	normalized := NormalizeAccountAutoOpsConfig(cfg)
	normalized.Configured = false
	if err := ValidateAccountAutoOpsConfig(normalized); err != nil {
		return err
	}
	data, err := json.Marshal(normalized)
	if err != nil {
		return fmt.Errorf("marshal account auto ops config: %w", err)
	}
	return s.settingRepo.Set(ctx, SettingKeyAccountAutoOpsConfig, string(data))
}
