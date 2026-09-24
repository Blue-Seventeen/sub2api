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
	require.JSONEq(t, `{"target_platform":"","affected_group_count":0,"added_models":[],"removed_models":[]}`, string(body))
}

func TestApplyGlobalModelOperationsValidatesFinalPolicy(t *testing.T) {
	for _, tc := range []struct {
		name       string
		config     GroupModelsListConfig
		operations []GroupModelOperation
	}{
		{"invalid addition", GroupModelsListConfig{}, []GroupModelOperation{{Operation: "add", Model: "foo*bar"}}},
		{"remove last enabled model", GroupModelsListConfig{Enabled: true, Models: []string{"only"}}, []GroupModelOperation{{Operation: "remove", Model: "ONLY"}}},
		{"enabled empty no-op", GroupModelsListConfig{Enabled: true}, nil},
		{"existing invalid pattern", GroupModelsListConfig{Models: []string{"foo*bar"}}, []GroupModelOperation{{Operation: "add", Model: "valid"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, changed, _, _, err := ApplyGlobalModelOperations(tc.config, tc.operations)
			require.Error(t, err)
			require.Equal(t, int32(http.StatusBadRequest), infraerrors.FromError(err).Code)
			require.False(t, changed)
		})
	}
}

func TestApplyGlobalModelOperationsValidatesAfterOrderedOperations(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		config := GroupModelsListConfig{Enabled: enabled, Models: []string{"before"}}
		updated, changed, added, removed, err := ApplyGlobalModelOperations(config, []GroupModelOperation{
			{Operation: "remove", Model: "BEFORE"},
			{Operation: "add", Model: " after* "},
		})
		require.NoError(t, err)
		require.True(t, changed)
		require.Equal(t, GroupModelsListConfig{Enabled: enabled, Models: []string{"after*"}}, updated)
		require.Equal(t, []string{"after*"}, added)
		require.Equal(t, []string{"before"}, removed)
		require.Equal(t, []string{"before"}, config.Models, "input must not be mutated")
	}
}

func TestApplyGlobalModelOperationsDisabledEmptyResultHasArrayEffects(t *testing.T) {
	updated, changed, added, removed, err := ApplyGlobalModelOperations(GroupModelsListConfig{}, nil)
	require.NoError(t, err)
	require.False(t, updated.Enabled)
	require.False(t, changed)
	require.Equal(t, []string{}, added)
	require.Equal(t, []string{}, removed)
}
