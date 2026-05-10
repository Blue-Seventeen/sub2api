package service

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
)

type accountProbeHTTPUpstream struct {
	requests []*http.Request
	bodies   [][]byte
	resp     *http.Response
	err      error
}

func (u *accountProbeHTTPUpstream) Do(req *http.Request, proxyURL string, accountID int64, accountConcurrency int) (*http.Response, error) {
	return u.DoWithTLS(req, proxyURL, accountID, accountConcurrency, nil)
}

func (u *accountProbeHTTPUpstream) DoWithTLS(req *http.Request, _ string, _ int64, _ int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	u.requests = append(u.requests, req)
	if req.Body != nil {
		body, _ := io.ReadAll(req.Body)
		u.bodies = append(u.bodies, body)
		req.Body = io.NopCloser(bytes.NewReader(body))
	}
	if u.err != nil {
		return nil, u.err
	}
	if u.resp == nil {
		return nil, fmt.Errorf("missing mock response")
	}
	return u.resp, nil
}

func accountProbeTestContext() (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/1/test", nil)
	return c, rec
}

func decodeAudioDataURL(t *testing.T, audioURL string) []byte {
	t.Helper()
	prefixEnd := strings.Index(audioURL, "base64,")
	if prefixEnd < 0 {
		t.Fatalf("audio URL is not base64 data URL: %s", audioURL)
	}
	decoded, err := base64.StdEncoding.DecodeString(audioURL[prefixEnd+len("base64,"):])
	if err != nil {
		t.Fatalf("decode audio data URL: %v", err)
	}
	return decoded
}

func findWAVChunk(t *testing.T, body []byte, chunkID string) ([]byte, int) {
	t.Helper()
	if len(body) < 12 || !bytes.Equal(body[:4], []byte("RIFF")) || !bytes.Equal(body[8:12], []byte("WAVE")) {
		t.Fatalf("not a WAV body: %q", body[:min(len(body), 12)])
	}
	for offset := 12; offset+8 <= len(body); {
		size := int(binary.LittleEndian.Uint32(body[offset+4 : offset+8]))
		dataStart := offset + 8
		dataEnd := dataStart + size
		if dataEnd > len(body) {
			t.Fatalf("chunk %q size %d exceeds body length %d", body[offset:offset+4], size, len(body))
		}
		if string(body[offset:offset+4]) == chunkID {
			return body[dataStart:dataEnd], offset
		}
		offset = dataEnd
		if size%2 == 1 {
			offset++
		}
	}
	t.Fatalf("missing WAV chunk %q", chunkID)
	return nil, 0
}

func buildZeroSizedDataWAV(pcm []byte) []byte {
	fmtChunk := make([]byte, 16)
	binary.LittleEndian.PutUint16(fmtChunk[0:2], 1)
	binary.LittleEndian.PutUint16(fmtChunk[2:4], 1)
	binary.LittleEndian.PutUint32(fmtChunk[4:8], 24000)
	binary.LittleEndian.PutUint32(fmtChunk[8:12], 48000)
	binary.LittleEndian.PutUint16(fmtChunk[12:14], 2)
	binary.LittleEndian.PutUint16(fmtChunk[14:16], 16)

	var out bytes.Buffer
	out.WriteString("RIFF")
	_ = binary.Write(&out, binary.LittleEndian, uint32(36))
	out.WriteString("WAVE")
	out.WriteString("fmt ")
	_ = binary.Write(&out, binary.LittleEndian, uint32(len(fmtChunk)))
	out.Write(fmtChunk)
	out.WriteString("data")
	_ = binary.Write(&out, binary.LittleEndian, uint32(0))
	out.Write(pcm)
	return out.Bytes()
}

