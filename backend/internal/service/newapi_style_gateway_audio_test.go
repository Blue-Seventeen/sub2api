package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"math"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

func TestNewAPIStyleAudioForwardMapsOpenAIAndZhipuPaths(t *testing.T) {
	tests := []struct {
		name        string
		platform    string
		inboundPath string
		wantPath    string
		wantErr     error
	}{
		{
			name:        "openai v1 speech stays on openai path",
			platform:    PlatformOpenAI,
			inboundPath: "/v1/audio/speech",
			wantPath:    "/v1/audio/speech",
		},
		{
			name:        "openai root audio alias maps to v1 path",
			platform:    PlatformOpenAI,
			inboundPath: "/audio/transcriptions",
			wantPath:    "/v1/audio/transcriptions",
		},
		{
			name:        "zhipu unified transcription maps to glm official path",
			platform:    PlatformZhipu,
			inboundPath: "/v1/audio/transcriptions",
			wantPath:    zhipuAudioTranscriptionsPath,
		},
		{
			name:        "zhipu official speech alias stays on glm official path",
			platform:    PlatformZhipu,
			inboundPath: zhipuAudioSpeechPath,
			wantPath:    zhipuAudioSpeechPath,
		},
		{
			name:        "openai rejects glm official alias",
			platform:    PlatformOpenAI,
			inboundPath: zhipuAudioSpeechPath,
			wantErr:     ErrNewAPIStyleUnsupportedCapability,
		},
		{
			name:        "zhipu rejects unsupported audio subpath",
			platform:    PlatformZhipu,
			inboundPath: "/v1/audio/translations",
			wantErr:     ErrNewAPIStyleUnsupportedCapability,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			upstream := &httpUpstreamRecorder{resp: newAPIStyleAudioResponse("application/json", `{"ok":true}`)}
			svc := &NewAPIStyleGatewayService{httpUpstream: upstream}
			account := newAPIStyleAudioAccount(tt.platform, nil)

			_, endpoint, err := svc.Forward(context.Background(), newAPIStyleTestContext(), account, NewAPIStyleForwardOptions{
				Route:       NewAPIStyleRouteAudio,
				RequestBody: []byte(`{"model":"audio-model","input":"hello","voice":"alloy","response_format":"wav"}`),
				InboundPath: tt.inboundPath,
				ContentType: "application/json",
				HeaderSource: http.Header{
					"Authorization": []string{"Bearer client-token"},
					"Accept":        []string{"audio/wav"},
				},
			})

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("Forward() error = %v, want %v", err, tt.wantErr)
				}
				if upstream.lastReq != nil {
					t.Fatalf("upstream request should not be sent for unsupported path")
				}
				return
			}
			if err != nil {
				t.Fatalf("Forward() error = %v", err)
			}
			if endpoint != tt.wantPath {
				t.Fatalf("upstream endpoint = %q, want %q", endpoint, tt.wantPath)
			}
			if upstream.lastReq == nil {
				t.Fatalf("upstream request was not sent")
			}
			if got := upstream.lastReq.URL.Path; got != tt.wantPath {
				t.Fatalf("upstream path = %q, want %q", got, tt.wantPath)
			}
			wantAuth := "Bearer account-token"
			if tt.platform == PlatformZhipu {
				wantAuth = "Bearer zhipu-token"
			}
			if got := upstream.lastReq.Header.Get("Authorization"); got != wantAuth {
				t.Fatalf("authorization = %q, want %q", got, wantAuth)
			}
			if got := upstream.lastReq.Header.Get("Accept"); got != "audio/wav" {
				t.Fatalf("accept = %q, want client Accept", got)
			}
		})
	}
}

func TestNewAPIStyleAudioForwardPreservesTTSJSONBodyWithoutMapping(t *testing.T) {
	body := []byte(`{"model":"glm-4-voice","input":"hello","voice":"tongtong","response_format":"wav","speed":1.1}`)
	upstream := &httpUpstreamRecorder{resp: newAPIStyleAudioResponse("audio/wav", "RIFFxxxxWAVEaudio")}
	svc := &NewAPIStyleGatewayService{httpUpstream: upstream}

	_, _, err := svc.Forward(context.Background(), newAPIStyleTestContext(), newAPIStyleAudioAccount(PlatformZhipu, nil), NewAPIStyleForwardOptions{
		Route:       NewAPIStyleRouteAudio,
		RequestBody: body,
		InboundPath: "/v1/audio/speech",
		ContentType: "application/json",
	})
	if err != nil {
		t.Fatalf("Forward() error = %v", err)
	}
	if got := upstream.lastReq.URL.Path; got != zhipuAudioSpeechPath {
		t.Fatalf("upstream path = %q, want %q", got, zhipuAudioSpeechPath)
	}
	if !bytes.Equal(upstream.lastBody, body) {
		t.Fatalf("forwarded body changed:\n got %s\nwant %s", string(upstream.lastBody), string(body))
	}
}

