package service

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

type mixedBatchUsageAccountRepo struct {
	AccountRepository
}

func (*mixedBatchUsageAccountRepo) GetByIDs(_ context.Context, ids []int64) ([]*Account, error) {
	accounts := make([]*Account, 0, len(ids)/2)
	for _, id := range ids {
		if id%2 == 0 {
			accounts = append(accounts, &Account{ID: id, Platform: PlatformOpenAI, Type: AccountTypeAPIKey})
		}
	}
	return accounts, nil
}

func TestAccountUsageBatchMixedMissingAndFailures(t *testing.T) {
	svc := NewAccountUsageService(&mixedBatchUsageAccountRepo{}, nil, nil, nil, nil, nil, nil, nil, NewUsageCache(), nil, nil)
	ids := make([]int64, 100)
	for i := range ids {
		ids[i] = int64(i + 1)
	}
	for attempt := 0; attempt < 20; attempt++ {
		usage, failures, err := svc.GetUsageBatch(context.Background(), ids, false)
		require.NoError(t, err)
		require.Empty(t, usage)
		require.Len(t, failures, len(ids))
		for _, id := range ids {
			if id%2 == 0 {
				require.Contains(t, failures[id], "does not support usage query", fmt.Sprint(id))
			} else {
				require.Contains(t, failures[id], "account not found", fmt.Sprint(id))
			}
		}
	}
}
