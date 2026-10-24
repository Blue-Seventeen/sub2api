//go:build e2e

package integration

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

type e2eAuthData struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	User        struct {
		ID    int64  `json:"id"`
		Email string `json:"email"`
	} `json:"user"`
}

func TestUserRegistrationAndLogin(t *testing.T) {
	email := fmt.Sprintf("e2e-test-%d@example.com", time.Now().UnixNano())
	var secret [24]byte
	if _, err := rand.Read(secret[:]); err != nil {
		t.Fatal("cannot generate isolated registration password")
	}
	password := "E2e-" + hex.EncodeToString(secret[:])
	if !t.Run("注册新用户", func(t *testing.T) {
		body, err := json.Marshal(map[string]string{"email": email, "password": password})
		if err != nil {
			t.Fatal("cannot encode registration request")
		}
		resp := mustE2ERequest(t, http.MethodPost, "/api/v1/auth/register", body, "")
		defer resp.Body.Close()
		var auth e2eAuthData
		decodeE2EAPIData(t, resp, &auth)
		requireE2EAuth(t, auth, email)
	}) {
		return
	}

	var accessToken string
	if !t.Run("用户登录获取JWT", func(t *testing.T) {
		accessToken = loginE2EUser(t, email, password)
	}) {
		return
	}
	t.Run("获取当前用户信息", func(t *testing.T) {
		resp := mustE2ERequest(t, http.MethodGet, "/api/v1/auth/me", nil, accessToken)
		defer resp.Body.Close()
		var user struct {
			ID    int64  `json:"id"`
			Email string `json:"email"`
		}
		decodeE2EAPIData(t, resp, &user)
		if user.ID <= 0 || user.Email != email {
			t.Fatal("current user response does not match the registered user")
		}
	})
}

func TestAPIKeyLifecycle(t *testing.T) {
	accessToken := loginTestUser(t)
	groupRaw := strings.TrimSpace(os.Getenv("E2E_GROUP_ID"))
	if groupRaw == "" {
		groupRaw = requireE2EEnv(t, "E2E_GROUP_ID")
	}
	groupID, err := strconv.ParseInt(groupRaw, 10, 64)
	if err != nil || groupID <= 0 {
		t.Fatal("E2E_GROUP_ID must be a positive fixture group ID")
	}
	model := requireE2EEnv(t, "E2E_USER_MODEL")
	var created struct {
		ID      int64  `json:"id"`
		Key     string `json:"key"`
		GroupID *int64 `json:"group_id"`
	}
	if !t.Run("创建API_Key", func(t *testing.T) {
		body, err := json.Marshal(map[string]any{
			"name": fmt.Sprintf("e2e-test-key-%d", time.Now().UnixNano()), "group_id": groupID,
		})
		if err != nil {
			t.Fatal("cannot encode API key request")
		}
		resp := mustE2ERequest(t, http.MethodPost, "/api/v1/keys", body, accessToken)
		defer resp.Body.Close()
		decodeE2EAPIData(t, resp, &created)
		if created.ID <= 0 || created.Key == "" || created.GroupID == nil || *created.GroupID != groupID {
			t.Fatal("created API key is missing its ID, credential, or requested group")
		}
	}) {
		return
	}
	t.Cleanup(func() {
		resp := mustE2ERequest(t, http.MethodDelete, fmt.Sprintf("/api/v1/keys/%d", created.ID), nil, accessToken)
		defer resp.Body.Close()
		var data struct {
			Message string `json:"message"`
		}
		decodeE2EAPIData(t, resp, &data)
		if data.Message == "" {
			t.Error("API key deletion response is missing confirmation")
		}
	})
	if !t.Run("使用API_Key调用网关", func(t *testing.T) {
		resp := mustE2ERequest(t, http.MethodGet, endpointPrefix+"/v1/models", nil, created.Key)
		defer resp.Body.Close()
		requireE2EStatus(t, resp, http.StatusOK)
		var catalog struct {
			Object string            `json:"object"`
			Data   []json.RawMessage `json:"data"`
		}
		decodeE2EJSON(t, resp.Body, &catalog)
		if catalog.Object != "list" || len(catalog.Data) == 0 {
			t.Fatal("gateway model catalog must be a non-empty list")
		}
		testClaudeMessage(t, created.Key, model, false)
	}) {
		return
	}
	t.Run("查询用量记录", func(t *testing.T) {
		deadline := time.Now().Add(20 * time.Second)
		for {
			resp := mustE2ERequest(t, http.MethodGet, fmt.Sprintf("/api/v1/usage?api_key_id=%d&page=1&page_size=10", created.ID), nil, accessToken)
			var usage struct {
				Items *[]struct {
					ID       int64 `json:"id"`
					APIKeyID int64 `json:"api_key_id"`
				} `json:"items"`
			}
			decodeE2EAPIData(t, resp, &usage)
			_ = resp.Body.Close()
			if usage.Items == nil {
				t.Fatal("usage response is missing its items array")
			}
			found := false
			for _, record := range *usage.Items {
				found = found || (record.ID > 0 && record.APIKeyID == created.ID)
			}
			if found {
				break
			}
			if !time.Now().Before(deadline) {
				t.Fatal("gateway request did not persist usage for the newly created API key")
			}
			time.Sleep(time.Second)
		}
		resp := mustE2ERequest(t, http.MethodGet, "/api/v1/usage/dashboard/stats", nil, accessToken)
		defer resp.Body.Close()
		var stats struct {
			TotalRequests *int64 `json:"total_requests"`
		}
		decodeE2EAPIData(t, resp, &stats)
		if stats.TotalRequests == nil || *stats.TotalRequests < 1 {
			t.Fatal("usage dashboard must report the completed gateway request")
		}
	})
}

