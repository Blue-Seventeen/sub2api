package service

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/Wei-Shaw/sub2api/internal/util/urlvalidator"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

const (
	accountTestProbeMaxBodyBytes = int64(4 << 20)
	accountTestTaskPollTimeout   = 60 * time.Second
	accountTestTaskPollInterval  = 5 * time.Second
)

//go:embed testdata/asr_probe_zh.mp3
var accountTestChineseASRProbeMP3 []byte

//go:embed testdata/video_probe_zh.mp4
var accountTestChineseVideoProbeMP4 []byte

func (s *AccountTestService) testAccountConnectionByType(c *gin.Context, account *Account, modelID string, prompt string, mode string, testType string, options AccountTestOptions) error {
	if strings.TrimSpace(modelID) == "" {
		return s.sendErrorAndEnd(c, fmt.Sprintf("model_id is required for %s account tests", testType))
	}

	switch testType {
	case AccountTestTypeText:
		return s.testTextAccountConnection(c, account, modelID, prompt, mode)
	case AccountTestTypeImage:
		return s.testImageAccountConnection(c, account, modelID, prompt)
	case AccountTestTypeASR:
		return s.testAudioTranscriptionProbe(c, account, modelID)
	case AccountTestTypeTTS:
		return s.testAudioSpeechProbe(c, account, modelID, prompt, options)
	case AccountTestTypeVideo:
		return s.testVideoUnderstandingProbe(c, account, modelID, prompt)
	case AccountTestTypeTask:
		return s.testNewAPITaskProbe(c, account, modelID, prompt)
	case AccountTestTypeEmbedding:
		return s.testOpenAIEmbeddingProbe(c, account, modelID)
	case AccountTestTypeRerank:
		return s.testSiliconFlowNewAPIRerankProbe(c, account, modelID)
	default:
		return s.sendErrorAndEnd(c, fmt.Sprintf("Unsupported test type: %s", testType))
	}
}

func (s *AccountTestService) testTextAccountConnection(c *gin.Context, account *Account, modelID string, prompt string, mode string) error {
	if account == nil {
		return s.sendErrorAndEnd(c, "Account not found")
	}
	switch {
	case account.IsOpenAI():
		return s.testOpenAIAccountConnection(c, account, modelID, prompt, normalizeAccountTestMode(mode), true)
	case account.IsGemini():
		return s.testGeminiAccountConnection(c, account, modelID, prompt, true)
	case account.Platform == PlatformAntigravity:
		return s.routeAntigravityTest(c, account, modelID, prompt)
	case account.Platform == PlatformSuno || account.Platform == PlatformKling || account.Platform == PlatformMidjourney:
		return s.sendUnsupportedTestType(c, AccountTestTypeText, account, "task-only platform")
	case account.IsCompatiblePlatform():
		return s.testCompatibleAccountConnection(c, account, modelID)
	default:
		return s.testClaudeAccountConnection(c, account, modelID)
	}
}

func (s *AccountTestService) testImageAccountConnection(c *gin.Context, account *Account, modelID string, prompt string) error {
	if account == nil {
		return s.sendErrorAndEnd(c, "Account not found")
	}
	ctx := c.Request.Context()
	switch {
	case account.Platform == PlatformAli:
		return s.testAliImageGenerationProbe(c, account, modelID, prompt)
	case account.IsOpenAI():
		testModelID := strings.TrimSpace(modelID)
		testModelID = account.GetMappedModel(testModelID)
		imagePrompt := strings.TrimSpace(prompt)
		if imagePrompt == "" {
			imagePrompt = defaultOpenAIImageTestPrompt
		}
		if account.Type == AccountTypeAPIKey {
			return s.testOpenAIImageAPIKey(c, ctx, account, testModelID, imagePrompt)
		}
		return s.testOpenAIImageOAuth(c, ctx, account, testModelID, imagePrompt)
	case account.IsGemini() || (account.Platform == PlatformAntigravity && account.Type == AccountTypeAPIKey):
		testModelID := strings.TrimSpace(modelID)
		return s.testGeminiImageAccountConnection(c, account, testModelID, prompt)
	default:
		return s.sendUnsupportedTestType(c, AccountTestTypeImage, account, "platform does not expose an image probe")
	}
}

func (s *AccountTestService) testAliImageGenerationProbe(c *gin.Context, account *Account, modelID string, prompt string) error {
	if account == nil || account.Type != AccountTypeAPIKey {
		return s.sendErrorAndEnd(c, fmt.Sprintf("test type %s requires a Qwen/DashScope API key account", AccountTestTypeImage))
	}
	model := strings.TrimSpace(modelID)
	imagePrompt := strings.TrimSpace(prompt)
	if imagePrompt == "" {
		imagePrompt = "A tiny orange cat sticker on a clean white background."
	}
	payload := map[string]any{
		"model": model,
		"input": map[string]any{
			"messages": []map[string]any{
				{
					"role": "user",
					"content": []map[string]any{
						{"text": imagePrompt},
					},
				},
			},
		},
		"parameters": map[string]any{
			"negative_prompt": "",
			"watermark":       false,
		},
	}
	body, _ := json.Marshal(payload)

	s.prepareAccountTestStream(c)
	s.sendEvent(c, TestEvent{Type: "test_start", Model: model, Data: map[string]any{"test_type": AccountTestTypeImage}})

	req, err := s.aliQwenMultimodalGenerationTestRequest(c.Request.Context(), account, body, false)
	if err != nil {
		return s.sendErrorAndEnd(c, err.Error())
	}
	resp, responseBody, err := s.executeAccountTestRequest(c.Request.Context(), account, req)
	if err != nil {
		return s.sendErrorAndEnd(c, fmt.Sprintf("Request failed: %s", err.Error()))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return s.sendErrorAndEnd(c, fmt.Sprintf("API returned %d: %s", resp.StatusCode, strings.TrimSpace(extractUpstreamErrorMessage(responseBody))))
	}
	imageURL := extractAliQwenGeneratedImageURL(responseBody)
	if imageURL == "" {
		return s.sendErrorAndEnd(c, "Qwen image probe response did not include an image URL")
	}
	s.sendEvent(c, TestEvent{
		Type:     "image",
		ImageURL: imageURL,
		Data: map[string]any{
			"prompt": imagePrompt,
			"url":    imageURL,
		},
	})
	s.sendEvent(c, TestEvent{Type: "content", Text: "Qwen image generation probe succeeded", Data: map[string]any{"url": imageURL}})
	s.sendEvent(c, TestEvent{Type: "test_complete", Success: true})
	return nil
}

