package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"mime/multipart"
	"net/textproto"
	"testing"
)

func TestBillingModeDurationAndCharacterAreValid(t *testing.T) {
	if !BillingModeDuration.IsValid() {
		t.Fatalf("duration billing mode should be valid")
	}
	if !BillingModeCharacter.IsValid() {
		t.Fatalf("character billing mode should be valid")
	}
}

func TestGatewayRecordUsageRejectsNilInputOrResult(t *testing.T) {
	svc := &GatewayService{}

	if err := svc.RecordUsage(context.Background(), nil); err == nil || err.Error() != "usage input is nil" {
		t.Fatalf("RecordUsage(nil) error = %v, want usage input is nil", err)
	}
	if err := svc.RecordUsage(context.Background(), &RecordUsageInput{}); err == nil || err.Error() != "usage result is nil" {
		t.Fatalf("RecordUsage(nil result) error = %v, want usage result is nil", err)
	}
	if err := svc.RecordUsageWithLongContext(context.Background(), nil); err == nil || err.Error() != "usage input is nil" {
		t.Fatalf("RecordUsageWithLongContext(nil) error = %v, want usage input is nil", err)
	}
	if err := svc.RecordUsageWithLongContext(context.Background(), &RecordUsageLongContextInput{}); err == nil || err.Error() != "usage result is nil" {
		t.Fatalf("RecordUsageWithLongContext(nil result) error = %v, want usage result is nil", err)
	}
}

func TestCalculateCostUnifiedDurationAndCharacterModes(t *testing.T) {
	svc := &BillingService{}
	resolver := &ModelPricingResolver{}

	durationCost, err := svc.CalculateCostUnified(CostInput{
		Ctx:             context.Background(),
		Model:           "asr-model",
		DurationSeconds: 13,
		RateMultiplier:  1.5,
		Resolver:        resolver,
		Resolved: &ResolvedPricing{
			Mode:                   BillingModeDuration,
			DefaultPerRequestPrice: 0.02,
		},
	})
	if err != nil {
		t.Fatalf("duration cost error: %v", err)
	}
	if diff := durationCost.TotalCost - 0.26; diff < -1e-12 || diff > 1e-12 {
		t.Fatalf("duration total cost = %v, want 0.26", durationCost.TotalCost)
	}
	if diff := durationCost.ActualCost - 0.39; diff < -1e-12 || diff > 1e-12 {
		t.Fatalf("duration actual cost = %v, want 0.39", durationCost.ActualCost)
	}

	durationFallbackCost, err := svc.CalculateCostUnified(CostInput{
		Ctx:            context.Background(),
		Model:          "asr-model",
		RateMultiplier: 1,
		Resolver:       resolver,
		Resolved: &ResolvedPricing{
			Mode:                   BillingModeDuration,
			DefaultPerRequestPrice: 0.02,
		},
	})
	if err != nil {
		t.Fatalf("duration fallback cost error: %v", err)
	}
	if diff := durationFallbackCost.TotalCost - 0.02; diff < -1e-12 || diff > 1e-12 {
		t.Fatalf("duration fallback total cost = %v, want 0.02", durationFallbackCost.TotalCost)
	}

	characterCost, err := svc.CalculateCostUnified(CostInput{
		Ctx:            context.Background(),
		Model:          "tts-model",
		CharacterCount: 2500,
		RateMultiplier: 2,
		Resolver:       resolver,
		Resolved: &ResolvedPricing{
			Mode:                   BillingModeCharacter,
			DefaultPerRequestPrice: 0.03,
		},
	})
	if err != nil {
		t.Fatalf("character cost error: %v", err)
	}
	if diff := characterCost.TotalCost - 0.075; diff < -1e-12 || diff > 1e-12 {
		t.Fatalf("character total cost = %v, want 0.075", characterCost.TotalCost)
	}
	if diff := characterCost.ActualCost - 0.15; diff < -1e-12 || diff > 1e-12 {
		t.Fatalf("character actual cost = %v, want 0.15", characterCost.ActualCost)
	}

	characterFallbackCost, err := svc.CalculateCostUnified(CostInput{
		Ctx:            context.Background(),
		Model:          "tts-model",
		RateMultiplier: 1,
		Resolver:       resolver,
		Resolved: &ResolvedPricing{
			Mode:                   BillingModeCharacter,
			DefaultPerRequestPrice: 0.03,
		},
	})
	if err != nil {
		t.Fatalf("character fallback cost error: %v", err)
	}
	if diff := characterFallbackCost.TotalCost - 0.03; diff < -1e-12 || diff > 1e-12 {
		t.Fatalf("character fallback total cost = %v, want 0.03", characterFallbackCost.TotalCost)
	}
}

