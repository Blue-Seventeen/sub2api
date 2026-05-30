package handler

import (
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestNewAPIStyleGroupModelsListModel(t *testing.T) {
	tests := []struct {
		name      string
		route     service.NewAPIStyleRoute
		model     string
		method    string
		path      string
		wantModel string
		wantOK    bool
	}{
		{
			name:      "explicit model wins",
			route:     service.NewAPIStyleRouteSuno,
			model:     "Custom-Model",
			method:    http.MethodPost,
			path:      "/suno/submit/music",
			wantModel: "Custom-Model",
			wantOK:    true,
		},
		{
			name:      "qwen tts official route uses request model",
			route:     service.NewAPIStyleRouteQwenTTS,
			model:     "qwen3-tts-flash",
			method:    http.MethodPost,
			path:      "/api/v1/services/aigc/multimodal-generation/generation",
			wantModel: "qwen3-tts-flash",
			wantOK:    true,
		},
		{
			name:      "suno music submit infers product model",
			route:     service.NewAPIStyleRouteSuno,
			method:    http.MethodPost,
			path:      "/suno/submit/music",
			wantModel: "suno_music",
			wantOK:    true,
		},
		{
			name:      "suno lyrics submit infers product model",
			route:     service.NewAPIStyleRouteSuno,
			method:    http.MethodPost,
			path:      "/suno/submit/lyrics",
			wantModel: "suno_lyrics",
			wantOK:    true,
		},
		{
			name:   "suno fetch does not enforce model whitelist",
			route:  service.NewAPIStyleRouteSuno,
			method: http.MethodPost,
			path:   "/suno/fetch",
		},
		{
			name:      "midjourney submit infers product model",
			route:     service.NewAPIStyleRouteMidjourney,
			method:    http.MethodPost,
			path:      "/mj/submit/imagine",
			wantModel: "midjourney",
			wantOK:    true,
		},
		{
			name:      "kling text2video submit infers default model",
			route:     service.NewAPIStyleRouteKling,
			method:    http.MethodPost,
			path:      "/kling/v1/videos/text2video",
			wantModel: "kling-v1",
			wantOK:    true,
		},
		{
			name:   "task polling get does not enforce model whitelist",
			route:  service.NewAPIStyleRouteMidjourney,
			method: http.MethodGet,
			path:   "/mj/task/123/fetch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotModel, gotOK := newAPIStyleGroupModelsListModel(tt.route, tt.model, tt.method, tt.path)
			require.Equal(t, tt.wantOK, gotOK)
			require.Equal(t, tt.wantModel, gotModel)
		})
	}
}