func (s *AccountTestService) testAudioTranscriptionProbe(c *gin.Context, account *Account, modelID string) error {
	if account != nil && account.Platform == PlatformAli {
		return s.testAliAudioTranscriptionProbe(c, account, modelID)
	}
	if err := s.requireAudioProbeAccount(account, AccountTestTypeASR); err != nil {
		return s.sendErrorAndEnd(c, err.Error())
	}
	model := strings.TrimSpace(modelID)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("model", model)
	part, err := writer.CreateFormFile("file", "sub2api-test.mp3")
	if err != nil {
		return s.sendErrorAndEnd(c, "Failed to create ASR test audio")
	}
	if _, err := part.Write(accountTestASRProbeAudio()); err != nil {
		return s.sendErrorAndEnd(c, "Failed to write ASR test audio")
	}
	_ = writer.Close()

	return s.runNewAPIProbe(c, account, newAPIProbeRequest{
		TestType:    AccountTestTypeASR,
		Route:       NewAPIStyleRouteAudio,
		InboundPath: audioTranscriptionProbePath(account),
		Model:       model,
		Method:      http.MethodPost,
		Body:        body.Bytes(),
		ContentType: writer.FormDataContentType(),
		Validate: func(resp *http.Response, responseBody []byte) error {
			if len(responseBody) == 0 {
				return fmt.Errorf("ASR probe returned an empty body")
			}
			text := strings.TrimSpace(gjson.GetBytes(responseBody, "text").String())
			caption := "ASR transcription probe succeeded"
			if text != "" {
				caption = "ASR transcription: " + text
			}
			s.sendEvent(c, TestEvent{Type: "content", Text: caption, Data: map[string]any{"text": text}})
			return nil
		},
	})
}

func (s *AccountTestService) testAliAudioTranscriptionProbe(c *gin.Context, account *Account, modelID string) error {
	if account == nil || account.Type != AccountTypeAPIKey {
		return s.sendErrorAndEnd(c, fmt.Sprintf("test type %s requires an API key account", AccountTestTypeASR))
	}
	model := strings.TrimSpace(modelID)
	audioData := "data:audio/mpeg;base64," + base64.StdEncoding.EncodeToString(accountTestASRProbeAudio())
	payload := map[string]any{
		"model": model,
		"messages": []map[string]any{
			{
				"role": "user",
				"content": []map[string]any{
					{
						"type": "input_audio",
						"input_audio": map[string]any{
							"data":   audioData,
							"format": "mp3",
						},
					},
				},
			},
		},
		"stream": false,
	}
	body, _ := json.Marshal(payload)
	return s.runNewAPIProbe(c, account, newAPIProbeRequest{
		TestType:    AccountTestTypeASR,
		Route:       NewAPIStyleRouteChatCompletions,
		InboundPath: "/v1/chat/completions",
		Model:       model,
		Method:      http.MethodPost,
		Body:        body,
		ContentType: "application/json",
		Validate: func(resp *http.Response, responseBody []byte) error {
			text := strings.TrimSpace(gjson.GetBytes(responseBody, "choices.0.message.content").String())
			if text == "" {
				text = strings.TrimSpace(gjson.GetBytes(responseBody, "output.text").String())
			}
			if text == "" {
				return fmt.Errorf("ASR probe returned an empty transcription")
			}
			s.sendEvent(c, TestEvent{Type: "content", Text: "ASR transcription: " + text, Data: map[string]any{"text": text}})
			return nil
		},
	})
}

func (s *AccountTestService) testAudioSpeechProbe(c *gin.Context, account *Account, modelID string, prompt string, options AccountTestOptions) error {
	if account != nil && account.Platform == PlatformAli {
		return s.testAliAudioSpeechProbe(c, account, modelID, prompt, options)
	}
	if err := s.requireAudioProbeAccount(account, AccountTestTypeTTS); err != nil {
		return s.sendErrorAndEnd(c, err.Error())
	}
	model := strings.TrimSpace(modelID)
	input := strings.TrimSpace(prompt)
	if input == "" {
		input = "hi"
	}
	payload := audioSpeechProbePayload(account, model, input, options.Voice)
	body, _ := json.Marshal(payload)
	return s.runNewAPIProbe(c, account, newAPIProbeRequest{
		TestType:    AccountTestTypeTTS,
		Route:       NewAPIStyleRouteAudio,
		InboundPath: audioSpeechProbePath(account),
		Model:       model,
		Method:      http.MethodPost,
		Body:        body,
		ContentType: "application/json",
		Validate: func(resp *http.Response, responseBody []byte) error {
			contentType := resp.Header.Get("Content-Type")
			normalizedContentType := strings.ToLower(contentType)
			if len(responseBody) == 0 {
				return fmt.Errorf("TTS probe did not return audio content")
			}
			if !isAccountTestAudioResponse(normalizedContentType, responseBody) {
				return fmt.Errorf("TTS probe returned non-audio content type: %s", contentType)
			}
			audioURL, mimeType, previewBytes := accountTestAudioDataURL(account, contentType, responseBody)
			s.sendEvent(c, TestEvent{Type: "audio", AudioURL: audioURL, MimeType: mimeType, Data: map[string]any{
				"content_type": mimeType,
				"bytes":        previewBytes,
			}})
			s.sendEvent(c, TestEvent{Type: "content", Text: "TTS audio probe succeeded", Data: map[string]any{
				"content_type": contentType,
				"bytes":        len(responseBody),
			}})
			return nil
		},
	})
}

