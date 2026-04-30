package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

type backupScheduleTestSettingRepo struct {
	mu   sync.Mutex
	data map[string]string
}

func newBackupScheduleTestSettingRepo() *backupScheduleTestSettingRepo {
	return &backupScheduleTestSettingRepo{data: make(map[string]string)}
}

func (m *backupScheduleTestSettingRepo) Get(_ context.Context, key string) (*Setting, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.data[key]
	if !ok {
		return nil, ErrSettingNotFound
	}
	return &Setting{Key: key, Value: v}, nil
}

func (m *backupScheduleTestSettingRepo) GetValue(_ context.Context, key string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.data[key], nil
}

func (m *backupScheduleTestSettingRepo) Set(_ context.Context, key, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[key] = value
	return nil
}

func (m *backupScheduleTestSettingRepo) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make(map[string]string)
	for _, key := range keys {
		if value, ok := m.data[key]; ok {
			result[key] = value
		}
	}
	return result, nil
}

func (m *backupScheduleTestSettingRepo) SetMultiple(_ context.Context, settings map[string]string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for key, value := range settings {
		m.data[key] = value
	}
	return nil
}

func (m *backupScheduleTestSettingRepo) GetAll(_ context.Context) (map[string]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make(map[string]string, len(m.data))
	for key, value := range m.data {
		result[key] = value
	}
	return result, nil
}

func (m *backupScheduleTestSettingRepo) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, key)
	return nil
}

type backupScheduleTestEncryptor struct{}

func (e *backupScheduleTestEncryptor) Encrypt(plaintext string) (string, error) {
	return "ENC:" + plaintext, nil
}

func (e *backupScheduleTestEncryptor) Decrypt(ciphertext string) (string, error) {
	if strings.HasPrefix(ciphertext, "ENC:") {
		return strings.TrimPrefix(ciphertext, "ENC:"), nil
	}
	return "", fmt.Errorf("not encrypted")
}

type backupScheduleTestDumper struct {
	data []byte
}

func (d *backupScheduleTestDumper) Dump(_ context.Context) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(d.data)), nil
}

func (d *backupScheduleTestDumper) Restore(_ context.Context, data io.Reader) error {
	_, _ = io.ReadAll(data)
	return nil
}

type backupScheduleTestObjectStore struct {
	mu        sync.Mutex
	objects   map[string][]byte
	deleteErr error
}

func newBackupScheduleTestObjectStore() *backupScheduleTestObjectStore {
	return &backupScheduleTestObjectStore{objects: make(map[string][]byte)}
}

