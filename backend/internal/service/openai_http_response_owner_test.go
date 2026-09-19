package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenAIHTTPResponseOwnerProductionBinding(t *testing.T) {
	ctx := context.Background()
	svc := &OpenAIGatewayService{}
	c, _ := newNonStreamingFailoverContext(t)
	groupID := int64(41)
	c.Set("api_key", &APIKey{GroupID: &groupID})
	SetOpenAIHTTPResponseOwner(c, 201, 301)
	svc.bindHTTPResponseAccount(ctx, c, &Account{ID: 91}, " resp_owned ")
	for _, tt := range []struct {
		name string
		groupID, userID, apiKeyID int64
		responseID string
		want bool
	}{
		{"original key", 41, 201, 301, "resp_owned", true},
		{"same user different key", 41, 201, 302, "resp_owned", true},
		{"other tenant same key", 41, 202, 301, "resp_owned", false},
		{"other group", 42, 201, 301, "resp_owned", false},
		{"unknown response", 41, 201, 301, "resp_unknown", false},
		{"invalid user", 41, 0, 301, "resp_owned", false},
		{"invalid key", 41, 201, 0, "resp_owned", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			allowed, err := svc.ValidateOpenAIHTTPResponseOwner(ctx, tt.groupID, tt.responseID, tt.userID, tt.apiKeyID)
			require.NoError(t, err)
			require.Equal(t, tt.want, allowed)
		})
	}
	userID, keyID, found, err := svc.getOpenAIWSStateStore().GetHTTPResponseOwner(ctx, groupID, "resp_owned")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, int64(201), userID)
	require.Equal(t, int64(301), keyID)
}
