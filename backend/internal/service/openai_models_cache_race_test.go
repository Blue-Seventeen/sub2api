package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestOpenAIModelsRefreshRechecksCacheAfterPriorMiss(t *testing.T) {
	for _, stale := range []bool{false, true} {
		name := "fresh"
		if stale {
			name = "stale"
		}
		t.Run(name, func(t *testing.T) {
			svc := &OpenAIGatewayService{}
			key := "shared-catalog"
			_, state := svc.openAIModelsCache.get(key, time.Now())
			require.Equal(t, openAIModelsCacheMiss, state)
			cached := &OpenAIModelsResponse{Body: []byte(`{"data":[{"id":"cached"}]}`), upstreamETag: "upstream-etag"}
			storedAt := time.Now()
			if stale {
				storedAt = storedAt.Add(-openAIModelsCacheTTL - time.Second)
			}
			// Another caller completed the cold fetch before this caller joined
			// singleflight. A fresh result must suppress a redundant fetch.
			svc.openAIModelsCache.set(key, cached, storedAt)
			calls := 0
			result := <-svc.refreshCachedOpenAIModels(key, openAIModelsRequest{}, func(_ context.Context, etag string) (*OpenAIModelsResponse, error) {
				calls++
				return &OpenAIModelsResponse{Body: []byte(`{"data":[]}`)}, nil
			})
			require.NoError(t, result.Err)
			if stale {
				require.Equal(t, 1, calls, "stale or expired results still refresh")
			} else {
				require.Zero(t, calls)
				cachedResult, ok := result.Val.(*OpenAIModelsResponse)
				require.True(t, ok)
				require.Equal(t, cached.Body, cachedResult.Body)
			}
		})
	}
}
