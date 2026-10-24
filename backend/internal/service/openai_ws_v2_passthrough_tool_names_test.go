package service

import (
	"context"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestPassthroughLifecycle_OAuthRestoresReservedToolNames(t *testing.T) {
	controlCtx, cancelControl := context.WithCancelCause(context.Background())
	defer cancelControl(context.Canceled)
	upstream := newStagedPassthroughConn()
	cfg := passthroughLifecycleConfig()
	cfg.Gateway.OpenAIWS.OAuthEnabled = true
	account := passthroughLifecycleAccount()
	account.Type = AccountTypeOAuth
	account.Extra = map[string]any{"openai_oauth_responses_websockets_v2_mode": OpenAIWSIngressModePassthrough}
	server, serverErr := startPassthroughLifecycleServer(t, controlCtx, newPassthroughLifecycleService(cfg, upstream), account)
	defer server.Close()
	clientConn := dialPassthroughLifecycleClientWithPayload(t, server,
		`{"type":"response.create","model":"gpt-5.1","tools":[{"type":"function","name":"python","parameters":{"type":"object"}}],"input":"run python"}`)
	defer func() { _ = clientConn.CloseNow() }()

	request := requirePassthroughUpstreamWrite(t, upstream, 3*time.Second)
	require.Equal(t, "python__sub2api", gjson.GetBytes(request, "tools.0.name").String())
	upstream.Send(`{"type":"response.output_item.done","item":{"type":"function_call","id":"fc_python","call_id":"call_python","name":"python__sub2api","arguments":"{}"}}`)
	upstream.Send(`{"type":"response.completed","response":{"id":"resp_python","model":"gpt-5.1","output":[{"type":"function_call","id":"fc_python","call_id":"call_python","name":"python__sub2api","arguments":"{}"}],"usage":{"input_tokens":1,"output_tokens":1}}}`)
	item, err := readPassthroughLifecycleFrame(t, clientConn, 3*time.Second)
	require.NoError(t, err)
	require.Equal(t, "python", gjson.GetBytes(item, "item.name").String(), "the client must receive the tool name it declared")
	completed, err := readPassthroughLifecycleFrame(t, clientConn, 3*time.Second)
	require.NoError(t, err)
	require.Equal(t, "python", gjson.GetBytes(completed, "response.output.0.name").String())

	// A later turn may explicitly declare the literal alias as a different tool.
	writeCtx, cancelWrite := context.WithTimeout(context.Background(), 3*time.Second)
	err = clientConn.Write(writeCtx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.1","tools":[{"type":"function","name":"python__sub2api","parameters":{"type":"object"}}],"input":"run literal tool"}`))
	cancelWrite()
	require.NoError(t, err)
	request = requirePassthroughUpstreamWrite(t, upstream, 3*time.Second)
	require.Equal(t, "python__sub2api", gjson.GetBytes(request, "tools.0.name").String())
	upstream.Send(`{"type":"response.completed","response":{"id":"resp_literal","model":"gpt-5.1","output":[{"type":"function_call","id":"fc_literal","call_id":"call_literal","name":"python__sub2api","arguments":"{}"}],"usage":{"input_tokens":1,"output_tokens":1}}}`)
	completed, err = readPassthroughLifecycleFrame(t, clientConn, 3*time.Second)
	require.NoError(t, err)
	require.Equal(t, "python__sub2api", gjson.GetBytes(completed, "response.output.0.name").String(), "turn-local restoration must not rename a literal alias")

	require.NoError(t, clientConn.Close(coderws.StatusNormalClosure, "done"))
	select {
	case <-serverErr:
	case <-time.After(3 * time.Second):
		t.Fatal("tool-name passthrough did not exit")
	}
}
