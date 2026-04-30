package repository

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestOpsInsertErrorLogArgs_IncludesNetworkErrorType(t *testing.T) {
	input := &service.OpsInsertErrorLogInput{
		ErrorPhase:       "network",
		ErrorType:        "invalid_request_error",
		NetworkErrorType: "request_body_timeout",
		CreatedAt:        time.Now().UTC(),
	}

	args := opsInsertErrorLogArgs(input)

	require.Len(t, args, 48)
	require.Contains(t, insertOpsErrorLogSQL, "network_error_type")
	got, ok := args[35].(sql.NullString)
	require.True(t, ok)
	require.True(t, got.Valid)
	require.Equal(t, "request_body_timeout", got.String)
	require.Equal(t, 48, strings.Count(insertOpsErrorLogSQL, "$"))
}
