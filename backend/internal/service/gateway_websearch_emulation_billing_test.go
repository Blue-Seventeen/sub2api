package service

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/websearch"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestEmulatedWebSearch_BillsUnpricedAliasWithoutTokenPricing(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "buffered", true: "streaming"}[stream], func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			response := &websearch.SearchResponse{Results: []websearch.SearchResult{{URL: "https://example.com", Title: "result"}}}
			var result *ForwardResult
			var err error
			if stream {
				result, err = writeWebSearchStreamResponse(c, "query", response, "public-search-alias", time.Now())
			} else {
				result, err = writeWebSearchNonStreamResponse(c, "query", response, "public-search-alias", time.Now())
			}
			require.NoError(t, err)
			require.Equal(t, 1, result.SearchCount)
			price := 20.0
			apiKey := &APIKey{Group: &Group{SearchPricePer1k: &price}}
			billing := NewBillingService(&config.Config{}, nil)
			t.Run("anthropic_gateway", func(t *testing.T) {
				svc := &GatewayService{billingService: billing}
				cost, err := svc.calculateRecordUsageCost(context.Background(), result, apiKey, result.Model, 2.5, 1, nil)
				require.NoError(t, err, "emulation performs no model request and must not require a token price")
				require.InDelta(t, 0.02, cost.TotalCost, 1e-12)
				require.InDelta(t, 0.05, cost.ActualCost, 1e-12)
			})
			t.Run("openai_gateway", func(t *testing.T) {
				svc := &OpenAIGatewayService{billingService: billing}
				forwarded := OpenAIForwardResultFromForwardResult(result)
				cost, err := svc.calculateOpenAIRecordUsageCostAt(context.Background(), forwarded, apiKey, []string{forwarded.Model},
					9, 1, 1, 2.5, UsageTokens{}, "", nil, false, time.Time{})
				require.NoError(t, err, "search billing must survive an unpriced client-visible alias")
				require.InDelta(t, 0.02, cost.TotalCost, 1e-12)
				require.InDelta(t, 0.05, cost.ActualCost, 1e-12, "emulation must use the search multiplier")
			})
		})
	}
}