func (s *AccountTestService) testAliAudioSpeechProbe(c *gin.Context, account *Account, modelID string, prompt string, options AccountTestOptions) error {
	if account == nil || account.Type != AccountTypeAPIKey {
		return s.sendErrorAndEnd(c, fmt.Sprintf("test type %s requires an API key account", AccountTestTypeTTS))
	}

	model := strings.TrimSpace(modelID)
	input := strings.TrimSpace(prompt)
	if input == "" {
		input = "\u4f60\u597d"
	}
	voice := normalizeAccountTestVoice(options.Voice)
	if voice == "" {
		voice = "Cherry"
	}
	payload := map[string]any{
		"model": model,
		"input": map[string]any{
			"text":          input,
			"voice":         voice,
			"language_type": inferAliQwenTTSLanguageType(input),
		},
	}
	body, _ := json.Marshal(payload)

	s.prepareAccountTestStream(c)
	s.sendEvent(c, TestEvent{Type: "test_start", Model: model, Data: map[string]any{"test_type": AccountTestTypeTTS}})

	req, err := s.aliQwenTTSTestRequest(c.Request.Context(), account, body)
	if err != nil {
		return s.sendErrorAndEnd(c, err.Error())
	}
	resp, responseBody, err := s.executeAccountTestRequest(c.Request.Context(), account, req)
	if err != nil {
		return s.sendErrorAndEnd(c, fmt.Sprintf("Request failed: %s", err.Error()))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return s.sendErrorAndEnd(c, fmt.Sprintf("API returned %d: %s", resp.StatusCode, strings.TrimSpace(extractUpstreamErrorMessage(responseBody))))
	}

	audioBody, err := extractAliQwenTTSAudioBody(responseBody)
	if err != nil {
		return s.sendErrorAndEnd(c, err.Error())
	}
	audioURL, mimeType, previewBytes := accountTestAudioDataURL(account, "", audioBody)
	s.sendEvent(c, TestEvent{Type: "audio", AudioURL: audioURL, MimeType: mimeType, Data: map[string]any{
		"content_type": mimeType,
		"bytes":        previewBytes,
		"voice":        voice,
	}})
	s.sendEvent(c, TestEvent{Type: "content", Text: "Qwen TTS audio probe succeeded", Data: map[string]any{
		"content_type": mimeType,
		"bytes":        len(audioBody),
		"voice":        voice,
	}})
	s.sendEvent(c, TestEvent{Type: "test_complete", Success: true})
	return nil
}

func (s *AccountTestService) testOpenAIEmbeddingProbe(c *gin.Context, account *Account, modelID string) error {
	if err := s.requireOpenAIAPIKeyProbe(account, AccountTestTypeEmbedding); err != nil {
		return s.sendErrorAndEnd(c, err.Error())
	}
	model := strings.TrimSpace(modelID)
	payload := map[string]any{
		"model": model,
		"input": "hi",
	}
	body, _ := json.Marshal(payload)
	return s.runNewAPIProbe(c, account, newAPIProbeRequest{
		TestType:    AccountTestTypeEmbedding,
		Route:       NewAPIStyleRouteEmbeddings,
		InboundPath: "/v1/embeddings",
		Model:       model,
		Method:      http.MethodPost,
		Body:        body,
		ContentType: "application/json",
		Validate: func(_ *http.Response, responseBody []byte) error {
			if !gjson.GetBytes(responseBody, "data.0.embedding").Exists() &&
				!gjson.GetBytes(responseBody, "embedding").Exists() &&
				!gjson.GetBytes(responseBody, "embeddings.0").Exists() {
				return fmt.Errorf("embedding probe response does not contain embeddings")
			}
			count := len(gjson.GetBytes(responseBody, "data").Array())
			if count == 0 && gjson.GetBytes(responseBody, "embedding").Exists() {
				count = 1
			}
			s.sendEvent(c, TestEvent{Type: "content", Text: "Embedding probe succeeded", Data: map[string]any{"items": count}})
			return nil
		},
	})
}

func (s *AccountTestService) testSiliconFlowNewAPIRerankProbe(c *gin.Context, account *Account, modelID string) error {
	if account == nil || account.Platform != PlatformSiliconFlow || account.Type != AccountTypeAPIKey || !accountUsesNewAPIStyleForAccountTest(account) {
		return s.sendUnsupportedTestType(c, AccountTestTypeRerank, account, "rerank probe requires a SiliconFlow New-API style API key account")
	}
	model := strings.TrimSpace(modelID)
	payload := map[string]any{
		"model":     model,
		"query":     "sub2api billing",
		"documents": []string{"usage cost calculation", "docker deployment"},
	}
	body, _ := json.Marshal(payload)
	return s.runNewAPIProbe(c, account, newAPIProbeRequest{
		TestType:    AccountTestTypeRerank,
		Route:       NewAPIStyleRouteRerank,
		InboundPath: "/v1/rerank",
		Model:       model,
		Method:      http.MethodPost,
		Body:        body,
		ContentType: "application/json",
		Validate: func(_ *http.Response, responseBody []byte) error {
			if !gjson.GetBytes(responseBody, "results").Exists() &&
				!gjson.GetBytes(responseBody, "data").Exists() &&
				!gjson.GetBytes(responseBody, "scores").Exists() {
				return fmt.Errorf("rerank probe response does not contain rankings")
			}
			s.sendEvent(c, TestEvent{Type: "content", Text: "Rerank probe succeeded", Data: map[string]any{
				"results": len(gjson.GetBytes(responseBody, "results").Array()),
			}})
			return nil
		},
	})
}

func (s *AccountTestService) testVideoUnderstandingProbe(c *gin.Context, account *Account, modelID string, prompt string) error {
	if err := s.requireVideoUnderstandingProbeAccount(account); err != nil {
		return s.sendErrorAndEnd(c, err.Error())
	}
	model := strings.TrimSpace(modelID)
	videoPrompt := strings.TrimSpace(prompt)
	if videoPrompt == "" {
		videoPrompt = "请同时根据画面和声音，简要说明视频里发生了什么，并只回答一句中文。"
	}
	videoDataURL := "data:video/mp4;base64," + base64.StdEncoding.EncodeToString(accountTestVideoProbe())
	payload := map[string]any{
		"model": model,
		"messages": []map[string]any{
			{
				"role": "user",
				"content": []map[string]any{
					{"type": "text", "text": videoPrompt},
					{"type": "video_url", "video_url": map[string]any{"url": videoDataURL}},
				},
			},
		},
		"stream":     false,
		"max_tokens": 128,
	}
	body, _ := json.Marshal(payload)
	return s.runNewAPIProbe(c, account, newAPIProbeRequest{
		TestType:    AccountTestTypeVideo,
		Route:       NewAPIStyleRouteChatCompletions,
		InboundPath: "/v1/chat/completions",
		Model:       model,
		Method:      http.MethodPost,
		Body:        body,
		ContentType: "application/json",
		Validate: func(_ *http.Response, responseBody []byte) error {
			text := extractAccountTestChatContent(responseBody)
			if text == "" {
				return fmt.Errorf("video understanding probe response does not contain text content")
			}
			s.sendEvent(c, TestEvent{Type: "content", Text: "Video understanding: " + text, Data: map[string]any{"text": text}})
			return nil
		},
	})
}

