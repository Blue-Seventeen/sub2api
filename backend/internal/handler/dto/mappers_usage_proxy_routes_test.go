package dto

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUsageProxyRoutesHistoricalAdminUnknownUserPrivate(t *testing.T) {
	log := &service.UsageLog{Model: "model"}
	adminJSON, err := json.Marshal(UsageLogFromServiceAdmin(log))
	require.NoError(t, err)
	var admin map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(adminJSON, &admin))
	require.Contains(t, admin, "proxy_routes", "admin API must explicitly represent historical route uncertainty")
	require.JSONEq(t, "null", string(admin["proxy_routes"]))
	userJSON, err := json.Marshal(UsageLogFromService(log))
	require.NoError(t, err)
	require.NotContains(t, string(userJSON), "proxy_routes")
}

func TestUsageProxyRoutesAdminSnapshotUserPrivate(t *testing.T) {
	log := &service.UsageLog{
		Model: "model",
		ProxyRoutes: []service.ProxyRouteSnapshot{
			{Kind: "proxy", ProxyID: 19, Name: "selected-node", Host: "2001:db8::4", Port: 1080},
			{Kind: "direct"},
		},
	}
	admin := UsageLogFromServiceAdmin(log)
	adminJSON, err := json.Marshal(admin)
	require.NoError(t, err)
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(adminJSON, &fields))
	require.JSONEq(t, `[{"kind":"proxy","proxy_id":19,"name":"selected-node","host":"2001:db8::4","port":1080},{"kind":"direct"}]`, string(fields["proxy_routes"]))
	userJSON, err := json.Marshal(UsageLogFromService(log))
	require.NoError(t, err)
	for _, secret := range []string{"proxy_routes", "selected-node", "2001:db8::4"} {
		require.NotContains(t, string(userJSON), secret)
	}
	log.ProxyRoutes[0].Name = "changed-after-mapping"
	require.Equal(t, "selected-node", admin.ProxyRoutes[0].Name, "mapping preserves a detached snapshot")
}