func (s *backupScheduleTestObjectStore) Upload(_ context.Context, key string, body io.Reader, _ string) (int64, error) {
	data, err := io.ReadAll(body)
	if err != nil {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.objects[key] = data
	return int64(len(data)), nil
}

func (s *backupScheduleTestObjectStore) Download(_ context.Context, key string) (io.ReadCloser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, ok := s.objects[key]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (s *backupScheduleTestObjectStore) Delete(_ context.Context, key string) error {
	if s.deleteErr != nil {
		return s.deleteErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.objects, key)
	return nil
}

func (s *backupScheduleTestObjectStore) PresignURL(_ context.Context, key string, _ time.Duration) (string, error) {
	return "https://example.test/" + key, nil
}

func (s *backupScheduleTestObjectStore) HeadBucket(_ context.Context) error { return nil }

func newBackupScheduleTestService(repo *backupScheduleTestSettingRepo, dumper DBDumper, objectStore *backupScheduleTestObjectStore, localConfigPath string) *BackupService {
	cfg := &config.Config{Database: config.DatabaseConfig{Host: "localhost", Port: 5432, User: "test", DBName: "sub2api"}}
	factory := func(context.Context, *BackupS3Config) (BackupObjectStore, error) { return objectStore, nil }
	svc := NewBackupService(repo, cfg, &backupScheduleTestEncryptor{}, factory, dumper)
	svc.scheduleLocalConfigPath = localConfigPath
	return svc
}

func seedBackupScheduleTestS3Config(t *testing.T, repo *backupScheduleTestSettingRepo) {
	t.Helper()
	cfg := BackupS3Config{Bucket: "bucket", AccessKeyID: "ak", SecretAccessKey: "ENC:sk", Prefix: "backups"}
	data, err := json.Marshal(cfg)
	require.NoError(t, err)
	require.NoError(t, repo.Set(context.Background(), settingKeyBackupS3Config, string(data)))
}

func TestBackupServiceSchedule_DefaultsLocalEnabledToFalse(t *testing.T) {
	repo := newBackupScheduleTestSettingRepo()
	require.NoError(t, repo.Set(context.Background(), settingKeyBackupSchedule, `{"enabled":true,"cron_expr":"0 2 * * *","retain_days":7,"retain_count":3}`))
	svc := newBackupScheduleTestService(repo, &backupScheduleTestDumper{}, newBackupScheduleTestObjectStore(), filepath.Join(t.TempDir(), backupScheduleLocalConfigFile))

	cfg, err := svc.GetSchedule(context.Background())

	require.NoError(t, err)
	require.False(t, cfg.Enabled)
	require.Equal(t, "0 2 * * *", cfg.CronExpr)
	require.Equal(t, 7, cfg.RetainDays)
	require.Equal(t, 3, cfg.RetainCount)
}

func TestBackupServiceSchedule_UpdatePersistsEnabledLocallyOnly(t *testing.T) {
	repo := newBackupScheduleTestSettingRepo()
	localConfigPath := filepath.Join(t.TempDir(), backupScheduleLocalConfigFile)
	svc := newBackupScheduleTestService(repo, &backupScheduleTestDumper{}, newBackupScheduleTestObjectStore(), localConfigPath)
	svc.Start()
	defer svc.Stop()

	cfg, err := svc.UpdateSchedule(context.Background(), BackupScheduleConfig{
		Enabled:     true,
		CronExpr:    "0 2 * * *",
		RetainDays:  14,
		RetainCount: 10,
	})

	require.NoError(t, err)
	require.True(t, cfg.Enabled)

	raw, err := repo.GetValue(context.Background(), settingKeyBackupSchedule)
	require.NoError(t, err)
	var shared BackupScheduleConfig
	require.NoError(t, json.Unmarshal([]byte(raw), &shared))
	require.False(t, shared.Enabled)
	require.Equal(t, "0 2 * * *", shared.CronExpr)

	var local backupScheduleLocalConfig
	localData, err := os.ReadFile(localConfigPath)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(localData, &local))
	require.True(t, local.Enabled)
}

func TestBackupServiceSchedule_UpdateDoesNotPersistSharedWhenLocalWriteFails(t *testing.T) {
	repo := newBackupScheduleTestSettingRepo()
	blockingPath := filepath.Join(t.TempDir(), "not-a-dir")
	require.NoError(t, os.WriteFile(blockingPath, []byte("x"), 0o600))
	svc := newBackupScheduleTestService(repo, &backupScheduleTestDumper{}, newBackupScheduleTestObjectStore(), filepath.Join(blockingPath, backupScheduleLocalConfigFile))
	svc.cronSched = cron.New()

	_, err := svc.UpdateSchedule(context.Background(), BackupScheduleConfig{
		Enabled:     true,
		CronExpr:    "0 2 * * *",
		RetainDays:  14,
		RetainCount: 10,
	})

	require.Error(t, err)
	raw, getErr := repo.GetValue(context.Background(), settingKeyBackupSchedule)
	require.NoError(t, getErr)
	require.Empty(t, raw)
}

func TestBackupServiceSchedule_ReconcileAppliesSharedCronChangesOnEnabledNode(t *testing.T) {
	repo := newBackupScheduleTestSettingRepo()
	svc := newBackupScheduleTestService(repo, &backupScheduleTestDumper{}, newBackupScheduleTestObjectStore(), filepath.Join(t.TempDir(), backupScheduleLocalConfigFile))
	svc.cronSched = cron.New()
	require.NoError(t, svc.saveLocalScheduleEnabled(true))

	require.NoError(t, repo.Set(context.Background(), settingKeyBackupSchedule, `{"enabled":false,"cron_expr":"0 2 * * *","retain_days":7,"retain_count":3}`))
	require.NoError(t, svc.reconcileCronSchedule(context.Background()))
	require.Equal(t, "0 2 * * *", svc.cronApplyKey)
	require.NotZero(t, svc.cronEntryID)

	require.NoError(t, repo.Set(context.Background(), settingKeyBackupSchedule, `{"enabled":false,"cron_expr":"0 3 * * *","retain_days":7,"retain_count":3}`))
	require.NoError(t, svc.reconcileCronSchedule(context.Background()))
	require.Equal(t, "0 3 * * *", svc.cronApplyKey)
	require.NotZero(t, svc.cronEntryID)

	require.NoError(t, svc.saveLocalScheduleEnabled(false))
	require.NoError(t, svc.reconcileCronSchedule(context.Background()))
	require.Empty(t, svc.cronApplyKey)
	require.Zero(t, svc.cronEntryID)
}

func TestBackupServiceScheduledBackupSkipsWhenLocalScheduleDisabled(t *testing.T) {
	repo := newBackupScheduleTestSettingRepo()
	seedBackupScheduleTestS3Config(t, repo)
	require.NoError(t, repo.Set(context.Background(), settingKeyBackupSchedule, `{"enabled":true,"cron_expr":"0 2 * * *","retain_days":7,"retain_count":3}`))
	svc := newBackupScheduleTestService(repo, &backupScheduleTestDumper{data: []byte("scheduled-data")}, newBackupScheduleTestObjectStore(), filepath.Join(t.TempDir(), backupScheduleLocalConfigFile))

	svc.runScheduledBackup()

	records, err := svc.loadRecords(context.Background())
	require.NoError(t, err)
	require.Empty(t, records)
}

func TestBackupServiceScheduledBackupRunsWhenLocalScheduleEnabled(t *testing.T) {
	repo := newBackupScheduleTestSettingRepo()
	seedBackupScheduleTestS3Config(t, repo)
	require.NoError(t, repo.Set(context.Background(), settingKeyBackupSchedule, `{"enabled":false,"cron_expr":"0 2 * * *","retain_days":7,"retain_count":3}`))
	objectStore := newBackupScheduleTestObjectStore()
	svc := newBackupScheduleTestService(repo, &backupScheduleTestDumper{data: []byte("scheduled-data")}, objectStore, filepath.Join(t.TempDir(), backupScheduleLocalConfigFile))
	require.NoError(t, svc.saveLocalScheduleEnabled(true))

	svc.runScheduledBackup()

	records, err := svc.loadRecords(context.Background())
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Equal(t, "completed", records[0].Status)
	require.Equal(t, "scheduled", records[0].TriggeredBy)
	require.NotZero(t, records[0].SizeBytes)
}

func TestBackupServiceCleanupOldBackupsKeepsRecordWhenS3DeleteFails(t *testing.T) {
	repo := newBackupScheduleTestSettingRepo()
	seedBackupScheduleTestS3Config(t, repo)
	store := newBackupScheduleTestObjectStore()
	store.objects["backups/expired.sql.gz"] = []byte("backup-data")
	store.deleteErr = fmt.Errorf("s3 delete failed")
	svc := newBackupScheduleTestService(repo, &backupScheduleTestDumper{}, store, filepath.Join(t.TempDir(), backupScheduleLocalConfigFile))

	expired := &BackupRecord{
		ID:          "expired-1",
		Status:      "completed",
		BackupType:  "postgres",
		FileName:    "expired.sql.gz",
		S3Key:       "backups/expired.sql.gz",
		StartedAt:   time.Now().Add(-48 * time.Hour).Format(time.RFC3339),
		FinishedAt:  time.Now().Add(-47 * time.Hour).Format(time.RFC3339),
		TriggeredBy: "scheduled",
	}
	require.NoError(t, svc.saveRecord(context.Background(), expired))

	err := svc.cleanupOldBackups(context.Background(), &BackupScheduleConfig{RetainDays: 1})

	require.Error(t, err)
	record, getErr := svc.GetBackupRecord(context.Background(), expired.ID)
	require.NoError(t, getErr)
	require.Equal(t, expired.S3Key, record.S3Key)
	store.mu.Lock()
	_, stillExists := store.objects[expired.S3Key]
	store.mu.Unlock()
	require.True(t, stillExists)
}
