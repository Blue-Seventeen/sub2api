package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenAICompactKeepaliveWriter_StatusAccessorsAreNilSafe(t *testing.T) {
	var nilWriter *openAICompactKeepaliveWriter
	require.NotPanics(t, func() {
		require.Equal(t, 0, nilWriter.Status())
		require.Equal(t, -1, nilWriter.Size())
		require.False(t, nilWriter.Written())
	})

	detachedWriter := &openAICompactKeepaliveWriter{}
	require.NotPanics(t, func() {
		require.Equal(t, 0, detachedWriter.Status())
		require.Equal(t, -1, detachedWriter.Size())
		require.False(t, detachedWriter.Written())
	})
}

func TestOpenAICompactKeepaliveWriter_DelegatesWithoutKeepaliveState(t *testing.T) {
	c, rec := newCompactBridgeTestContext(t, true)
	writer := &openAICompactKeepaliveWriter{ResponseWriter: c.Writer}

	_, err := writer.WriteString("ok")
	require.NoError(t, err)

	require.Equal(t, "ok", rec.Body.String())
	require.Equal(t, len("ok"), writer.Size())
	require.True(t, writer.Written())
}
