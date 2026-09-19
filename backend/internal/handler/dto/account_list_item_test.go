package dto

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAccountListItemFromAccountOmitsDetailGraphs(t *testing.T) {
	detail := &Account{
		ID: 7, Name: "account", Platform: "openai", Type: "oauth",
		GroupIDs: []int64{1, 2}, Groups: []*Group{{ID: 1, Name: "private"}},
		Proxy: &Proxy{ID: 3, Host: "proxy.example"},
	}
	item := AccountListItemFromAccount(detail)
	require.NotNil(t, item)
	require.Equal(t, int64(7), item.ID)
	require.Equal(t, "account", item.Name)

	body, err := json.Marshal(item)
	require.NoError(t, err)
	require.NotContains(t, string(body), "group_ids")
	require.NotContains(t, string(body), "groups")
}
