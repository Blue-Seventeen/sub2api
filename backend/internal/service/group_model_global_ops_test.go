package service

import (
	"testing"

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

	require.EqualError(t, err, "unsupported global model operation: replace")
}
