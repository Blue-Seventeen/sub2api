package handler

import (
	"encoding/json"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSanitizePaymentOrderForResponseHidesProviderInstanceID(t *testing.T) {
	t.Parallel()

	providerInstanceID := "internal-provider-instance"
	order := &dbent.PaymentOrder{
		ID:                 1,
		UserID:             2,
		Amount:             10,
		PayAmount:          10,
		FeeRate:            0,
		PaymentType:        "alipay",
		OutTradeNo:         "sub2_order",
		Status:             service.OrderStatusPending,
		OrderType:          "balance",
		CreatedAt:          time.Now(),
		ExpiresAt:          time.Now().Add(time.Hour),
		ProviderInstanceID: &providerInstanceID,
	}

	out := sanitizePaymentOrderForResponse(order)
	require.NotNil(t, out)
	require.Nil(t, out.ProviderInstanceID)
	require.False(t, out.CanRequestRefund)

	body, err := json.Marshal(out)
	require.NoError(t, err)
	require.NotContains(t, string(body), "provider_instance_id")
	require.NotContains(t, string(body), providerInstanceID)
	require.Contains(t, string(body), "can_request_refund")
}
