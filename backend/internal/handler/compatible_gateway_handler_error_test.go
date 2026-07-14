package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type compatibleErrorPassthroughRepo struct {
	rules []*model.ErrorPassthroughRule
}

func (r *compatibleErrorPassthroughRepo) List(context.Context) ([]*model.ErrorPassthroughRule, error) {
	return r.rules, nil
}

func (r *compatibleErrorPassthroughRepo) GetByID(context.Context, int64) (*model.ErrorPassthroughRule, error) {
	return nil, nil
}

func (r *compatibleErrorPassthroughRepo) Create(_ context.Context, rule *model.ErrorPassthroughRule) (*model.ErrorPassthroughRule, error) {
	return rule, nil
}

func (r *compatibleErrorPassthroughRepo) Update(_ context.Context, rule *model.ErrorPassthroughRule) (*model.ErrorPassthroughRule, error) {
	return rule, nil
}

func (r *compatibleErrorPassthroughRepo) Delete(context.Context, int64) error { return nil }

func TestCompatibleWriteFailoverErrorSanitizesUpstreamMessage(t *testing.T) {
	h := newCompatibleErrorTestHandler(nil)
	rec, c := newCompatibleErrorTestContext()

	h.writeFailoverError(c, service.CompatibleRouteChatCompletions, &service.UpstreamFailoverError{
		StatusCode:   http.StatusBadGateway,
		ResponseBody: []byte(`{"error":{"message":"relay https://api.private.example/v1 failed at 10.20.30.40:443; Authorization: Bearer sk-upstream-secret; access_token=refresh-secret"}}`),
	}, http.StatusBadGateway, false, service.PlatformZhipu)

	message := compatibleErrorResponseMessage(t, rec)
	assertCompatibleMessageSafe(t, message, "api.private.example", "10.20.30.40", "sk-upstream-secret", "refresh-secret")
	if !strings.Contains(message, "relay") || !strings.Contains(message, "failed") {
		t.Fatalf("sanitized message lost useful context: %q", message)
	}
}

func TestCompatibleWriteFailoverErrorSanitizesPassthroughBody(t *testing.T) {
	rule := &model.ErrorPassthroughRule{
		ID:              1,
		Name:            "safe passthrough",
		Enabled:         true,
		Priority:        1,
		ErrorCodes:      []int{http.StatusUnprocessableEntity},
		MatchMode:       model.MatchModeAny,
		Platforms:       []string{service.PlatformZhipu},
		PassthroughCode: true,
		PassthroughBody: true,
	}
	h := newCompatibleErrorTestHandler([]*model.ErrorPassthroughRule{rule})
	rec, c := newCompatibleErrorTestContext()

	h.writeFailoverError(c, service.CompatibleRouteChatCompletions, &service.UpstreamFailoverError{
		StatusCode:   http.StatusUnprocessableEntity,
		ResponseBody: []byte(`{"error":{"message":"schema rejected by relay.example.com at 192.168.1.8; api_key=sk-secret"}}`),
	}, http.StatusBadGateway, false, service.PlatformZhipu)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want passthrough status %d", rec.Code, http.StatusUnprocessableEntity)
	}
	message := compatibleErrorResponseMessage(t, rec)
	assertCompatibleMessageSafe(t, message, "relay.example.com", "192.168.1.8", "sk-secret")
	if !strings.Contains(message, "schema rejected") {
		t.Fatalf("passthrough message lost useful context: %q", message)
	}
}

func TestCompatibleWriteFailoverErrorKeepsConfiguredCustomMessage(t *testing.T) {
	responseCode := http.StatusConflict
	customMessage := "Configured maintenance at https://relay.internal.example token=leaked-secret"
	rule := &model.ErrorPassthroughRule{
		ID:              2,
		Name:            "custom response",
		Enabled:         true,
		Priority:        1,
		ErrorCodes:      []int{http.StatusBadGateway},
		MatchMode:       model.MatchModeAny,
		Platforms:       []string{service.PlatformZhipu},
		PassthroughCode: false,
		ResponseCode:    &responseCode,
		PassthroughBody: false,
		CustomMessage:   &customMessage,
	}
	h := newCompatibleErrorTestHandler([]*model.ErrorPassthroughRule{rule})
	rec, c := newCompatibleErrorTestContext()

	h.writeFailoverError(c, service.CompatibleRouteChatCompletions, &service.UpstreamFailoverError{
		StatusCode:   http.StatusBadGateway,
		ResponseBody: []byte(`{"error":{"message":"api_key=sk-secret at 10.0.0.1"}}`),
	}, http.StatusBadGateway, false, service.PlatformZhipu)

	if rec.Code != responseCode {
		t.Fatalf("status = %d, want configured status %d", rec.Code, responseCode)
	}
	message := compatibleErrorResponseMessage(t, rec)
	if !strings.Contains(message, "Configured maintenance") {
		t.Fatalf("message lost configured context: %q", message)
	}
	assertCompatibleMessageSafe(t, message, "relay.internal.example", "leaked-secret")
}

func TestCompatibleWriteFailoverErrorKeepsStatusMappingForEmptyMessage(t *testing.T) {
	h := newCompatibleErrorTestHandler(nil)
	rec, c := newCompatibleErrorTestContext()

	h.writeFailoverError(c, service.CompatibleRouteChatCompletions, &service.UpstreamFailoverError{
		StatusCode:   http.StatusTooManyRequests,
		ResponseBody: []byte(`{"error":{"type":"rate_limit_error"}}`),
	}, http.StatusBadGateway, false, service.PlatformZhipu)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}
	if message := compatibleErrorResponseMessage(t, rec); message != "Upstream rate limit exceeded, please retry later" {
		t.Fatalf("message = %q, want mapped rate-limit message", message)
	}
}

func TestSanitizeCompatibleUpstreamClientMessageRemovesNetworkIdentifiers(t *testing.T) {
	message := sanitizeCompatibleUpstreamClientMessage("dial tcp [2001:db8::1]:443 via localhost:8080 and api.internal.example: Authorization Bearer secret-token")
	assertCompatibleMessageSafe(t, message, "2001:db8::1", "localhost", "api.internal.example", "secret-token")
}

func newCompatibleErrorTestHandler(rules []*model.ErrorPassthroughRule) *CompatibleGatewayHandler {
	base := &GatewayHandler{}
	if rules != nil {
		base.errorPassthroughService = service.NewErrorPassthroughService(&compatibleErrorPassthroughRepo{rules: rules}, nil)
	}
	return &CompatibleGatewayHandler{base: base}
}

func newCompatibleErrorTestContext() (*httptest.ResponseRecorder, *gin.Context) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	return rec, c
}

func compatibleErrorResponseMessage(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var payload struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response body %q: %v", rec.Body.String(), err)
	}
	return payload.Error.Message
}

func assertCompatibleMessageSafe(t *testing.T, message string, forbidden ...string) {
	t.Helper()
	for _, value := range forbidden {
		if strings.Contains(message, value) {
			t.Fatalf("message %q leaked %q", message, value)
		}
	}
}
