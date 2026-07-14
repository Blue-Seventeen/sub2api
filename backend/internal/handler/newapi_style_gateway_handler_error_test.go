package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func TestNewAPIStyleWriteFailoverErrorSanitizesClientMessage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	h := &NewAPIStyleGatewayHandler{base: &GatewayHandler{}}

	h.writeFailoverError(c, service.NewAPIStyleRouteChatCompletions, &service.UpstreamFailoverError{
		StatusCode:   http.StatusBadGateway,
		ResponseBody: []byte(`{"error":{"message":"relay https://api.internal.example/v1 at 10.0.0.8 rejected api_key=sk-newapi-secret-123456"}}`),
	}, http.StatusBadGateway, false, service.PlatformZhipu)

	var payload struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	for _, leaked := range []string{"api.internal.example", "10.0.0.8", "sk-newapi-secret-123456"} {
		if strings.Contains(payload.Error.Message, leaked) {
			t.Fatalf("message %q leaked %q", payload.Error.Message, leaked)
		}
	}
}
