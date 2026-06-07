//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type updateServiceCacheStub struct {
	data string
}

func (s *updateServiceCacheStub) GetUpdateInfo(context.Context) (string, error) {
	if s.data == "" {
		return "", errors.New("cache miss")
	}
	return s.data, nil
}

func (s *updateServiceCacheStub) SetUpdateInfo(_ context.Context, data string, _ time.Duration) error {
	s.data = data
	return nil
}

type updateServiceGitHubClientStub struct {
	release *GitHubRelease
	repo    string
}

func (s *updateServiceGitHubClientStub) FetchLatestRelease(_ context.Context, repo string) (*GitHubRelease, error) {
	s.repo = repo
	return s.release, nil
}

func (s *updateServiceGitHubClientStub) DownloadFile(context.Context, string, string, int64) error {
	panic("DownloadFile should not be called when no update is available")
}

func (s *updateServiceGitHubClientStub) FetchChecksumFile(context.Context, string) ([]byte, error) {
	panic("FetchChecksumFile should not be called when no update is available")
}

func TestUpdateServicePerformUpdateNoUpdateReturnsSentinel(t *testing.T) {
	svc := NewUpdateService(
		&updateServiceCacheStub{},
		&updateServiceGitHubClientStub{
			release: &GitHubRelease{
				TagName: "v0.1.132",
				Name:    "v0.1.132",
			},
		},
		"0.1.132",
		"release",
	)

	err := svc.PerformUpdate(context.Background())

	require.Error(t, err)
	require.True(t, errors.Is(err, ErrNoUpdateAvailable))
	require.ErrorIs(t, err, ErrNoUpdateAvailable)
}

func TestUpdateServiceUsesOfficialRepositoryByDefault(t *testing.T) {
	client := &updateServiceGitHubClientStub{
		release: &GitHubRelease{TagName: "v0.1.133", Name: "v0.1.133"},
	}
	svc := NewUpdateService(&updateServiceCacheStub{}, client, "0.1.132", "release")

	info, err := svc.CheckUpdate(context.Background(), true)

	require.NoError(t, err)
	require.Equal(t, defaultUpdateRepo, client.repo)
	require.Equal(t, defaultUpdateRepo, info.Repository)
}

func TestUpdateServiceIgnoresDeploymentRepositoryEnv(t *testing.T) {
	t.Setenv("SUB2API_UPDATE_REPO", "example/sub2api-fork")
	t.Setenv("SUB2API_REPO", "Blue-Seventeen/sub2api")
	client := &updateServiceGitHubClientStub{
		release: &GitHubRelease{TagName: "v0.1.133", Name: "v0.1.133"},
	}
	svc := NewUpdateService(&updateServiceCacheStub{}, client, "0.1.132", "release")

	info, err := svc.CheckUpdate(context.Background(), true)

	require.NoError(t, err)
	require.Equal(t, defaultUpdateRepo, client.repo)
	require.Equal(t, defaultUpdateRepo, info.Repository)
}