func TestNewAPIStyleAudioMultipartModelExtractionAndRewrite(t *testing.T) {
	body, contentType := buildNewAPIStyleAudioMultipart(t, "glm-asr", []byte("fake-audio-bytes"))
	upstream := &httpUpstreamRecorder{resp: newAPIStyleAudioResponse("application/json", `{"text":"ok"}`)}
	svc := &NewAPIStyleGatewayService{httpUpstream: upstream}
	account := newAPIStyleAudioAccount(PlatformZhipu, map[string]any{"glm-asr": "glm-asr-upstream"})

	result, endpoint, err := svc.Forward(context.Background(), newAPIStyleTestContext(), account, NewAPIStyleForwardOptions{
		Route:       NewAPIStyleRouteAudio,
		RequestBody: body,
		InboundPath: "/v1/audio/transcriptions",
		ContentType: contentType,
		HeaderSource: http.Header{
			"Authorization": []string{"Bearer client-token"},
		},
	})
	if err != nil {
		t.Fatalf("Forward() error = %v", err)
	}
	if endpoint != zhipuAudioTranscriptionsPath {
		t.Fatalf("upstream endpoint = %q, want %q", endpoint, zhipuAudioTranscriptionsPath)
	}
	if result == nil || result.Model != "glm-asr-upstream" {
		t.Fatalf("result model = %#v, want mapped model", result)
	}
	if got := upstream.lastReq.Header.Get("Authorization"); got != "Bearer zhipu-token" {
		t.Fatalf("authorization = %q, want zhipu token", got)
	}
	upstreamContentType := upstream.lastReq.Header.Get("Content-Type")
	if !isMultipartFormData(upstreamContentType) {
		t.Fatalf("upstream content-type = %q, want multipart/form-data", upstreamContentType)
	}

	model, fileBytes := readNewAPIStyleAudioMultipart(t, upstream.lastBody, upstreamContentType)
	if model != "glm-asr-upstream" {
		t.Fatalf("multipart model = %q, want mapped model", model)
	}
	if string(fileBytes) != "fake-audio-bytes" {
		t.Fatalf("multipart file bytes = %q, want original file bytes", string(fileBytes))
	}
}

func TestExtractNewAPIStyleModelReadsMultipartFormField(t *testing.T) {
	body, contentType := buildNewAPIStyleAudioMultipart(t, "glm-asr", []byte("audio"))

	if got := ExtractNewAPIStyleModel(body, contentType); got != "glm-asr" {
		t.Fatalf("ExtractNewAPIStyleModel() = %q, want glm-asr", got)
	}
}

func TestNewAPIStyleAudioTokenChannelPricingWinsOverRequestGuardrail(t *testing.T) {
	groupID := int64(2)
	inputPrice := 0.000018
	svc := newAudioPricingGatewayService(t, groupID, PlatformZhipu, ChannelModelPricing{
		Platform:    PlatformZhipu,
		Models:      []string{"glm-asr-2512"},
		BillingMode: BillingModeToken,
		InputPrice:  &inputPrice,
	})

	cost := svc.calculateRecordUsageCost(context.Background(), &ForwardResult{
		Model:            "glm-asr-2512",
		Usage:            ClaudeUsage{InputTokens: 119, OutputTokens: 11},
		RequestCount:     1,
		BillableUnitType: BillableUnitTypeRequest,
	}, &APIKey{GroupID: &groupID}, "glm-asr-2512", 1, &recordUsageOpts{})

	if cost == nil {
		t.Fatalf("cost is nil")
	}
	if cost.BillingMode != string(BillingModeToken) {
		t.Fatalf("billing mode = %q, want token", cost.BillingMode)
	}
	want := float64(119) * inputPrice
	if math.Abs(cost.TotalCost-want) > 1e-12 {
		t.Fatalf("total cost = %.12f, want %.12f", cost.TotalCost, want)
	}
}

func TestNewAPIStyleAudioPerRequestChannelPricingStillUsesRequestCount(t *testing.T) {
	groupID := int64(2)
	perRequestPrice := 0.02
	svc := newAudioPricingGatewayService(t, groupID, PlatformZhipu, ChannelModelPricing{
		Platform:        PlatformZhipu,
		Models:          []string{"glm-tts"},
		BillingMode:     BillingModePerRequest,
		PerRequestPrice: &perRequestPrice,
	})

	cost := svc.calculateRecordUsageCost(context.Background(), &ForwardResult{
		Model:            "glm-tts",
		Usage:            ClaudeUsage{InputTokens: 32},
		RequestCount:     1,
		BillableUnitType: BillableUnitTypeRequest,
	}, &APIKey{GroupID: &groupID}, "glm-tts", 1, &recordUsageOpts{})

	if cost == nil {
		t.Fatalf("cost is nil")
	}
	if cost.BillingMode != string(BillingModePerRequest) {
		t.Fatalf("billing mode = %q, want per_request", cost.BillingMode)
	}
	if math.Abs(cost.TotalCost-perRequestPrice) > 1e-12 {
		t.Fatalf("total cost = %.12f, want %.12f", cost.TotalCost, perRequestPrice)
	}
}

