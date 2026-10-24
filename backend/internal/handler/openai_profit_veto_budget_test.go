package handler

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRecordOpenAIProfitVetoExcludesEveryUniqueCandidate(t *testing.T) {
	failed := make(map[int64]struct{})
	count := 0

	for i := int64(1); i <= 15; i++ {
		require.True(t, recordOpenAIProfitVeto(failed, i, &count), "第 %d 次否决应允许继续重选", i)
		require.Contains(t, failed, i, "否决的账号必须进入本请求排除集")
	}

	require.Equal(t, 15, count)
	require.Len(t, failed, 15)
}

func TestOpenAIProfitVetoRejectsRepeatedExcludedCandidate(t *testing.T) {
	failed := make(map[int64]struct{})
	count := 0
	require.True(t, recordOpenAIProfitVeto(failed, 91, &count))
	require.False(t, recordOpenAIProfitVeto(failed, 91, &count), "a selector repeating an excluded ID must not spin")
	require.Equal(t, 1, count)
	require.Len(t, failed, 1)
}

func TestProfitVetoUniqueExclusionsSharedWithFailoverState(t *testing.T) {
	fs := NewFailoverState(10, false)
	failed := make(map[int64]struct{})
	count := 0

	for i := int64(1); i <= 15; i++ {
		require.Equal(t, FailoverContinue, fs.RecordProfitVeto(i))
		require.True(t, recordOpenAIProfitVeto(failed, i, &count))
	}

	require.Equal(t, 15, fs.ProfitVetoCount())
	require.Equal(t, fs.ProfitVetoCount(), count)
	require.Equal(t, fs.FailedAccountIDs, failed)
	require.Equal(t, FailoverExhausted, fs.RecordProfitVeto(15))
	require.False(t, recordOpenAIProfitVeto(failed, 15, &count))
}