func TestNormalizeAccountTestType(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"", AccountTestTypeAuto},
		{" AUTO ", AccountTestTypeAuto},
		{"text", AccountTestTypeText},
		{"IMAGE", AccountTestTypeImage},
		{"asr", AccountTestTypeASR},
		{"tts", AccountTestTypeTTS},
		{"video", AccountTestTypeVideo},
		{"task", AccountTestTypeTask},
		{"embedding", AccountTestTypeEmbedding},
		{"rerank", AccountTestTypeRerank},
		{"unknown", "unknown"},
	}
	for _, tt := range tests {
		if got := normalizeAccountTestType(tt.in); got != tt.want {
			t.Fatalf("normalizeAccountTestType(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestAccountTestExplicitUnsupportedDoesNotCallUpstream(t *testing.T) {
	c, rec := accountProbeTestContext()
	upstream := &accountProbeHTTPUpstream{}
	svc := &AccountTestService{httpUpstream: upstream}
	account := &Account{
		ID:          1,
		Platform:    PlatformAnthropic,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Credentials: map[string]any{"api_key": "sk-test", "base_url": "http://upstream.example"},
	}

	err := svc.testAccountConnectionByType(c, account, "gpt-5.4", "", AccountTestModeDefault, AccountTestTypeASR, AccountTestOptions{})
	if err == nil {
		t.Fatal("expected unsupported ASR probe to fail")
	}
	if len(upstream.requests) != 0 {
		t.Fatalf("unexpected upstream calls: %d", len(upstream.requests))
	}
	if !strings.Contains(rec.Body.String(), "requires an OpenAI or GLM/Zhipu API key account") {
		t.Fatalf("expected unsupported SSE error, got %s", rec.Body.String())
	}
}

func TestAccountTestUnknownTypeDoesNotCallUpstream(t *testing.T) {
	c, rec := accountProbeTestContext()
	upstream := &accountProbeHTTPUpstream{}
	svc := &AccountTestService{httpUpstream: upstream}
	account := &Account{
		ID:          1,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Credentials: map[string]any{"api_key": "sk-test", "base_url": "http://upstream.example"},
		Extra:       map[string]any{AccountExtraNewAPIStyleInterfaceEnabled: true},
	}

	err := svc.testAccountConnectionByType(c, account, "gpt-5.4", "", AccountTestModeDefault, normalizeAccountTestType("unknown"), AccountTestOptions{})
	if err == nil {
		t.Fatal("expected unknown probe type to fail")
	}
	if len(upstream.requests) != 0 {
		t.Fatalf("unexpected upstream calls: %d", len(upstream.requests))
	}
	if !strings.Contains(rec.Body.String(), "Unsupported test type: unknown") {
		t.Fatalf("expected unknown type SSE error, got %s", rec.Body.String())
	}
}

func TestAccountTestExplicitTypeRequiresModel(t *testing.T) {
	c, rec := accountProbeTestContext()
	upstream := &accountProbeHTTPUpstream{}
	svc := &AccountTestService{httpUpstream: upstream}
	account := &Account{
		ID:          1,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Credentials: map[string]any{"api_key": "sk-test", "base_url": "http://upstream.example"},
		Extra:       map[string]any{AccountExtraNewAPIStyleInterfaceEnabled: true},
	}

	err := svc.testAccountConnectionByType(c, account, "", "", AccountTestModeDefault, AccountTestTypeASR, AccountTestOptions{})
	if err == nil {
		t.Fatal("expected empty model explicit probe type to fail")
	}
	if len(upstream.requests) != 0 {
		t.Fatalf("unexpected upstream calls: %d", len(upstream.requests))
	}
	if !strings.Contains(rec.Body.String(), "model_id is required for asr account tests") {
		t.Fatalf("expected missing model SSE error, got %s", rec.Body.String())
	}
}

func TestAccountTestASRProbeBuildsMultipartRequest(t *testing.T) {
	c, rec := accountProbeTestContext()
	upstream := &accountProbeHTTPUpstream{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"text":"hi"}`)),
		},
	}
	svc := &AccountTestService{httpUpstream: upstream}
	account := &Account{
		ID:          2,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Credentials: map[string]any{"api_key": "sk-test", "base_url": "http://upstream.example"},
		Extra:       map[string]any{AccountExtraNewAPIStyleInterfaceEnabled: true},
	}

	if err := svc.testAudioTranscriptionProbe(c, account, "whisper-1"); err != nil {
		t.Fatalf("ASR probe failed: %v", err)
	}
	if len(upstream.requests) != 1 {
		t.Fatalf("upstream calls = %d, want 1", len(upstream.requests))
	}
	req := upstream.requests[0]
	if req.URL.Path != "/v1/audio/transcriptions" {
		t.Fatalf("path = %s, want /v1/audio/transcriptions", req.URL.Path)
	}
	if !strings.Contains(req.Header.Get("Content-Type"), "multipart/form-data") {
		t.Fatalf("content type = %q, want multipart/form-data", req.Header.Get("Content-Type"))
	}
	bodyBytes := upstream.bodies[0]
	body := string(bodyBytes)
	if !strings.Contains(body, "whisper-1") || !strings.Contains(body, "sub2api-test.mp3") || !bytes.Contains(bodyBytes, []byte("ID3")) {
		t.Fatalf("expected ASR multipart body with embedded MP3, got len=%d", len(bodyBytes))
	}
	if !strings.Contains(rec.Body.String(), `"success":true`) {
		t.Fatalf("expected success SSE, got %s", rec.Body.String())
	}
}

func TestAccountTestTTSProbeBuildsNewAPIRequest(t *testing.T) {
	c, rec := accountProbeTestContext()
	header := make(http.Header)
	header.Set("Content-Type", "audio/mpeg")
	upstream := &accountProbeHTTPUpstream{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Header:     header,
			Body:       io.NopCloser(strings.NewReader("audio-bytes")),
		},
	}
	svc := &AccountTestService{httpUpstream: upstream}
	account := &Account{
		ID:          3,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Credentials: map[string]any{"api_key": "sk-test", "base_url": "http://upstream.example"},
		Extra:       map[string]any{AccountExtraNewAPIStyleInterfaceEnabled: true},
	}

	if err := svc.testAudioSpeechProbe(c, account, "gpt-4o-mini-tts", "hello", AccountTestOptions{Voice: "nova"}); err != nil {
		t.Fatalf("TTS probe failed: %v", err)
	}
	if len(upstream.requests) != 1 {
		t.Fatalf("upstream calls = %d, want 1", len(upstream.requests))
	}
	req := upstream.requests[0]
	if req.URL.Path != "/v1/audio/speech" {
		t.Fatalf("path = %s, want /v1/audio/speech", req.URL.Path)
	}
	body := string(upstream.bodies[0])
	if !strings.Contains(body, "gpt-4o-mini-tts") || !strings.Contains(body, "hello") || !strings.Contains(body, `"voice":"nova"`) {
		t.Fatalf("expected TTS json body, got %s", body)
	}
	if !strings.Contains(rec.Body.String(), `"type":"audio"`) || !strings.Contains(rec.Body.String(), `"audio_url":"data:audio/mpeg;base64,`) {
		t.Fatalf("expected audio SSE preview event, got %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"success":true`) {
		t.Fatalf("expected success SSE, got %s", rec.Body.String())
	}
}

func TestAccountTestASRProbeBuildsZhipuRequest(t *testing.T) {
	c, rec := accountProbeTestContext()
	upstream := &accountProbeHTTPUpstream{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"text":"hello","usage":{"total_tokens":12}}`)),
		},
	}
	svc := &AccountTestService{httpUpstream: upstream}
	account := &Account{
		ID:          6,
		Platform:    PlatformZhipu,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Credentials: map[string]any{"api_key": "glm-test", "base_url": "http://upstream.example"},
	}

	if err := svc.testAudioTranscriptionProbe(c, account, "GLM-ASR-2512"); err != nil {
		t.Fatalf("Zhipu ASR probe failed: %v", err)
	}
	if len(upstream.requests) != 1 {
		t.Fatalf("upstream calls = %d, want 1", len(upstream.requests))
	}
	req := upstream.requests[0]
	if req.URL.Path != "/api/paas/v4/audio/transcriptions" {
		t.Fatalf("path = %s, want /api/paas/v4/audio/transcriptions", req.URL.Path)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer glm-test" {
		t.Fatalf("authorization header = %q", got)
	}
	bodyBytes := upstream.bodies[0]
	body := string(bodyBytes)
	if !strings.Contains(body, "GLM-ASR-2512") || !strings.Contains(body, "sub2api-test.mp3") || !bytes.Contains(bodyBytes, []byte("ID3")) {
		t.Fatalf("expected Zhipu ASR multipart body with embedded MP3, got len=%d", len(bodyBytes))
	}
	if !strings.Contains(rec.Body.String(), `"success":true`) {
		t.Fatalf("expected success SSE, got %s", rec.Body.String())
	}
}

func TestAccountTestTTSProbeBuildsZhipuRequest(t *testing.T) {
	c, rec := accountProbeTestContext()
	header := make(http.Header)
	header.Set("Content-Type", "audio/wav")
	upstream := &accountProbeHTTPUpstream{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Header:     header,
			Body:       io.NopCloser(bytes.NewReader([]byte("RIFFxxxxWAVEaudio"))),
		},
	}
	svc := &AccountTestService{httpUpstream: upstream}
	account := &Account{
		ID:          7,
		Platform:    PlatformZhipu,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Credentials: map[string]any{"api_key": "glm-test", "base_url": "http://upstream.example"},
	}

	if err := svc.testAudioSpeechProbe(c, account, "GLM-TTS", "hello", AccountTestOptions{Voice: "custom-voice"}); err != nil {
		t.Fatalf("Zhipu TTS probe failed: %v", err)
	}
	if len(upstream.requests) != 1 {
		t.Fatalf("upstream calls = %d, want 1", len(upstream.requests))
	}
	req := upstream.requests[0]
	if req.URL.Path != "/api/paas/v4/audio/speech" {
		t.Fatalf("path = %s, want /api/paas/v4/audio/speech", req.URL.Path)
	}
	body := string(upstream.bodies[0])
	for _, want := range []string{`"model":"GLM-TTS"`, `"input":"hello"`, `"voice":"custom-voice"`, `"response_format":"wav"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected %s in Zhipu TTS json body, got %s", want, body)
		}
	}
	if !strings.Contains(rec.Body.String(), `"type":"audio"`) || !strings.Contains(rec.Body.String(), `"audio_url":"data:audio/wav;base64,`) {
		t.Fatalf("expected Zhipu audio SSE preview event, got %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"success":true`) {
		t.Fatalf("expected success SSE, got %s", rec.Body.String())
	}
}

func TestAccountTestAudioDataURLRepairsZeroSizedWAVDataChunk(t *testing.T) {
	pcm := []byte{0x00, 0x00, 0x01, 0x00, 0xff, 0x00, 0x00, 0x00}
	audioURL, mimeType, previewBytes := accountTestAudioDataURL(&Account{Platform: PlatformZhipu}, "audio/wav", buildZeroSizedDataWAV(pcm))
	if mimeType != "audio/wav" {
		t.Fatalf("mime type = %s, want audio/wav", mimeType)
	}
	decoded := decodeAudioDataURL(t, audioURL)
	if previewBytes != len(decoded) {
		t.Fatalf("preview bytes = %d, decoded len = %d", previewBytes, len(decoded))
	}
	if got := binary.LittleEndian.Uint32(decoded[4:8]); got != uint32(len(decoded)-8) {
		t.Fatalf("RIFF size = %d, want %d", got, len(decoded)-8)
	}
	data, offset := findWAVChunk(t, decoded, "data")
	if got := binary.LittleEndian.Uint32(decoded[offset+4 : offset+8]); got != uint32(len(pcm)) {
		t.Fatalf("data chunk size = %d, want %d", got, len(pcm))
	}
	if !bytes.Equal(data, pcm) {
		t.Fatalf("data chunk = %v, want %v", data, pcm)
	}
}

func TestAccountTestAudioDataURLWrapsZhipuRawPCMAsWAV(t *testing.T) {
	pcm := []byte{0x00, 0x00, 0x10, 0x00, 0x20, 0x00, 0x30, 0x00}
	audioURL, mimeType, _ := accountTestAudioDataURL(&Account{Platform: PlatformZhipu}, "audio/wav", pcm)
	if mimeType != "audio/wav" {
		t.Fatalf("mime type = %s, want audio/wav", mimeType)
	}
	decoded := decodeAudioDataURL(t, audioURL)
	fmtChunk, _ := findWAVChunk(t, decoded, "fmt ")
	if got := binary.LittleEndian.Uint32(fmtChunk[4:8]); got != 24000 {
		t.Fatalf("sample rate = %d, want 24000", got)
	}
	if got := binary.LittleEndian.Uint16(fmtChunk[14:16]); got != 16 {
		t.Fatalf("bits per sample = %d, want 16", got)
	}
	data, _ := findWAVChunk(t, decoded, "data")
	if !bytes.Equal(data, pcm) {
		t.Fatalf("data chunk = %v, want %v", data, pcm)
	}
}

func TestAccountTestVideoUnderstandingProbeBuildsChatRequest(t *testing.T) {
	c, rec := accountProbeTestContext()
	upstream := &accountProbeHTTPUpstream{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"视频里有人喝水并说话。"}}]}`)),
		},
	}
	svc := &AccountTestService{httpUpstream: upstream}
	account := &Account{
		ID:          8,
		Platform:    PlatformZhipu,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Credentials: map[string]any{"api_key": "glm-test", "base_url": "http://upstream.example"},
	}

	if err := svc.testVideoUnderstandingProbe(c, account, "glm-4v", "描述这个视频"); err != nil {
		t.Fatalf("Video understanding probe failed: %v", err)
	}
	if len(upstream.requests) != 1 {
		t.Fatalf("upstream calls = %d, want 1", len(upstream.requests))
	}
	req := upstream.requests[0]
	if req.URL.Path != "/api/paas/v4/chat/completions" {
		t.Fatalf("path = %s, want /api/paas/v4/chat/completions", req.URL.Path)
	}
	bodyBytes := upstream.bodies[0]
	for _, want := range [][]byte{
		[]byte(`"model":"glm-4v"`),
		[]byte(`"type":"video_url"`),
		[]byte(`data:video/mp4;base64,`),
		[]byte(`"stream":false`),
	} {
		if !bytes.Contains(bodyBytes, want) {
			t.Fatalf("expected %q in video understanding body, got len=%d", want, len(bodyBytes))
		}
	}
	if bytes.Contains(bodyBytes, []byte("/v1/video/generations")) {
		t.Fatalf("video understanding probe must not use video generation body")
	}
	if !strings.Contains(rec.Body.String(), `"success":true`) {
		t.Fatalf("expected success SSE, got %s", rec.Body.String())
	}
}

func TestAccountTestTTSProbeRejectsJSONResponse(t *testing.T) {
	c, rec := accountProbeTestContext()
	header := make(http.Header)
	header.Set("Content-Type", "application/json")
	upstream := &accountProbeHTTPUpstream{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Header:     header,
			Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
		},
	}
	svc := &AccountTestService{httpUpstream: upstream}
	account := &Account{
		ID:          3,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Credentials: map[string]any{"api_key": "sk-test", "base_url": "http://upstream.example"},
		Extra:       map[string]any{AccountExtraNewAPIStyleInterfaceEnabled: true},
	}

	if err := svc.testAudioSpeechProbe(c, account, "gpt-4o-mini-tts", "hello", AccountTestOptions{}); err == nil {
		t.Fatal("expected TTS JSON response to fail")
	}
	if len(upstream.requests) != 1 {
		t.Fatalf("upstream calls = %d, want 1", len(upstream.requests))
	}
	if !strings.Contains(rec.Body.String(), "non-audio content type") {
		t.Fatalf("expected non-audio SSE error, got %s", rec.Body.String())
	}
}

func TestAccountTestTTSProbeRejectsProblemJSONAndEmptyContentTypeText(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		body        string
	}{
		{
			name:        "problem_json",
			contentType: "application/problem+json",
			body:        `{"error":"bad voice"}`,
		},
		{
			name:        "empty_content_type_text",
			contentType: "",
			body:        `not-audio`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, rec := accountProbeTestContext()
			header := make(http.Header)
			if tt.contentType != "" {
				header.Set("Content-Type", tt.contentType)
			}
			upstream := &accountProbeHTTPUpstream{
				resp: &http.Response{
					StatusCode: http.StatusOK,
					Header:     header,
					Body:       io.NopCloser(strings.NewReader(tt.body)),
				},
			}
			svc := &AccountTestService{httpUpstream: upstream}
			account := &Account{
				ID:          3,
				Platform:    PlatformOpenAI,
				Type:        AccountTypeAPIKey,
				Concurrency: 1,
				Credentials: map[string]any{"api_key": "sk-test", "base_url": "http://upstream.example"},
				Extra:       map[string]any{AccountExtraNewAPIStyleInterfaceEnabled: true},
			}

			if err := svc.testAudioSpeechProbe(c, account, "gpt-4o-mini-tts", "hello", AccountTestOptions{}); err == nil {
				t.Fatal("expected non-audio response to fail")
			}
			if !strings.Contains(rec.Body.String(), "non-audio content type") {
				t.Fatalf("expected non-audio SSE error, got %s", rec.Body.String())
			}
		})
	}
}

func TestAccountTestTTSProbeAllowsEmptyContentTypeWithAudioSignature(t *testing.T) {
	c, rec := accountProbeTestContext()
	upstream := &accountProbeHTTPUpstream{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewReader([]byte("ID3\x04audio"))),
		},
	}
	svc := &AccountTestService{httpUpstream: upstream}
	account := &Account{
		ID:          3,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Credentials: map[string]any{"api_key": "sk-test", "base_url": "http://upstream.example"},
		Extra:       map[string]any{AccountExtraNewAPIStyleInterfaceEnabled: true},
	}

	if err := svc.testAudioSpeechProbe(c, account, "gpt-4o-mini-tts", "hello", AccountTestOptions{}); err != nil {
		t.Fatalf("expected audio signature response to succeed: %v", err)
	}
	if !strings.Contains(rec.Body.String(), `"success":true`) {
		t.Fatalf("expected success SSE, got %s", rec.Body.String())
	}
}

func TestAccountTestEmbeddingProbeBuildsNewAPIRequest(t *testing.T) {
	c, rec := accountProbeTestContext()
	upstream := &accountProbeHTTPUpstream{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"data":[{"embedding":[0.1,0.2]}]}`)),
		},
	}
	svc := &AccountTestService{httpUpstream: upstream}
	account := &Account{
		ID:          2,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Credentials: map[string]any{"api_key": "sk-test", "base_url": "http://upstream.example"},
		Extra:       map[string]any{AccountExtraNewAPIStyleInterfaceEnabled: true},
	}

	err := svc.testOpenAIEmbeddingProbe(c, account, "gpt-5.4")
	if err != nil {
		t.Fatalf("embedding probe failed: %v", err)
	}
	if len(upstream.requests) != 1 {
		t.Fatalf("upstream calls = %d, want 1", len(upstream.requests))
	}
	req := upstream.requests[0]
	if req.Method != http.MethodPost {
		t.Fatalf("method = %s, want POST", req.Method)
	}
	if req.URL.Path != "/v1/embeddings" {
		t.Fatalf("path = %s, want /v1/embeddings", req.URL.Path)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer sk-test" {
		t.Fatalf("authorization header = %q", got)
	}
	if !strings.Contains(string(upstream.bodies[0]), "gpt-5.4") {
		t.Fatalf("expected selected embedding model in body, got %s", string(upstream.bodies[0]))
	}
	if !strings.Contains(rec.Body.String(), `"success":true`) {
		t.Fatalf("expected success SSE, got %s", rec.Body.String())
	}
}

