package migrations

import (
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCompositeModelRoutesMigrationAllowsCompatiblePlatforms(t *testing.T) {
	content, err := FS.ReadFile("172_composite_model_routes.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "composite_model_routes_target_platform_check")
	for _, platform := range []string{
		service.PlatformAnthropic,
		service.PlatformOpenAI,
		service.PlatformGemini,
		service.PlatformAntigravity,
		service.PlatformGrok,
	} {
		require.Contains(t, sql, "'"+platform+"'")
	}
	for _, platform := range service.CompatiblePlatforms() {
		require.Contains(t, sql, "'"+platform+"'")
	}
}