func (s *AccountTestService) testNewAPITaskProbe(c *gin.Context, account *Account, modelID string, prompt string) error {
	if account == nil || !accountUsesNewAPIStyleForAccountTest(account) || account.Type != AccountTypeAPIKey {
		return s.sendUnsupportedTestType(c, AccountTestTypeTask, account, "task probe requires a New-API style API key account")
	}
	taskPrompt := strings.TrimSpace(prompt)
	if taskPrompt == "" {
		taskPrompt = "A cute orange cat astronaut sticker."
	}
	model := strings.TrimSpace(modelID)
	var route NewAPIStyleRoute
	var submitPath string
	var payload map[string]any
	var pollBuilder func(string) func(context.Context) (*http.Request, error)

	switch account.Platform {
	case PlatformSuno:
		route = NewAPIStyleRouteSuno
		submitPath = "/suno/submit/music"
		payload = map[string]any{
			"prompt":                 taskPrompt,
			"gpt_description_prompt": taskPrompt,
			"mv":                     "chirp-v3-0",
			"make_instrumental":      false,
		}
		pollBuilder = func(taskID string) func(context.Context) (*http.Request, error) {
			pollBody, _ := json.Marshal(map[string]any{"ids": []string{taskID}})
			return func(ctx context.Context) (*http.Request, error) {
				return s.newAPIProbeHTTPRequest(ctx, account, newAPIProbeRequest{
					Route:       NewAPIStyleRouteSuno,
					InboundPath: "/suno/fetch",
					Model:       model,
					Method:      http.MethodPost,
					Body:        pollBody,
					ContentType: "application/json",
				})
			}
		}
	case PlatformKling:
		route = NewAPIStyleRouteKling
		submitPath = "/kling/v1/videos/text2video"
		payload = map[string]any{
			"model":        model,
			"model_name":   model,
			"prompt":       taskPrompt,
			"mode":         "std",
			"duration":     "5",
			"aspect_ratio": "1:1",
		}
		pollBuilder = func(taskID string) func(context.Context) (*http.Request, error) {
			return func(ctx context.Context) (*http.Request, error) {
				return s.newAPIProbeHTTPRequest(ctx, account, newAPIProbeRequest{
					Route:       NewAPIStyleRouteKling,
					InboundPath: "/kling/v1/videos/text2video/" + taskID,
					Model:       model,
					Method:      http.MethodGet,
				})
			}
		}
	case PlatformMidjourney:
		route = NewAPIStyleRouteMidjourney
		submitPath = "/mj/submit/imagine"
		payload = map[string]any{
			"prompt": taskPrompt,
		}
		pollBuilder = func(taskID string) func(context.Context) (*http.Request, error) {
			return func(ctx context.Context) (*http.Request, error) {
				return s.newAPIProbeHTTPRequest(ctx, account, newAPIProbeRequest{
					Route:       NewAPIStyleRouteMidjourney,
					InboundPath: "/mj/task/" + taskID + "/fetch",
					Model:       model,
					Method:      http.MethodGet,
				})
			}
		}
	default:
		return s.sendUnsupportedTestType(c, AccountTestTypeTask, account, "platform does not expose a New-API task probe")
	}

	body, _ := json.Marshal(payload)
	return s.runTaskLikeNewAPIProbe(c, account, newAPIProbeRequest{
		TestType:    AccountTestTypeTask,
		Route:       route,
		InboundPath: submitPath,
		Model:       model,
		Method:      http.MethodPost,
		Body:        body,
		ContentType: "application/json",
	}, pollBuilder)
}

type newAPIProbeRequest struct {
	TestType    string
	Route       NewAPIStyleRoute
	InboundPath string
	Model       string
	Method      string
	Body        []byte
	ContentType string
	Validate    func(resp *http.Response, responseBody []byte) error
}

func (s *AccountTestService) runNewAPIProbe(c *gin.Context, account *Account, probe newAPIProbeRequest) error {
	s.prepareAccountTestStream(c)
	s.sendEvent(c, TestEvent{Type: "test_start", Model: probe.Model, Data: map[string]any{"test_type": probe.TestType}})
	req, err := s.newAPIProbeHTTPRequest(c.Request.Context(), account, probe)
	if err != nil {
		return s.sendErrorAndEnd(c, err.Error())
	}
	resp, body, err := s.executeAccountTestRequest(c.Request.Context(), account, req)
	if err != nil {
		return s.sendErrorAndEnd(c, fmt.Sprintf("Request failed: %s", err.Error()))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return s.sendErrorAndEnd(c, fmt.Sprintf("API returned %d: %s", resp.StatusCode, strings.TrimSpace(extractUpstreamErrorMessage(body))))
	}
	if probe.Validate != nil {
		if err := probe.Validate(resp, body); err != nil {
			return s.sendErrorAndEnd(c, err.Error())
		}
	}
	s.sendEvent(c, TestEvent{Type: "test_complete", Success: true})
	return nil
}

