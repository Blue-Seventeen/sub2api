package service

import (
	"encoding/json"
	"net/http"
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestApplyGlobalModelOperations_PreservesWildcardAndUsesLastOperationWins(t *testing.T) {
	config := GroupModelsListConfig{
		Enabled: true,
		Models:  []string{"GPT-4o", "gpt-*", "claude-3"},
	}
	operations := []GroupModelOperation{
		{Operation: "remove", Model: "gpt-4o"},
		{Operation: "add", Model: "gpt-4o"},
		{Operation: "remove", Model: "GPT-4O"},
	}

	updated, changed, added, removed, err := ApplyGlobalModelOperations(config, operations)

	require.NoError(t, err)
	require.True(t, changed)
	require.True(t, updated.Enabled)
	require.Equal(t, []string{"gpt-*", "claude-3"}, updated.Models)
	require.Empty(t, added)
	require.Equal(t, []string{"GPT-4o"}, removed)
}

func TestApplyGlobalModelOperations_RemovesWildcardOnlyWhenRequestedExactly(t *testing.T) {
	config := GroupModelsListConfig{Enabled: true, Models: []string{"gpt-*", "claude-3"}}

	updated, _, _, removed, err := ApplyGlobalModelOperations(config, []GroupModelOperation{{Operation: "remove", Model: "gpt-*"}})

	require.NoError(t, err)
	require.Equal(t, []string{"claude-3"}, updated.Models)
	require.Equal(t, []string{"gpt-*"}, removed)
}

func TestApplyGlobalModelOperations_AddsExactModelWithoutMatchingWildcard(t *testing.T) {
	config := GroupModelsListConfig{Enabled: false, Models: []string{"gpt-*"}}

	updated, changed, added, removed, err := ApplyGlobalModelOperations(config, []GroupModelOperation{
		{Operation: "add", Model: "GPT-4o"},
	})

	require.NoError(t, err)
	require.True(t, changed)
	require.False(t, updated.Enabled)
	require.Equal(t, []string{"gpt-*", "GPT-4o"}, updated.Models)
	require.Equal(t, []string{"GPT-4o"}, added)
	require.Empty(t, removed)
}

func TestApplyGlobalModelOperations_RejectsUnknownOperation(t *testing.T) {
	_, _, _, _, err := ApplyGlobalModelOperations(GroupModelsListConfig{}, []GroupModelOperation{{Operation: "replace", Model: "gpt-4o"}})

	appErr := infraerrors.FromError(err)
	require.Equal(t, int32(http.StatusBadRequest), appErr.Code)
	require.Equal(t, "INVALID_GLOBAL_MODEL_OPERATION", appErr.Reason)
}

func TestApplyGlobalModelOperations_RejectsEmptyModelAsBadRequest(t *testing.T) {
	_, _, _, _, err := ApplyGlobalModelOperations(GroupModelsListConfig{}, []GroupModelOperation{{Operation: "add", Model: " "}})

	appErr := infraerrors.FromError(err)
	require.Equal(t, int32(http.StatusBadRequest), appErr.Code)
	require.Equal(t, "INVALID_GLOBAL_MODEL_OPERATION", appErr.Reason)
}

func TestGlobalModelOperationSummaryDoesNotMarshalAffectedGroupIDs(t *testing.T) {
	body, err := json.Marshal(GlobalModelOperationSummary{AffectedGroupIDs: []int64{4, 9}})

	require.NoError(t, err)
	require.NotContains(t, string(body), "affected_group_ids")
}
