package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type createProxyAdminStub struct {
	*stubAdminService
	creates, binds int
}

func TestCodexImportProxyModeChanges(t *testing.T) {
	for _, mode := range []string{"auto", "manual"} {
		t.Run(mode, func(t *testing.T) {
			token := buildCodexImportTestJWT(t, time.Now().Add(time.Hour), map[string]any{"sub": "user-1", "https://api.openai.com/auth": map[string]any{"chatgpt_account_id": "workspace-1"}})
			fixed := int64(17)
			existing := service.Account{ID: 7, Name: "existing", Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, ProxyID: &fixed, Credentials: map[string]any{"access_token": token, "chatgpt_account_id": "workspace-1", "chatgpt_user_id": "user-1"}, Extra: map[string]any{"quota_limit": 42.0}}
			req := CodexSessionImportRequest{Content: token}
			if mode == "auto" {
				req.Extra = map[string]any{"auto_select_proxy": true}
			} else {
				existing.ProxyID = nil
				existing.Extra["auto_select_proxy"] = true
				req.ProxyID = &fixed
			}
			svc := newCodexImportMemoryAdminService([]service.Account{existing})
			h := &AccountHandler{adminService: svc}
			entries, err := parseCodexSessionImportEntries(req)
			require.NoError(t, err)
			result, err := h.importCodexSessions(context.Background(), req, entries)
			require.NoError(t, err)
			require.Equal(t, 1, result.Updated)
			require.Len(t, svc.updatedAccounts, 1)
			updated := svc.updatedAccounts[0].input
			require.Equal(t, 42.0, updated.Extra["quota_limit"])
			if mode == "auto" {
				require.NotNil(t, updated.ProxyID)
				require.Zero(t, *updated.ProxyID)
				require.Equal(t, true, updated.Extra["auto_select_proxy"])
			} else {
				require.Equal(t, &fixed, updated.ProxyID)
				require.Equal(t, false, updated.Extra["auto_select_proxy"])
			}
		})
	}
}

func (s *createProxyAdminStub) CreateAccount(_ context.Context, in *service.CreateAccountInput) (*service.Account, error) {
	s.creates++
	return &service.Account{ID: 42, Platform: in.Platform, Type: in.Type, Extra: in.Extra, ProxyID: in.ProxyID}, nil
}
func (s *createProxyAdminStub) ValidateAccountGroupBindings(context.Context, []int64) error {
	s.binds++
	return nil
}

func TestAccountCreateRejectsInvalidProxyBeforeWrites(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, batch := range []bool{false, true} {
		for _, config := range []map[string]any{
			{"proxy_id": -1}, {"proxy_id": 0}, {"extra": map[string]any{"auto_select_proxy": "true"}}, {"proxy_id": 17, "extra": map[string]any{"auto_select_proxy": true}},
		} {
			t.Run(string(rune('a'+len(config)))+func() string {
				if batch {
					return "_batch"
				}
				return "_single"
			}(), func(t *testing.T) {
				svc := &createProxyAdminStub{stubAdminService: newStubAdminService()}
				h := &AccountHandler{adminService: svc}
				payload := map[string]any{"name": "fixture", "platform": "deepseek", "type": "apikey", "credentials": map[string]any{"api_key": "fixture"}}
				for k, v := range config {
					payload[k] = v
				}
				var body any = payload
				if batch {
					body = map[string]any{"accounts": []any{map[string]any{"name": "valid", "platform": "deepseek", "type": "apikey", "credentials": map[string]any{"api_key": "fixture"}}, payload}}
				}
				data, _ := json.Marshal(body)
				req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(data))
				req.Header.Set("Content-Type", "application/json")
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = req
				if batch {
					h.BatchCreate(c)
				} else {
					h.Create(c)
				}
				require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
				require.Zero(t, svc.creates)
				require.Zero(t, svc.binds)
			})
		}
	}
}
