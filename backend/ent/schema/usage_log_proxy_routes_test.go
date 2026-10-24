package schema

import (
	"testing"

	"entgo.io/ent/dialect"
	"entgo.io/ent/schema/field"
	"github.com/stretchr/testify/require"
)

func TestUsageLogProxyRoutesNullableJSONB(t *testing.T) {
	for _, f := range (UsageLog{}).Fields() {
		d := f.Descriptor()
		if d.Name != "proxy_routes" {
			continue
		}
		require.Equal(t, field.TypeJSON, d.Info.Type)
		require.True(t, d.Optional, "historical rows must remain NULL rather than be inferred from current account routing")
		require.Nil(t, d.Default)
		require.Equal(t, "jsonb", d.SchemaType[dialect.Postgres])
		return
	}
	t.Fatal("usage log schema does not persist proxy route snapshots")
}
