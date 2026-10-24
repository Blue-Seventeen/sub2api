package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpsErrorProxyRoutes_AdminOnlyJSONContract(t *testing.T) {
	var entry OpsErrorLog
	require.NoError(t, json.Unmarshal([]byte(`{"proxy_routes":[{"kind":"proxy","proxy_id":152,"name":"request-time-node","host":"192.0.2.10","port":1080}]}`), &entry))
	adminJSON, err := json.Marshal(entry)
	require.NoError(t, err)
	require.Contains(t, string(adminJSON), `"proxy_routes":[{"kind":"proxy","proxy_id":152`)
	for _, userView := range []any{ToUserErrorRequest(&entry), ToUserErrorRequestDetail(&OpsErrorLogDetail{OpsErrorLog: entry})} {
		userJSON, err := json.Marshal(userView)
		require.NoError(t, err)
		for _, forbidden := range []string{"proxy_routes", "proxy_id", "request-time-node", "192.0.2.10", "1080"} {
			require.NotContains(t, string(userJSON), forbidden)
		}
	}
	unknownJSON, err := json.Marshal(OpsErrorLog{})
	require.NoError(t, err)
	require.Contains(t, string(unknownJSON), `"proxy_routes":null`)
}