func (s *AccountTestService) runTaskLikeNewAPIProbe(c *gin.Context, account *Account, probe newAPIProbeRequest, pollBuilder func(string) func(context.Context) (*http.Request, error)) error {
	s.prepareAccountTestStream(c)
	s.sendEvent(c, TestEvent{Type: "test_start", Model: probe.Model, Data: map[string]any{"test_type": probe.TestType}})
	req, err := s.newAPIProbeHTTPRequest(c.Request.Context(), account, probe)
	if err != nil {
		return s.sendErrorAndEnd(c, err.Error())
	}
	resp, body, err := s.executeAccountTestRequest(c.Request.Context(), account, req)
	if err != nil {
		return s.sendErrorAndEnd(c, fmt.Sprintf("Request failed: %s", err.Error()))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return s.sendErrorAndEnd(c, fmt.Sprintf("API returned %d: %s", resp.StatusCode, strings.TrimSpace(extractUpstreamErrorMessage(body))))
	}
	taskID := extractAccountTestTaskID(body)
	status := normalizeAccountTestTaskStatus(extractAccountTestTaskStatus(body))
	if status == "failed" {
		return s.sendErrorAndEnd(c, fmt.Sprintf("Task failed: %s", strings.TrimSpace(string(body))))
	}
	if taskID == "" {
		return s.sendErrorAndEnd(c, fmt.Sprintf("Task-style probe response did not include a task id: %s", strings.TrimSpace(extractUpstreamErrorMessage(body))))
	}
	s.sendEvent(c, TestEvent{Type: "content", Text: "Task submitted: " + taskID, Data: map[string]any{"task_id": taskID, "status": status}})
	if status == "completed" {
		s.sendEvent(c, TestEvent{Type: "test_complete", Success: true})
		return nil
	}
	if pollBuilder == nil {
		s.sendEvent(c, TestEvent{Type: "test_complete", Success: true, Data: map[string]any{"task_id": taskID, "status": "submitted"}})
		return nil
	}
	return s.pollAccountTestTask(c, account, taskID, pollBuilder(taskID))
}

func (s *AccountTestService) pollAccountTestTask(c *gin.Context, account *Account, taskID string, buildReq func(context.Context) (*http.Request, error)) error {
	ctx := c.Request.Context()
	deadline := time.Now().Add(accountTestTaskPollTimeout)
	for {
		if time.Now().After(deadline) {
			s.sendEvent(c, TestEvent{Type: "content", Text: "Task submitted and still processing", Data: map[string]any{"task_id": taskID, "status": "processing"}})
			s.sendEvent(c, TestEvent{Type: "test_complete", Success: true})
			return nil
		}
		select {
		case <-ctx.Done():
			return s.sendErrorAndEnd(c, "Task polling canceled")
		case <-time.After(accountTestTaskPollInterval):
		}
		req, err := buildReq(ctx)
		if err != nil {
			return s.sendErrorAndEnd(c, err.Error())
		}
		resp, body, err := s.executeAccountTestRequest(ctx, account, req)
		if err != nil {
			return s.sendErrorAndEnd(c, fmt.Sprintf("Task poll failed: %s", err.Error()))
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return s.sendErrorAndEnd(c, fmt.Sprintf("Task poll returned %d: %s", resp.StatusCode, strings.TrimSpace(extractUpstreamErrorMessage(body))))
		}
		status := normalizeAccountTestTaskStatus(extractAccountTestTaskStatus(body))
		if status == "" {
			status = "processing"
		}
		s.sendEvent(c, TestEvent{Type: "content", Text: "Task status: " + status, Data: map[string]any{"task_id": taskID, "status": status}})
		switch status {
		case "completed":
			s.sendEvent(c, TestEvent{Type: "test_complete", Success: true})
			return nil
		case "failed":
			return s.sendErrorAndEnd(c, fmt.Sprintf("Task failed: %s", strings.TrimSpace(string(body))))
		}
	}
}

func (s *AccountTestService) requireOpenAIAPIKeyProbe(account *Account, testType string) error {
	if account == nil || !account.IsOpenAI() || account.Type != AccountTypeAPIKey {
		return fmt.Errorf("test type %s requires an OpenAI API key account", testType)
	}
	return nil
}

func (s *AccountTestService) requireAudioProbeAccount(account *Account, testType string) error {
	if account == nil || account.Type != AccountTypeAPIKey {
		return fmt.Errorf("test type %s requires an API key account", testType)
	}
	if account.IsOpenAI() || account.Platform == PlatformZhipu {
		return nil
	}
	return fmt.Errorf("test type %s requires an OpenAI, GLM/Zhipu, or Qwen/DashScope API key account", testType)
}

func (s *AccountTestService) requireVideoUnderstandingProbeAccount(account *Account) error {
	if account == nil || account.Type != AccountTypeAPIKey {
		return fmt.Errorf("test type %s requires an API key account", AccountTestTypeVideo)
	}
	switch account.Platform {
	case PlatformSuno, PlatformKling, PlatformMidjourney:
		return fmt.Errorf("test type %s requires a chat-capable account; use task for %s", AccountTestTypeVideo, account.Platform)
	}
	if account.IsOpenAI() || account.IsCompatiblePlatform() {
		return nil
	}
	return fmt.Errorf("test type %s requires an OpenAI-compatible API key account", AccountTestTypeVideo)
}

func audioTranscriptionProbePath(account *Account) string {
	if account != nil && account.Platform == PlatformZhipu {
		return "/api/paas/v4/audio/transcriptions"
	}
	return "/v1/audio/transcriptions"
}

func audioSpeechProbePath(account *Account) string {
	if account != nil && account.Platform == PlatformZhipu {
		return "/api/paas/v4/audio/speech"
	}
	return "/v1/audio/speech"
}

func audioSpeechProbePayload(account *Account, model string, input string, voice string) map[string]any {
	payload := map[string]any{
		"model": model,
		"input": input,
	}
	voice = normalizeAccountTestVoice(voice)
	if account != nil && account.Platform == PlatformZhipu {
		if voice == "" {
			voice = "tongtong"
		}
		payload["voice"] = voice
		payload["response_format"] = "wav"
		return payload
	}
	if voice == "" {
		voice = "alloy"
	}
	payload["voice"] = voice
	payload["response_format"] = "mp3"
	return payload
}