func TestNewAPIStyleAudioTokenChannelPricingWithoutUsageStaysZero(t *testing.T) {
	groupID := int64(2)
	inputPrice := 0.000018
	svc := newAudioPricingGatewayService(t, groupID, PlatformZhipu, ChannelModelPricing{
		Platform:    PlatformZhipu,
		Models:      []string{"glm-asr-2512", "glm-tts"},
		BillingMode: BillingModeToken,
		InputPrice:  &inputPrice,
	})

	for _, model := range []string{"glm-asr-2512", "glm-tts"} {
		model := model
		t.Run(model, func(t *testing.T) {
			cost := svc.calculateRecordUsageCost(context.Background(), &ForwardResult{
				Model:            model,
				RequestCount:     1,
				BillableUnitType: BillableUnitTypeRequest,
			}, &APIKey{GroupID: &groupID}, model, 1, &recordUsageOpts{})

			if cost == nil {
				t.Fatalf("cost is nil")
			}
			if cost.BillingMode != string(BillingModeToken) {
				t.Fatalf("billing mode = %q, want token", cost.BillingMode)
			}
			if cost.TotalCost != 0 {
				t.Fatalf("total cost = %.12f, want 0", cost.TotalCost)
			}
			if cost.ActualCost != 0 {
				t.Fatalf("actual cost = %.12f, want 0", cost.ActualCost)
			}
		})
	}
}

func newAPIStyleAudioAccount(platform string, modelMapping map[string]any) *Account {
	credentials := map[string]any{
		"api_key":  "account-token",
		"base_url": "http://upstream.example",
	}
	if modelMapping != nil {
		credentials["model_mapping"] = modelMapping
	}
	if platform == PlatformZhipu {
		credentials["token"] = "zhipu-token"
	}
	return &Account{
		ID:          7,
		Name:        "audio-account",
		Platform:    platform,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Credentials: credentials,
		Extra:       map[string]any{AccountExtraNewAPIStyleInterfaceEnabled: true},
	}
}

func newAudioPricingGatewayService(t *testing.T, groupID int64, platform string, pricing ChannelModelPricing) *GatewayService {
	t.Helper()
	cfg := &config.Config{}
	billing := NewBillingService(cfg, nil)
	channelSvc := &ChannelService{}
	channel := Channel{
		ID:                 42,
		Status:             StatusActive,
		GroupIDs:           []int64{groupID},
		BillingModelSource: BillingModelSourceRequested,
		ModelPricing:       []ChannelModelPricing{pricing},
	}
	channelSvc.cache.Store(populateChannelCache([]Channel{channel}, map[int64]string{groupID: platform}))
	return &GatewayService{
		billingService: billing,
		channelService: channelSvc,
		resolver:       NewModelPricingResolver(channelSvc, billing),
	}
}

func newAPIStyleAudioResponse(contentType string, body string) *http.Response {
	header := make(http.Header)
	if strings.TrimSpace(contentType) != "" {
		header.Set("Content-Type", contentType)
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func buildNewAPIStyleAudioMultipart(t *testing.T, model string, fileBytes []byte) ([]byte, string) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("model", model); err != nil {
		t.Fatalf("write model field: %v", err)
	}
	fileWriter, err := writer.CreateFormFile("file", "sample.wav")
	if err != nil {
		t.Fatalf("create file field: %v", err)
	}
	if _, err := fileWriter.Write(fileBytes); err != nil {
		t.Fatalf("write file field: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	return body.Bytes(), writer.FormDataContentType()
}

func readNewAPIStyleAudioMultipart(t *testing.T, body []byte, contentType string) (string, []byte) {
	t.Helper()
	_, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		t.Fatalf("parse content-type: %v", err)
	}
	reader := multipart.NewReader(bytes.NewReader(body), params["boundary"])
	var model string
	var fileBytes []byte
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read multipart part: %v", err)
		}
		data, err := io.ReadAll(part)
		_ = part.Close()
		if err != nil {
			t.Fatalf("read part body: %v", err)
		}
		switch part.FormName() {
		case "model":
			model = strings.TrimSpace(string(data))
		case "file":
			fileBytes = data
		}
	}
	return model, fileBytes
}
