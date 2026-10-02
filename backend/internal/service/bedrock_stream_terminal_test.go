package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"hash/crc32"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func bedrockChunkFrameForTest(payload []byte) []byte {
	var headers bytes.Buffer
	writeHeader := func(name, value string) {
		_ = headers.WriteByte(byte(len(name)))
		_, _ = headers.WriteString(name)
		_ = headers.WriteByte(7)
		_ = binary.Write(&headers, binary.BigEndian, uint16(len(value)))
		_, _ = headers.WriteString(value)
	}
	writeHeader(":event-type", "chunk")
	writeHeader(":message-type", "event")

	prelude := make([]byte, 8)
	totalLength := uint32(12 + headers.Len() + len(payload) + 4)
	binary.BigEndian.PutUint32(prelude[0:4], totalLength)
	binary.BigEndian.PutUint32(prelude[4:8], uint32(headers.Len()))

	var frame bytes.Buffer
	_, _ = frame.Write(prelude)
	_ = binary.Write(&frame, binary.BigEndian, crc32.Checksum(prelude, crc32.IEEETable))
	_, _ = frame.Write(headers.Bytes())
	_, _ = frame.Write(payload)
	_ = binary.Write(&frame, binary.BigEndian, crc32.Checksum(frame.Bytes(), crc32.IEEETable))
	return frame.Bytes()
}

func bedrockChunkResponseFrameForTest(sse string) []byte {
	payload := []byte(`{"bytes":"` + base64.StdEncoding.EncodeToString([]byte(sse)) + `"}`)
	return bedrockChunkFrameForTest(payload)
}

func TestHandleBedrockStreamingResponse_MissingTerminalKeepsPartialUsage(t *testing.T) {
	svc := &GatewayService{}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader(bedrockChunkResponseFrameForTest(`{"type":"message_delta","usage":{"output_tokens":4}}`))),
		Header:     http.Header{},
	}

	result, err := svc.handleBedrockStreamingResponse(context.Background(), resp, c, &Account{ID: 1}, time.Now(), "claude-sonnet")

	require.Error(t, err)
	require.EqualError(t, err, "bedrock stream usage incomplete: missing terminal event")
	require.NotNil(t, result)
	require.NotNil(t, result.usage)
	require.Equal(t, 4, result.usage.OutputTokens)
	require.Contains(t, rec.Body.String(), "message_delta")
}

func TestHandleBedrockStreamingResponse_MessageStopCompletes(t *testing.T) {
	svc := &GatewayService{}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
	var body bytes.Buffer
	_, _ = body.Write(bedrockChunkResponseFrameForTest(`{"type":"message_delta","usage":{"output_tokens":4}}`))
	_, _ = body.Write(bedrockChunkResponseFrameForTest(`{"type":"message_stop"}`))
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader(body.Bytes())),
		Header:     http.Header{},
	}

	result, err := svc.handleBedrockStreamingResponse(context.Background(), resp, c, &Account{ID: 1}, time.Now(), "claude-sonnet")

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 4, result.usage.OutputTokens)
	require.Contains(t, rec.Body.String(), "message_stop")
}