func (s *AccountTestService) aliQwenMultimodalGenerationTestRequest(ctx context.Context, account *Account, body []byte, enableSSE bool) (*http.Request, error) {
	baseURL := strings.TrimSpace(account.GetCredential("base_url"))
	if baseURL == "" && account.IsCustomBaseURLEnabled() {
		baseURL = strings.TrimSpace(account.GetCustomBaseURL())
	}
	if baseURL == "" {
		baseURL = CompatibleDefaultBaseURL(PlatformAli)
	}
	if baseURL == "" {
		return nil, fmt.Errorf("base_url is required for Qwen/DashScope multimodal account test")
	}
	normalizedBaseURL, err := s.validateAccountTestBaseURL(baseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid base URL: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, joinRelayCompatibleURL(normalizedBaseURL, aliQwenMultimodalGenerationPath), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	token := s.newAPIProbeAuthToken(account)
	if token == "" {
		return nil, fmt.Errorf("no API key available")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	if enableSSE {
		req.Header.Set("X-DashScope-SSE", "enable")
	}
	req.ContentLength = int64(len(body))
	return req, nil
}

func (s *AccountTestService) aliQwenTTSTestRequest(ctx context.Context, account *Account, body []byte) (*http.Request, error) {
	return s.aliQwenMultimodalGenerationTestRequest(ctx, account, body, true)
}

func inferAliQwenTTSLanguageType(input string) string {
	for _, r := range input {
		if unicode.Is(unicode.Han, r) {
			return "Chinese"
		}
	}
	return "English"
}

func extractAliQwenTTSAudioBody(responseBody []byte) ([]byte, error) {
	var out bytes.Buffer
	for _, line := range strings.Split(string(responseBody), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		raw := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if raw == "" {
			continue
		}
		audioData := strings.TrimSpace(gjson.Get(raw, "output.audio.data").String())
		if audioData == "" {
			continue
		}
		decoded, err := base64.StdEncoding.DecodeString(audioData)
		if err != nil {
			return nil, fmt.Errorf("qwen TTS probe returned invalid audio data: %w", err)
		}
		if len(decoded) == 0 {
			continue
		}
		if _, err := out.Write(decoded); err != nil {
			return nil, fmt.Errorf("qwen TTS probe buffered audio data: %w", err)
		}
	}
	if out.Len() == 0 {
		return nil, fmt.Errorf("qwen TTS probe response did not include audio data")
	}
	audio := out.Bytes()
	if repaired, ok := repairWAVForBrowser(audio); ok {
		return repaired, nil
	}
	return audio, nil
}

func extractAliQwenGeneratedImageURL(responseBody []byte) string {
	for _, path := range []string{
		"output.choices.0.message.content.0.image",
		"output.choices.0.message.content.0.image_url",
		"output.choices.0.message.content.0.url",
		"output.image_url",
		"output.url",
		"image_url",
		"url",
	} {
		value := gjson.GetBytes(responseBody, path)
		if value.Exists() {
			if s := strings.TrimSpace(value.String()); s != "" {
				return s
			}
		}
	}
	for _, item := range gjson.GetBytes(responseBody, "output.choices.0.message.content").Array() {
		for _, key := range []string{"image", "image_url", "url"} {
			if s := strings.TrimSpace(item.Get(key).String()); s != "" {
				return s
			}
		}
	}
	return ""
}

func normalizeAccountTestVoice(voice string) string {
	voice = strings.TrimSpace(voice)
	if voice == "" {
		return ""
	}
	var builder strings.Builder
	for _, r := range voice {
		if r < 0x20 || r == 0x7f {
			continue
		}
		if _, err := builder.WriteRune(r); err != nil {
			return ""
		}
	}
	normalized := strings.TrimSpace(builder.String())
	runes := []rune(normalized)
	if len(runes) > 128 {
		normalized = string(runes[:128])
	}
	return normalized
}

func accountUsesNewAPIStyleForAccountTest(account *Account) bool {
	if account == nil {
		return false
	}
	if account.UseNewAPIStyleInterface() {
		return true
	}
	for _, group := range account.Groups {
		if groupEnablesNewAPIStyleInterface(group, account.Platform) {
			return true
		}
	}
	return false
}

func (s *AccountTestService) sendUnsupportedTestType(c *gin.Context, testType string, account *Account, reason string) error {
	platform := ""
	if account != nil {
		platform = account.Platform
	}
	if reason == "" {
		reason = "unsupported capability"
	}
	return s.sendErrorAndEnd(c, fmt.Sprintf("Test type %s is unsupported for platform %s: %s", testType, platform, reason))
}

func (s *AccountTestService) prepareAccountTestStream(c *gin.Context) {
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Writer.Flush()
}

func (s *AccountTestService) newAPIProbeHTTPRequest(ctx context.Context, account *Account, probe newAPIProbeRequest) (*http.Request, error) {
	targetURL, err := s.newAPIProbeURL(account, probe)
	if err != nil {
		return nil, err
	}
	method := strings.TrimSpace(probe.Method)
	if method == "" {
		method = http.MethodPost
	}
	var body io.Reader
	if len(probe.Body) > 0 {
		body = bytes.NewReader(probe.Body)
	}
	req, err := http.NewRequestWithContext(ctx, method, targetURL, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	token := s.newAPIProbeAuthToken(account)
	if token == "" {
		return nil, fmt.Errorf("no API key available")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if probe.ContentType != "" {
		req.Header.Set("Content-Type", probe.ContentType)
	}
	if len(probe.Body) > 0 {
		req.ContentLength = int64(len(probe.Body))
	}
	return req, nil
}

func (s *AccountTestService) newAPIProbeURL(account *Account, probe newAPIProbeRequest) (string, error) {
	if account == nil {
		return "", fmt.Errorf("account not found")
	}
	baseURL := strings.TrimSpace(account.GetCredential("base_url"))
	if baseURL == "" && account.IsCustomBaseURLEnabled() {
		baseURL = strings.TrimSpace(account.GetCustomBaseURL())
	}
	if baseURL == "" {
		baseURL = newAPIStyleDefaultBaseURL(account.Platform)
	}
	if baseURL == "" && account.IsCompatiblePlatform() {
		baseURL = CompatibleDefaultBaseURL(account.Platform)
	}
	if baseURL == "" {
		return "", fmt.Errorf("new-api style base_url is required for platform %s", account.Platform)
	}
	normalizedBaseURL, err := s.validateAccountTestBaseURL(baseURL)
	if err != nil {
		return "", fmt.Errorf("invalid base URL: %w", err)
	}
	path := newAPIStyleRoutePath(account.Platform, NewAPIStyleForwardOptions{
		Route:       probe.Route,
		InboundPath: probe.InboundPath,
		Model:       probe.Model,
	})
	if path == "" {
		path = "/" + strings.TrimLeft(strings.TrimSpace(probe.InboundPath), "/")
	}
	return joinRelayCompatibleURL(normalizedBaseURL, path), nil
}

func (s *AccountTestService) validateAccountTestBaseURL(raw string) (string, error) {
	if s != nil && s.cfg != nil {
		return s.validateUpstreamBaseURL(raw)
	}
	return urlvalidator.ValidateURLFormat(raw, true)
}

func (s *AccountTestService) newAPIProbeAuthToken(account *Account) string {
	if account == nil {
		return ""
	}
	if account.IsOpenAI() {
		return strings.TrimSpace(account.GetOpenAIApiKey())
	}
	if account.IsCompatiblePlatform() {
		if preset, ok := newAPIStyleCompatibleProviderPresetForPlatform(account.Platform); ok {
			return strings.TrimSpace(getCompatibleAuthToken(account, preset.AuthMode))
		}
		if preset, ok := CompatibleProviderPresetForPlatform(account.Platform); ok {
			return strings.TrimSpace(getCompatibleAuthToken(account, preset.AuthMode))
		}
	}
	return strings.TrimSpace(account.GetCredential("api_key"))
}

func (s *AccountTestService) executeAccountTestRequest(ctx context.Context, account *Account, req *http.Request) (*http.Response, []byte, error) {
	proxyURL := resolveAccountProxyURL(ctx, account, nil)
	resp, err := s.httpUpstream.DoWithTLS(req, proxyURL, account.ID, account.Concurrency, s.tlsFPProfileService.ResolveTLSProfile(account))
	if err != nil {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		return nil, nil, err
	}
	if resp == nil {
		return nil, nil, fmt.Errorf("no upstream response")
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, accountTestProbeMaxBodyBytes))
	if err != nil {
		return resp, nil, err
	}
	return resp, body, nil
}

func isAccountTestAudioResponse(contentType string, body []byte) bool {
	normalized := strings.ToLower(strings.TrimSpace(contentType))
	if strings.Contains(normalized, "audio/") || strings.Contains(normalized, "application/octet-stream") {
		return true
	}
	if normalized != "" {
		return false
	}
	return looksLikeAudioPayload(body)
}

func looksLikeAudioPayload(body []byte) bool {
	if len(body) < 4 {
		return false
	}
	if bytes.HasPrefix(body, []byte("ID3")) ||
		bytes.HasPrefix(body, []byte("OggS")) ||
		bytes.HasPrefix(body, []byte("fLaC")) {
		return true
	}
	if len(body) >= 12 && bytes.Equal(body[:4], []byte("RIFF")) && bytes.Equal(body[8:12], []byte("WAVE")) {
		return true
	}
	if len(body) >= 2 && body[0] == 0xff && (body[1]&0xe0) == 0xe0 {
		return true
	}
	if len(body) >= 12 && bytes.Equal(body[4:8], []byte("ftyp")) {
		return true
	}
	return false
}

func accountTestAudioDataURL(account *Account, contentType string, body []byte) (string, string, int) {
	audioBody, mimeType := accountTestAudioPreviewBody(account, contentType, body)
	return fmt.Sprintf("data:%s;base64,%s", mimeType, base64.StdEncoding.EncodeToString(audioBody)), mimeType, len(audioBody)
}

func accountTestAudioPreviewBody(account *Account, contentType string, body []byte) ([]byte, string) {
	mimeType := accountTestAudioMIMEType(contentType, body)
	if isAccountTestWAVMIMEType(mimeType) || isAccountTestWAVMIMEType(contentType) || looksLikeWAVPayload(body) {
		if repaired, ok := repairWAVForBrowser(body); ok {
			return repaired, "audio/wav"
		}
		if account != nil && account.Platform == PlatformZhipu && !looksLikeTextPayload(body) {
			return buildPCM16WAV(body, 24000, 1), "audio/wav"
		}
	}
	return body, mimeType
}

func accountTestAudioMIMEType(contentType string, body []byte) string {
	normalized := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	if inferred := inferAccountTestAudioMIMEType(body); inferred != "" {
		return inferred
	}
	if isAccountTestWAVMIMEType(normalized) {
		return "audio/wav"
	}
	if strings.HasPrefix(normalized, "audio/") {
		return normalized
	}
	if normalized == "application/octet-stream" {
		return normalized
	}
	return "audio/mpeg"
}

func isAccountTestWAVMIMEType(contentType string) bool {
	normalized := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	return normalized == "audio/wav" || normalized == "audio/wave" || normalized == "audio/x-wav" || normalized == "audio/vnd.wave"
}

func inferAccountTestAudioMIMEType(body []byte) string {
	if len(body) < 4 {
		return ""
	}
	if bytes.HasPrefix(body, []byte("ID3")) || (len(body) >= 2 && body[0] == 0xff && (body[1]&0xe0) == 0xe0) {
		return "audio/mpeg"
	}
	if len(body) >= 12 && bytes.Equal(body[:4], []byte("RIFF")) && bytes.Equal(body[8:12], []byte("WAVE")) {
		return "audio/wav"
	}
	if bytes.HasPrefix(body, []byte("OggS")) {
		return "audio/ogg"
	}
	if bytes.HasPrefix(body, []byte("fLaC")) {
		return "audio/flac"
	}
	if len(body) >= 12 && bytes.Equal(body[4:8], []byte("ftyp")) {
		return "audio/mp4"
	}
	return ""
}

func looksLikeWAVPayload(body []byte) bool {
	return len(body) >= 12 && bytes.Equal(body[:4], []byte("RIFF")) && bytes.Equal(body[8:12], []byte("WAVE"))
}

func repairWAVForBrowser(body []byte) ([]byte, bool) {
	if !looksLikeWAVPayload(body) {
		return nil, false
	}
	var fmtChunk []byte
	var dataChunk []byte
	for offset := 12; offset+8 <= len(body); {
		chunkID := string(body[offset : offset+4])
		chunkSize := int(binary.LittleEndian.Uint32(body[offset+4 : offset+8]))
		dataStart := offset + 8
		dataEnd := dataStart + chunkSize
		if chunkSize < 0 || dataEnd > len(body) {
			if chunkID != "data" || dataStart >= len(body) {
				return nil, false
			}
			dataEnd = len(body)
		}
		if chunkID == "data" && chunkSize == 0 && dataStart < len(body) {
			dataEnd = len(body)
		}
		chunkData := body[dataStart:dataEnd]
		switch chunkID {
		case "fmt ":
			fmtChunk = append([]byte(nil), chunkData...)
		case "data":
			dataChunk = append([]byte(nil), chunkData...)
		}
		offset = dataEnd
		if chunkSize%2 == 1 && offset < len(body) {
			offset++
		}
		if len(dataChunk) > 0 && len(fmtChunk) > 0 {
			break
		}
	}
	if len(fmtChunk) == 0 || len(dataChunk) == 0 {
		return nil, false
	}
	return buildWAVFromChunks(fmtChunk, dataChunk), true
}

func buildPCM16WAV(pcm []byte, sampleRate int, channels int) []byte {
	if sampleRate <= 0 {
		sampleRate = 24000
	}
	if channels <= 0 {
		channels = 1
	}
	bitsPerSample := 16
	blockAlign := channels * bitsPerSample / 8
	byteRate := sampleRate * blockAlign
	fmtChunk := make([]byte, 16)
	binary.LittleEndian.PutUint16(fmtChunk[0:2], 1)
	binary.LittleEndian.PutUint16(fmtChunk[2:4], uint16(channels))
	binary.LittleEndian.PutUint32(fmtChunk[4:8], uint32(sampleRate))
	binary.LittleEndian.PutUint32(fmtChunk[8:12], uint32(byteRate))
	binary.LittleEndian.PutUint16(fmtChunk[12:14], uint16(blockAlign))
	binary.LittleEndian.PutUint16(fmtChunk[14:16], uint16(bitsPerSample))
	return buildWAVFromChunks(fmtChunk, pcm)
}

func buildWAVFromChunks(fmtChunk []byte, dataChunk []byte) []byte {
	var out bytes.Buffer
	mustWriteProbeString(&out, "RIFF")
	mustWriteProbeBinary(&out, uint32(0))
	mustWriteProbeString(&out, "WAVE")
	writeWAVChunk(&out, "fmt ", fmtChunk)
	writeWAVChunk(&out, "data", dataChunk)
	bytes := out.Bytes()
	binary.LittleEndian.PutUint32(bytes[4:8], uint32(len(bytes)-8))
	return bytes
}

func writeWAVChunk(out *bytes.Buffer, id string, data []byte) {
	mustWriteProbeString(out, id)
	mustWriteProbeBinary(out, uint32(len(data)))
	mustWriteProbeBytes(out, data)
	if len(data)%2 == 1 {
		if err := out.WriteByte(0); err != nil {
			panic(err)
		}
	}
}

func mustWriteProbeString(out *bytes.Buffer, value string) {
	if _, err := out.WriteString(value); err != nil {
		panic(err)
	}
}

func mustWriteProbeBytes(out *bytes.Buffer, value []byte) {
	if _, err := out.Write(value); err != nil {
		panic(err)
	}
}

func mustWriteProbeBinary(out *bytes.Buffer, value uint32) {
	if err := binary.Write(out, binary.LittleEndian, value); err != nil {
		panic(err)
	}
}

func looksLikeTextPayload(body []byte) bool {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return false
	}
	switch trimmed[0] {
	case '{', '[', '<':
		return true
	}
	sampleSize := len(trimmed)
	if sampleSize > 256 {
		sampleSize = 256
	}
	printable := 0
	for _, b := range trimmed[:sampleSize] {
		if b == '\n' || b == '\r' || b == '\t' || (b >= 0x20 && b <= 0x7e) {
			printable++
		}
	}
	return printable*100/sampleSize >= 90
}

func accountTestASRProbeAudio() []byte {
	return accountTestChineseASRProbeMP3
}

func accountTestVideoProbe() []byte {
	return accountTestChineseVideoProbeMP4
}

func extractAccountTestChatContent(body []byte) string {
	for _, path := range []string{
		"choices.0.message.content",
		"choices.0.delta.content",
		"output_text",
		"text",
		"content",
		"message.content",
	} {
		value := gjson.GetBytes(body, path)
		if value.Exists() {
			if s := strings.TrimSpace(value.String()); s != "" {
				return s
			}
		}
	}
	return ""
}

func extractAccountTestTaskID(body []byte) string {
	for _, path := range []string{
		"id", "task_id", "taskId", "taskID",
		"data", "data.id", "data.task_id", "data.taskId", "data.taskID",
		"result.id", "result.task_id", "result.taskId", "result.taskID",
		"data.0.id", "data.0.task_id", "data.0.taskId",
	} {
		value := gjson.GetBytes(body, path)
		if value.Exists() && value.Type == gjson.String {
			if s := strings.TrimSpace(value.String()); s != "" {
				return s
			}
		}
	}
	return ""
}

func extractAccountTestTaskStatus(body []byte) string {
	for _, path := range []string{
		"status", "state", "task_status", "taskStatus",
		"data.status", "data.state", "data.task_status", "data.taskStatus",
		"result.status", "result.state", "result.task_status",
		"data.0.status", "data.0.state", "data.0.task_status",
	} {
		value := gjson.GetBytes(body, path)
		if value.Exists() {
			if s := strings.TrimSpace(value.String()); s != "" {
				return s
			}
		}
	}
	return ""
}

func normalizeAccountTestTaskStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "success", "succeed", "succeeded", "completed", "complete", "finished", "done", "finish", "successed":
		return "completed"
	case "fail", "failed", "failure", "error", "errored", "canceled", "cancelled", "rejected":
		return "failed"
	case "submitted", "queued", "queueing", "pending", "processing", "in_progress", "running", "created", "wait":
		return "processing"
	default:
		return strings.ToLower(strings.TrimSpace(status))
	}
}
