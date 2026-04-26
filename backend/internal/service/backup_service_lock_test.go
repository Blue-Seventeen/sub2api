package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

type backupLockTestSettingRepo struct {
	mu   sync.Mutex
	data map[string]string
}

func newBackupLockTestSettingRepo() *backupLockTestSettingRepo {
	return &backupLockTestSettingRepo{data: make(map[string]string)}
}

func (m *backupLockTestSettingRepo) Get(_ context.Context, key string) (*Setting, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.data[key]
	if !ok {
		return nil, ErrSettingNotFound
	}
	return &Setting{Key: key, Value: v}, nil
}

func (m *backupLockTestSettingRepo) GetValue(_ context.Context, key string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.data[key], nil
}

func (m *backupLockTestSettingRepo) Set(_ context.Context, key, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[key] = value
	return nil
}

func (m *backupLockTestSettingRepo) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
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

func (m *backupLockTestSettingRepo) SetMultiple(_ context.Context, settings map[string]string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for key, value := range settings {
		m.data[key] = value
	}
	return nil
}

func (m *backupLockTestSettingRepo) GetAll(_ context.Context) (map[string]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make(map[string]string, len(m.data))
	for key, value := range m.data {
		result[key] = value
	}
	return result, nil
}

func (m *backupLockTestSettingRepo) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, key)
	return nil
}

type backupLockTestEncryptor struct{}

func (e *backupLockTestEncryptor) Encrypt(plaintext string) (string, error) {
	return "ENC:" + plaintext, nil
}

func (e *backupLockTestEncryptor) Decrypt(ciphertext string) (string, error) {
	if strings.HasPrefix(ciphertext, "ENC:") {
		return strings.TrimPrefix(ciphertext, "ENC:"), nil
	}
	return "", fmt.Errorf("not encrypted")
}

type backupLockTestDumper struct {
	data []byte
}

func (d *backupLockTestDumper) Dump(_ context.Context) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(d.data)), nil
}

func (d *backupLockTestDumper) Restore(_ context.Context, data io.Reader) error {
	_, _ = io.ReadAll(data)
	return nil
}

type backupLockTestObjectStore struct {
	mu      sync.Mutex
	objects map[string][]byte
}

func newBackupLockTestObjectStore() *backupLockTestObjectStore {
	return &backupLockTestObjectStore{objects: make(map[string][]byte)}
}

func (s *backupLockTestObjectStore) Upload(_ context.Context, key string, body io.Reader, _ string) (int64, error) {
	data, err := io.ReadAll(body)
	if err != nil {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.objects[key] = data
	return int64(len(data)), nil
}

func (s *backupLockTestObjectStore) Download(_ context.Context, key string) (io.ReadCloser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, ok := s.objects[key]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (s *backupLockTestObjectStore) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.objects, key)
	return nil
}

func (s *backupLockTestObjectStore) PresignURL(_ context.Context, key string, _ time.Duration) (string, error) {
	return "https://example.test/" + key, nil
}

func (s *backupLockTestObjectStore) HeadBucket(_ context.Context) error { return nil }

type backupLockTestStore struct {
	mu        sync.Mutex
	acquireOK bool
	acquired  int
	released  int
	lastKey   string
	lastValue string
	lastTTL   time.Duration
}

func (s *backupLockTestStore) Acquire(_ context.Context, key string, value string, ttl time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.acquired++
	s.lastKey = key
	s.lastValue = value
	s.lastTTL = ttl
	return s.acquireOK, nil
}

func (s *backupLockTestStore) Release(_ context.Context, key string, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.released++
	s.lastKey = key
	s.lastValue = value
	return nil
}

func newBackupLockTestService(repo *backupLockTestSettingRepo, dumper DBDumper, objectStore *backupLockTestObjectStore, lockStore BackupScheduleLockStore) *BackupService {
	cfg := &config.Config{Database: config.DatabaseConfig{Host: "localhost", Port: 5432, User: "test", DBName: "sub2api"}}
	factory := func(context.Context, *BackupS3Config) (BackupObjectStore, error) { return objectStore, nil }
	return NewBackupService(repo, cfg, &backupLockTestEncryptor{}, factory, dumper, lockStore)
}

func seedBackupLockTestS3Config(t *testing.T, repo *backupLockTestSettingRepo) {
	t.Helper()
	cfg := BackupS3Config{Bucket: "bucket", AccessKeyID: "ak", SecretAccessKey: "ENC:sk", Prefix: "backups"}
	data, err := json.Marshal(cfg)
	require.NoError(t, err)
	require.NoError(t, repo.Set(context.Background(), settingKeyBackupS3Config, string(data)))
}

func TestBackupServiceScheduledLock_AllowsSingleScheduledBackup(t *testing.T) {
	repo := newBackupLockTestSettingRepo()
	seedBackupLockTestS3Config(t, repo)
	objectStore := newBackupLockTestObjectStore()
	lockStore := &backupLockTestStore{acquireOK: true}
	svc := newBackupLockTestService(repo, &backupLockTestDumper{data: []byte("scheduled-data")}, objectStore, lockStore)

	svc.runScheduledBackup()

	require.Equal(t, 1, lockStore.acquired)
	require.Equal(t, 1, lockStore.released)
	require.Equal(t, backupScheduleLockKey, lockStore.lastKey)
	require.NotEmpty(t, lockStore.lastValue)
	require.Equal(t, backupScheduleLockTTL, lockStore.lastTTL)

	records, err := svc.loadRecords(context.Background())
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Equal(t, "completed", records[0].Status)
	require.Equal(t, "scheduled", records[0].TriggeredBy)
	require.NotZero(t, records[0].SizeBytes)
}

func TestBackupServiceScheduledLock_SkipsWhenAnotherNodeHoldsLock(t *testing.T) {
	repo := newBackupLockTestSettingRepo()
	seedBackupLockTestS3Config(t, repo)
	lockStore := &backupLockTestStore{acquireOK: false}
	svc := newBackupLockTestService(repo, &backupLockTestDumper{data: []byte("scheduled-data")}, newBackupLockTestObjectStore(), lockStore)

	svc.runScheduledBackup()

	require.Equal(t, 1, lockStore.acquired)
	require.Equal(t, 0, lockStore.released)

	records, err := svc.loadRecords(context.Background())
	require.NoError(t, err)
	require.Empty(t, records)
}