func TestAccountTestRerankProbeBuildsSiliconFlowRequest(t *testing.T) {
	c, rec := accountProbeTestContext()
	upstream := &accountProbeHTTPUpstream{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"results":[{"index":0,"score":0.9}]}`)),
		},
	}
	svc := &AccountTestService{httpUpstream: upstream}
	account := &Account{
		ID:          4,
		Platform:    PlatformSiliconFlow,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Credentials: map[string]any{"api_key": "sk-test", "base_url": "http://upstream.example"},
	}

	if err := svc.testSiliconFlowNewAPIRerankProbe(c, account, "BAAI/bge-reranker-v2-m3"); err != nil {
		t.Fatalf("rerank probe failed: %v", err)
	}
	if len(upstream.requests) != 1 {
		t.Fatalf("upstream calls = %d, want 1", len(upstream.requests))
	}
	req := upstream.requests[0]
	if req.URL.Path != "/v1/rerank" {
		t.Fatalf("path = %s, want /v1/rerank", req.URL.Path)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer sk-test" {
		t.Fatalf("authorization header = %q", got)
	}
	if !strings.Contains(rec.Body.String(), `"success":true`) {
		t.Fatalf("expected success SSE, got %s", rec.Body.String())
	}
}

func TestAccountTestTaskProbeWithoutTaskIDFails(t *testing.T) {
	c, rec := accountProbeTestContext()
	upstream := &accountProbeHTTPUpstream{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"status":"queued"}`)),
		},
	}
	svc := &AccountTestService{httpUpstream: upstream}
	account := &Account{
		ID:          5,
		Platform:    PlatformSuno,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Credentials: map[string]any{"api_key": "sk-test", "base_url": "http://upstream.example"},
	}

	if err := svc.testNewAPITaskProbe(c, account, "suno_music", "cat song"); err == nil {
		t.Fatal("expected missing task id response to fail")
	}
	if len(upstream.requests) != 1 {
		t.Fatalf("upstream calls = %d, want submit only", len(upstream.requests))
	}
	if !strings.Contains(rec.Body.String(), "did not include a task id") {
		t.Fatalf("expected missing task id SSE error, got %s", rec.Body.String())
	}
}

