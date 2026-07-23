package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func newGatewayImageUsageRecordServiceForTest(usageRepo UsageLogRepository, userRepo UserRepository, subRepo UserSubscriptionRepository) *GatewayService {
	cfg := &config.Config{}
	cfg.Default.RateMultiplier = 1.1
	return NewGatewayService(
		nil,
		nil,
		usageRepo,
		nil,
		userRepo,
		subRepo,
		nil,
		nil,
		cfg,
		nil,
		nil,
		NewBillingService(cfg, nil),
		nil,
		&BillingCacheService{},
		nil,
		nil,
		&DeferredService{},
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil, // balanceNotifyService
		nil, // proxyStatsRepo
		nil, // userPlatformQuotaRepo
	)
}

func TestGatewayServiceRecordUsage_PersistsImageBillingMetadata(t *testing.T) {
	tests := []struct {
		name         string
		imageSize    string
		inputSize    string
		price1K      float64
		price2K      float64
		expectedCost float64
	}{
		{
			name:         "1k",
			imageSize:    ImageBillingSize1K,
			inputSize:    "1024x1024",
			price1K:      0.12,
			price2K:      0.25,
			expectedCost: 0.12,
		},
		{
			name:         "2k",
			imageSize:    ImageBillingSize2K,
			inputSize:    "2048x2048",
			price1K:      0.12,
			price2K:      0.25,
			expectedCost: 0.25,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			groupID := int64(1203)
			usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
			svc := newGatewayImageUsageRecordServiceForTest(
				usageRepo,
				&openAIRecordUsageUserRepoStub{},
				&openAIRecordUsageSubRepoStub{},
			)

			err := svc.RecordUsage(context.Background(), &RecordUsageInput{
				Result: &ForwardResult{
					RequestID:        "gateway_qwen_image_size_metadata",
					Model:            "qwen-image",
					ImageCount:       1,
					ImageSize:        tt.imageSize,
					ImageInputSize:   tt.inputSize,
					BillableUnitType: BillableUnitTypeImage,
					Duration:         time.Second,
				},
				APIKey: &APIKey{
					ID:      11203,
					GroupID: i64p(groupID),
					Group: &Group{
						ID:             groupID,
						RateMultiplier: 1.0,
						ImagePrice1K:   &tt.price1K,
						ImagePrice2K:   &tt.price2K,
					},
				},
				User:    &User{ID: 21203},
				Account: &Account{ID: 31203, Platform: PlatformAli},
			})

			require.NoError(t, err)
			require.NotNil(t, usageRepo.lastLog)
			require.Equal(t, 1, usageRepo.lastLog.ImageCount)
			require.NotNil(t, usageRepo.lastLog.ImageSize)
			require.Equal(t, tt.imageSize, *usageRepo.lastLog.ImageSize)
			require.NotNil(t, usageRepo.lastLog.ImageInputSize)
			require.Equal(t, tt.inputSize, *usageRepo.lastLog.ImageInputSize)
			require.Nil(t, usageRepo.lastLog.ImageOutputSize)
			require.NotNil(t, usageRepo.lastLog.ImageSizeSource)
			require.Equal(t, ImageSizeSourceInput, *usageRepo.lastLog.ImageSizeSource)
			require.Nil(t, usageRepo.lastLog.ImageSizeBreakdown)
			require.InDelta(t, tt.expectedCost, usageRepo.lastLog.TotalCost, 1e-12)
			require.InDelta(t, tt.expectedCost, usageRepo.lastLog.ActualCost, 1e-12)
		})
	}
}