func TestExtractBillableDurationSecondsFromResponse(t *testing.T) {
	seconds, ok := extractBillableDurationSeconds([]byte(`{"usage":{"seconds":2.1}}`), nil, "", false)
	if !ok || seconds != 3 {
		t.Fatalf("duration seconds = %d, ok=%v, want 3 true", seconds, ok)
	}

	seconds, ok = extractBillableDurationSeconds([]byte(`{"usage":{"seconds":0}}`), nil, "", false)
	if !ok || seconds != 1 {
		t.Fatalf("zero response seconds fallback = %d, ok=%v, want 1 true", seconds, ok)
	}
}

func TestExtractBillableDurationSecondsFromMultipartAudio(t *testing.T) {
	audio := testWAVBytes(2)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", `form-data; name="file"; filename="sample.wav"`)
	header.Set("Content-Type", "audio/wav")
	part, err := writer.CreatePart(header)
	if err != nil {
		t.Fatalf("create multipart part: %v", err)
	}
	if _, err := part.Write(audio); err != nil {
		t.Fatalf("write multipart part: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	seconds, ok := extractBillableDurationSeconds(nil, body.Bytes(), writer.FormDataContentType(), false)
	if !ok || seconds != 2 {
		t.Fatalf("multipart audio duration = %d, ok=%v, want 2 true", seconds, ok)
	}
}

func TestExtractBillableDurationSecondsFromJSONAudioDataURL(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString(testWAVBytes(3))
	body := []byte(`{"input_audio":{"data":"data:audio/wav;base64,` + encoded + `"}}`)

	seconds, ok := extractBillableDurationSeconds(nil, body, "application/json", false)
	if !ok || seconds != 3 {
		t.Fatalf("json audio duration = %d, ok=%v, want 3 true", seconds, ok)
	}
}

func TestExtractBillableCharacterCountFromRequestText(t *testing.T) {
	body := []byte(`{
		"input":{"text":"你好abc","voice":"Cherry"},
		"audio":"data:audio/wav;base64,UklGRg==",
		"url":"https://example.test/audio.wav"
	}`)

	count, ok := extractBillableCharacterCount(body, "application/json", false)
	if !ok || count != 5 {
		t.Fatalf("character count = %d, ok=%v, want 5 true", count, ok)
	}
}

func TestExtractBillableCharacterCountFallback(t *testing.T) {
	count, ok := extractBillableCharacterCount([]byte(`{"input":"https://example.test/audio.wav"}`), "application/json", true)
	if !ok || count != 1000 {
		t.Fatalf("character fallback count = %d, ok=%v, want 1000 true", count, ok)
	}
}

func testWAVBytes(seconds int) []byte {
	const (
		channels      = 1
		sampleRate    = 8000
		bitsPerSample = 16
	)
	byteRate := sampleRate * channels * bitsPerSample / 8
	blockAlign := channels * bitsPerSample / 8
	dataSize := byteRate * seconds
	totalSize := 4 + (8 + 16) + (8 + dataSize)
	out := make([]byte, 8+totalSize)

	copy(out[0:4], "RIFF")
	binary.LittleEndian.PutUint32(out[4:8], uint32(totalSize))
	copy(out[8:12], "WAVE")
	copy(out[12:16], "fmt ")
	binary.LittleEndian.PutUint32(out[16:20], 16)
	binary.LittleEndian.PutUint16(out[20:22], 1)
	binary.LittleEndian.PutUint16(out[22:24], channels)
	binary.LittleEndian.PutUint32(out[24:28], uint32(sampleRate))
	binary.LittleEndian.PutUint32(out[28:32], uint32(byteRate))
	binary.LittleEndian.PutUint16(out[32:34], uint16(blockAlign))
	binary.LittleEndian.PutUint16(out[34:36], bitsPerSample)
	copy(out[36:40], "data")
	binary.LittleEndian.PutUint32(out[40:44], uint32(dataSize))
	return out
}
