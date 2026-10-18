package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type tlsFingerprintProfileRepoStub struct {
	createErr error
}

func (r *tlsFingerprintProfileRepoStub) List(context.Context) ([]*model.TLSFingerprintProfile, error) {
	return nil, nil
}

func (r *tlsFingerprintProfileRepoStub) GetByID(context.Context, int64) (*model.TLSFingerprintProfile, error) {
	return nil, nil
}

func (r *tlsFingerprintProfileRepoStub) Create(context.Context, *model.TLSFingerprintProfile) (*model.TLSFingerprintProfile, error) {
	return nil, r.createErr
}

func (r *tlsFingerprintProfileRepoStub) Update(context.Context, *model.TLSFingerprintProfile) (*model.TLSFingerprintProfile, error) {
	return nil, nil
}

func (r *tlsFingerprintProfileRepoStub) Delete(context.Context, int64) error {
	return nil
}

func TestTLSFingerprintProfileCreateReturnsConflictForDuplicateName(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &tlsFingerprintProfileRepoStub{createErr: &ent.ConstraintError{}}
	handler := NewTLSFingerprintProfileHandler(service.NewTLSFingerprintProfileService(repo, nil))
	router := gin.New()
	router.POST("/api/v1/admin/tls-fingerprint-profiles", handler.Create)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/tls-fingerprint-profiles", strings.NewReader(`{"name":"Claude Code TLS Profile"}`))
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusConflict, recorder.Code)
	var response map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, float64(http.StatusConflict), response["code"])
	require.Equal(t, "TLS fingerprint profile name already exists", response["message"])
}