func TestAccountTestTaskProbeCompletedSubmitSucceeds(t *testing.T) {
	c, rec := accountProbeTestContext()
	upstream := &accountProbeHTTPUpstream{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"task_id":"task-1","status":"completed"}`)),
		},
	}
	svc := &AccountTestService{httpUpstream: upstream}
	account := &Account{
		ID:          5,
		Platform:    PlatformSuno,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Credentials: map[string]any{"api_key": "sk-test", "base_url": "http://upstream.example"},
	}

	if err := svc.testNewAPITaskProbe(c, account, "suno_music", "cat song"); err != nil {
		t.Fatalf("task probe failed: %v", err)
	}
	if len(upstream.requests) != 1 {
		t.Fatalf("upstream calls = %d, want submit only", len(upstream.requests))
	}
	req := upstream.requests[0]
	if req.URL.Path != "/suno/submit/music" {
		t.Fatalf("path = %s, want /suno/submit/music", req.URL.Path)
	}
	if !strings.Contains(string(upstream.bodies[0]), "cat song") {
		t.Fatalf("expected task prompt in body, got %s", string(upstream.bodies[0]))
	}
	if !strings.Contains(rec.Body.String(), `"success":true`) {
		t.Fatalf("expected success SSE, got %s", rec.Body.String())
	}
}
