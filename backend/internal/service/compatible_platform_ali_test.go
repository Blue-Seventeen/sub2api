//go:build unit

package service

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/tidwall/gjson"
)

func TestAliCompatibleProviderPreset(t *testing.T) {
	preset := aliCompatibleProviderPreset()

	if preset.Platform != PlatformAli {
		t.Fatalf("Platform = %q, want %q", preset.Platform, PlatformAli)
	}
	if preset.DefaultBaseURL != "https://dashscope.aliyuncs.com" {
		t.Fatalf("DefaultBaseURL = %q", preset.DefaultBaseURL)
	}
	if preset.DefaultTestModel != "qwen-turbo" {
		t.Fatalf("DefaultTestModel = %q", preset.DefaultTestModel)
	}
	if preset.AuthMode != CompatibleAuthBearer {
		t.Fatalf("AuthMode = %q, want %q", preset.AuthMode, CompatibleAuthBearer)
	}
	if !preset.SupportsChat {
		t.Fatal("SupportsChat = false, want true")
	}
	if !preset.SupportsResponses {
		t.Fatal("SupportsResponses = false, want true")
	}
	if preset.SupportsMessages == nil {
		t.Fatal("SupportsMessages should not be nil")
	}
	if len(preset.DefaultModels) != 4 {
		t.Fatalf("len(DefaultModels) = %d, want 4", len(preset.DefaultModels))
	}

	wantModels := []string{"qwen-turbo", "qwen-plus", "qwen-max", "qwq-32b"}
	for i, want := range wantModels {
		if preset.DefaultModels[i].ID != want {
			t.Fatalf("DefaultModels[%d].ID = %q, want %q", i, preset.DefaultModels[i].ID, want)
		}
	}
}

func TestAliCompatibleProviderPreset_Routes(t *testing.T) {
	preset := aliCompatibleProviderPreset()
	baseURL := "https://dashscope.aliyuncs.com/"

	wantChat := "https://dashscope.aliyuncs.com/compatible-mode/v1/chat/completions"
	if got := preset.BuildChatURL(baseURL, "qwen-plus"); got != wantChat {
		t.Fatalf("BuildChatURL() = %q, want %q", got, wantChat)
	}

	wantResponses := "https://dashscope.aliyuncs.com/api/v2/apps/protocols/compatible-mode/v1/responses"
	if got := preset.BuildResponsesURL(baseURL, "qwen-plus"); got != wantResponses {
		t.Fatalf("BuildResponsesURL() = %q, want %q", got, wantResponses)
	}

	wantMessages := "https://dashscope.aliyuncs.com/apps/anthropic/v1/messages"
	if got := preset.BuildMessagesURL(baseURL, " qWeN-plus "); got != wantMessages {
		t.Fatalf("BuildMessagesURL(qwen) = %q, want %q", got, wantMessages)
	}

	if got := preset.BuildMessagesURL(baseURL, "qwq-32b"); got != wantChat {
		t.Fatalf("BuildMessagesURL(non-qwen) = %q, want %q", got, wantChat)
	}
}

func TestAliCompatibleProviderPreset_SupportsMessages(t *testing.T) {
	preset := aliCompatibleProviderPreset()

	if !preset.SupportsMessages("qwen-turbo") {
		t.Fatal("SupportsMessages(qwen-turbo) = false, want true")
	}
	if !preset.SupportsMessages(" QWEN-max ") {
		t.Fatal("SupportsMessages(QWEN-max) = false, want true")
	}
	if preset.SupportsMessages("qwq-32b") {
		t.Fatal("SupportsMessages(qwq-32b) = true, want false")
	}
	if preset.SupportsMessages("deepseek-v3") {
		t.Fatal("SupportsMessages(deepseek-v3) = true, want false")
	}
}

func TestAliCompatibleProviderPreset_PatchHeaders(t *testing.T) {
	preset := aliCompatibleProviderPreset()

	tests := []struct {
		name    string
		patch   func(*http.Request, *Account, string)
		url     string
		wantSSE string
	}{
		{
			name:    "chat adds sse header",
			patch:   preset.PatchChatHeaders,
			url:     "https://example.com/compatible-mode/v1/chat/completions",
			wantSSE: "enable",
		},
		{
			name:    "responses adds sse header",
			patch:   preset.PatchResponsesHeaders,
			url:     "https://example.com/api/v2/apps/protocols/compatible-mode/v1/responses",
			wantSSE: "enable",
		},
		{
			name:    "messages skips sse header",
			patch:   preset.PatchChatHeaders,
			url:     "https://example.com/apps/anthropic/v1/messages",
			wantSSE: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodPost, tt.url, nil)
			if err != nil {
				t.Fatalf("NewRequest() error = %v", err)
			}

			tt.patch(req, nil, "qwen-plus")

			if got := getHeaderRaw(req.Header, "x-dashscope-sse"); got != tt.wantSSE {
				t.Fatalf("X-DashScope-SSE = %q, want %q", got, tt.wantSSE)
			}
		})
	}
}

func TestAliCompatibleProviderPreset_PatchBody(t *testing.T) {
	preset := aliCompatibleProviderPreset()

	patchers := map[string]func([]byte, *Account, string) ([]byte, error){
		"chat":      preset.PatchChatBody,
		"responses": preset.PatchResponsesBody,
	}

	tests := []struct {
		name        string
		body        []byte
		wantTopPRaw string
		wantStop    []string
	}{
		{
			name:        "clamps high top_p and wraps stop string",
			body:        []byte(`{"top_p":1.2,"stop":"END"}`),
			wantTopPRaw: "0.99",
			wantStop:    []string{"END"},
		},
		{
			name:        "clamps low top_p and keeps stop array",
			body:        []byte(`{"top_p":0,"stop":["END","DONE"]}`),
			wantTopPRaw: "0.001",
			wantStop:    []string{"END", "DONE"},
		},
	}

	for patchName, patcher := range patchers {
		if patcher == nil {
			t.Fatalf("%s patcher should not be nil", patchName)
		}

		for _, tt := range tests {
			t.Run(patchName+"/"+tt.name, func(t *testing.T) {
				patched, err := patcher(tt.body, nil, "qwen-plus")
				if err != nil {
					t.Fatalf("%s patcher error = %v", patchName, err)
				}

				if got := gjson.GetBytes(patched, "top_p").Raw; got != tt.wantTopPRaw {
					t.Fatalf("%s patched top_p = %s, want %s", patchName, got, tt.wantTopPRaw)
				}

				stop := gjson.GetBytes(patched, "stop")
				if !stop.IsArray() {
					t.Fatalf("%s patched stop is not array: %s", patchName, stop.Raw)
				}
				if len(stop.Array()) != len(tt.wantStop) {
					t.Fatalf("%s patched stop len = %d, want %d", patchName, len(stop.Array()), len(tt.wantStop))
				}
				for i, want := range tt.wantStop {
					if got := gjson.GetBytes(patched, "stop."+strconv.Itoa(i)).String(); got != want {
						t.Fatalf("%s patched stop[%d] = %q, want %q", patchName, i, got, want)
					}
				}
			})
		}
	}
}