func doRequest(t *testing.T, method, path string, body []byte, token string) (*http.Response, error) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, baseURL+path, reader)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if method == http.MethodPost && path == "/api/v1/keys" {
		req.Header.Set("Idempotency-Key", fmt.Sprintf("e2e-key-%d", time.Now().UnixNano()))
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return (&http.Client{Timeout: 30 * time.Second}).Do(req)
}

func mustE2ERequest(t *testing.T, method, path string, body []byte, token string) *http.Response {
	t.Helper()
	resp, err := doRequest(t, method, path, body, token)
	if err != nil {
		t.Fatalf("%s request failed (credentials and response omitted)", method)
	}
	return resp
}

func requireE2EAuth(t *testing.T, auth e2eAuthData, email string) {
	t.Helper()
	if len(strings.Split(auth.AccessToken, ".")) != 3 || auth.TokenType != "Bearer" || auth.User.ID <= 0 || auth.User.Email != email {
		t.Fatal("authentication response must contain a JWT, Bearer token type, and the expected user")
	}
}

func loginE2EUser(t *testing.T, email, password string) string {
	t.Helper()
	body, err := json.Marshal(map[string]string{"email": email, "password": password})
	if err != nil {
		t.Fatal("cannot encode login request")
	}
	resp := mustE2ERequest(t, http.MethodPost, "/api/v1/auth/login", body, "")
	defer resp.Body.Close()
	var auth e2eAuthData
	decodeE2EAPIData(t, resp, &auth)
	requireE2EAuth(t, auth, email)
	return auth.AccessToken
}

func loginTestUser(t *testing.T) string {
	t.Helper()
	email, password := strings.TrimSpace(os.Getenv("E2E_USER_EMAIL")), os.Getenv("E2E_USER_PASSWORD")
	if email == "" && password == "" {
		email, password = strings.TrimSpace(os.Getenv("ADMIN_EMAIL")), os.Getenv("ADMIN_PASSWORD")
	}
	if email == "" || password == "" {
		if strictE2E() {
			t.Fatal("strict E2E requires E2E_USER_EMAIL/E2E_USER_PASSWORD or ADMIN_EMAIL/ADMIN_PASSWORD fixtures")
		}
		t.Skip("user credentials fixture is not configured")
	}
	return loginE2EUser(t, email, password)
}

func redactAPIKey(string) string { return "[redacted]" }
