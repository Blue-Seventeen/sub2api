package handler

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestRequestBodyParseLogRedactsSensitiveSnippetFields(t *testing.T) {
	core, logs := observer.New(zap.WarnLevel)
	body := []byte(`{"model":"gpt-5.4","api_key":"sk-secret","access_token":"tok-secret","Authorization":"Bearer bearer-secret",`)

	logRequestBodyParseFailure(zap.New(core), body, nil)

	entries := logs.All()
	require.Len(t, entries, 1)
	var head string
	for _, field := range entries[0].Context {
		if field.Key == "body_head" {
			head = field.String
			break
		}
	}
	require.NotEmpty(t, head)
	require.NotContains(t, head, "sk-secret")
	require.NotContains(t, head, "tok-secret")
	require.NotContains(t, head, "bearer-secret")
	require.Contains(t, head, `api_key\":\"***`)
	require.Contains(t, head, `access_token\":\"***`)
}
