//go:build unit

package service

import (
	"net/http"
	"testing"

	"github.com/tidwall/gjson"
)

func TestZhipuCompatibleProviderPreset(t *testing.T) {
	preset := zhipuCompatibleProviderPreset()

	if preset.Platform != PlatformZhipu {
		t.Fatalf("Platform = %q, want %q", preset.Platform, PlatformZhipu)
	}
	if preset.DefaultBaseURL != "https://open.bigmodel.cn" {
		t.Fatalf("DefaultBaseURL = %q", preset.DefaultBaseURL)
	}
	if preset.DefaultTestModel != "glm-4.5-flash" {
		t.Fatalf("DefaultTestModel = %q", preset.DefaultTestModel)
	}
	if preset.AuthMode != CompatibleAuthZhipuToken {
		t.Fatalf("AuthMode = %q, want %q", preset.AuthMode, CompatibleAuthZhipuToken)
	}
	if !preset.SupportsChat {
		t.Fatal("SupportsChat = false, want true")
	}
	if preset.SupportsResponses {
		t.Fatal("SupportsResponses = true, want false")
	}
	if preset.SupportsMessages == nil {
		t.Fatal("SupportsMessages should not be nil")
	}
	if !preset.SupportsMessages("glm-4.5") {
		t.Fatal("SupportsMessages(glm-4.5) = false, want true")
	}
	if len(preset.DefaultModels) != 4 {
		t.Fatalf("len(DefaultModels) = %d, want 4", len(preset.DefaultModels))
	}
	wantModels := []string{"glm-4.5", "glm-4.5-air", "glm-4.5-flash", "glm-4-plus"}
	for i, want := range wantModels {
		if preset.DefaultModels[i].ID != want {
			t.Fatalf("DefaultModels[%d].ID = %q, want %q", i, preset.DefaultModels[i].ID, want)
		}
	}
}

func TestZhipuCompatibleProviderPreset_Routes(t *testing.T) {
	preset := zhipuCompatibleProviderPreset()
	baseURL := "https://open.bigmodel.cn/"
	wantChat := "https://open.bigmodel.cn/api/paas/v4/chat/completions"
	wantMessages := "https://open.bigmodel.cn/api/anthropic/v1/messages"

	if got := preset.BuildChatURL(baseURL, "glm-4.5"); got != wantChat {
		t.Fatalf("BuildChatURL() = %q, want %q", got, wantChat)
	}
	if got := preset.BuildResponsesURL(baseURL, "glm-4.5"); got != wantChat {
		t.Fatalf("BuildResponsesURL() = %q, want %q", got, wantChat)
	}
	if got := preset.BuildMessagesURL(baseURL, "glm-4.5"); got != wantMessages {
		t.Fatalf("BuildMessagesURL() = %q, want %q", got, wantMessages)
	}
	if got := preset.BuildMessagesURL("https://open.bigmodel.cn/api/anthropic", "glm-4.5"); got != wantMessages {
		t.Fatalf("BuildMessagesURL(anthropic base) = %q, want %q", got, wantMessages)
	}
	if got := preset.BuildMessagesURL("https://open.bigmodel.cn/api/anthropic/v1", "glm-4.5"); got != wantMessages {
		t.Fatalf("BuildMessagesURL(anthropic v1 base) = %q, want %q", got, wantMessages)
	}
}

func TestZhipuCompatibleProviderPreset_PatchHeaders(t *testing.T) {
	preset := zhipuCompatibleProviderPreset()
	account := &Account{
		Credentials: map[string]any{
			"token":   "generated-zhipu-token",
			"api_key": "zhipu-api-key",
		},
	}

	req, err := http.NewRequest(http.MethodPost, "https://example.com", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	preset.PatchChatHeaders(req, account, "glm-4.5")
	if got := getHeaderRaw(req.Header, "authorization"); got != "Bearer generated-zhipu-token" {
		t.Fatalf("authorization = %q, want %q", got, "Bearer generated-zhipu-token")
	}

	reqWithAuth, err := http.NewRequest(http.MethodPost, "https://example.com", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	setHeaderRaw(reqWithAuth.Header, "authorization", "Bearer already-set")
	preset.PatchResponsesHeaders(reqWithAuth, account, "glm-4.5")
	if got := getHeaderRaw(reqWithAuth.Header, "authorization"); got != "Bearer already-set" {
		t.Fatalf("authorization after patch = %q, want %q", got, "Bearer already-set")
	}
}

func TestZhipuCompatibleProviderPreset_PatchBody(t *testing.T) {
	preset := zhipuCompatibleProviderPreset()
	body := []byte(`{
		"top_p": 1.2,
		"stop": "END",
		"max_completion_tokens": 128,
		"messages": [
			{
				"role": "developer",
				"content": "follow developer instruction"
			},
			{
				"role": "user",
				"content": [
					{
						"type": "image_url",
						"image_url": {
							"url": "data:image/png;base64,Zm9vYmFy"
						}
					}
				]
			}
		]
	}`)

	patchers := map[string]func([]byte, *Account, string) ([]byte, error){
		"chat":      preset.PatchChatBody,
		"responses": preset.PatchResponsesBody,
	}

	for name, patcher := range patchers {
		if patcher == nil {
			t.Fatalf("%s patcher should not be nil", name)
		}
		patched, err := patcher(body, nil, "glm-4.5")
		if err != nil {
			t.Fatalf("%s patcher error = %v", name, err)
		}
		if got := gjson.GetBytes(patched, "top_p").Float(); got != 0.99 {
			t.Fatalf("%s patched top_p = %v, want 0.99", name, got)
		}
		if !gjson.GetBytes(patched, "stop").IsArray() || len(gjson.GetBytes(patched, "stop").Array()) != 1 || gjson.GetBytes(patched, "stop.0").String() != "END" {
			t.Fatalf("%s patched stop = %s, want [\"END\"]", name, gjson.GetBytes(patched, "stop").Raw)
		}
		if got := gjson.GetBytes(patched, "messages.0.role").String(); got != "system" {
			t.Fatalf("%s patched developer role = %q, want %q", name, got, "system")
		}
		if got := gjson.GetBytes(patched, "max_tokens").Int(); got != 128 {
			t.Fatalf("%s patched max_tokens = %d, want 128", name, got)
		}
		if gjson.GetBytes(patched, "max_completion_tokens").Exists() {
			t.Fatalf("%s should delete max_completion_tokens", name)
		}
		if got := gjson.GetBytes(patched, "messages.0.content.0.image_url.url").String(); got != "Zm9vYmFy" {
			t.Fatalf("%s patched image url = %q, want %q", name, got, "Zm9vYmFy")
		}
	}
}

func TestZhipuCompatibleProviderPreset_PatchMessagesBodyPassthrough(t *testing.T) {
	preset := zhipuCompatibleProviderPreset()
	if preset.PatchMessagesBody == nil {
		t.Fatal("PatchMessagesBody should not be nil")
	}

	body := []byte(`{
		"model":"glm-4.6v",
		"max_tokens":128,
		"messages":[
			{
				"role":"user",
				"content":[
					{"type":"text","text":"hello"},
					{
						"type":"image",
						"source":{
							"type":"base64",
							"media_type":"image/png",
							"data":"Zm9vYmFy"
						}
					}
				]
			}
		]
	}`)

	patched, err := preset.PatchMessagesBody(body, nil, "glm-4.6v")
	if err != nil {
		t.Fatalf("PatchMessagesBody() error = %v", err)
	}
	if string(patched) != string(body) {
		t.Fatalf("PatchMessagesBody() should keep native anthropic payload unchanged, got %s", string(patched))
	}
}
