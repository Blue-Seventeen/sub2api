package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	grokVideoPendingBillingTTL = 24 * time.Hour
	grokVideoBilledClaimTTL    = 48 * time.Hour
)

func grokVideoPendingBillingKey(requestID string, userID, apiKeyID int64) string {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" || userID <= 0 || apiKeyID <= 0 {
		return ""
	}
	return fmt.Sprintf("%d:%d:%s", userID, apiKeyID, requestID)
}

func (s *OpenAIGatewayService) StoreGrokVideoPendingBilling(ctx context.Context, requestID string, userID, apiKeyID int64, pending GrokVideoPendingBilling) error {
	if s == nil || s.cache == nil {
		return fmt.Errorf("grok video pending billing cache is unavailable")
	}
	key := grokVideoPendingBillingKey(requestID, userID, apiKeyID)
	if key == "" {
		return fmt.Errorf("grok video pending billing key is invalid")
	}
	pending.Model = strings.TrimSpace(pending.Model)
	pending.BillingModel = strings.TrimSpace(pending.BillingModel)
	pending.UpstreamModel = strings.TrimSpace(pending.UpstreamModel)
	pending.OriginalModel = strings.TrimSpace(pending.OriginalModel)
	if pending.VideoResolution != "" {
		pending.VideoResolution = NormalizeVideoBillingResolutionOrDefault(pending.VideoResolution)
	}
	if pending.VideoDurationSeconds > 0 {
		pending.VideoDurationSeconds = NormalizeVideoBillingDurationSecondsOrDefault(pending.VideoDurationSeconds)
	}
	pending.CreatedAt = strings.TrimSpace(pending.CreatedAt)
	if pending.CreatedAt == "" {
		pending.CreatedAt = GrokVideoPendingCreatedAtNow()
	}
	payload, err := json.Marshal(pending)
	if err != nil {
		return err
	}
	return s.cache.SetGrokVideoPendingBilling(ctx, key, payload, grokVideoPendingBillingTTL)
}

func (s *OpenAIGatewayService) LoadGrokVideoPendingBilling(ctx context.Context, requestID string, userID, apiKeyID int64) (*GrokVideoPendingBilling, error) {
	if s == nil || s.cache == nil {
		return nil, fmt.Errorf("grok video pending billing cache is unavailable")
	}
	key := grokVideoPendingBillingKey(requestID, userID, apiKeyID)
	if key == "" {
		return nil, fmt.Errorf("grok video pending billing key is invalid")
	}
	payload, err := s.cache.GetGrokVideoPendingBilling(ctx, key)
	if err != nil || len(payload) == 0 {
		return nil, err
	}
	var pending GrokVideoPendingBilling
	if err := json.Unmarshal(payload, &pending); err != nil {
		return nil, err
	}
	return &pending, nil
}

func (s *OpenAIGatewayService) ClaimGrokVideoBilling(ctx context.Context, requestID string, userID, apiKeyID int64) (bool, error) {
	if s == nil || s.cache == nil {
		return false, fmt.Errorf("grok video billing claim cache is unavailable")
	}
	key := grokVideoPendingBillingKey(requestID, userID, apiKeyID)
	if key == "" {
		return false, fmt.Errorf("grok video billing claim key is invalid")
	}
	return s.cache.ClaimGrokVideoBilled(ctx, key, grokVideoBilledClaimTTL)
}

func (s *OpenAIGatewayService) ReleaseGrokVideoBilling(ctx context.Context, requestID string, userID, apiKeyID int64) error {
	if s == nil || s.cache == nil {
		return fmt.Errorf("grok video billing claim cache is unavailable")
	}
	key := grokVideoPendingBillingKey(requestID, userID, apiKeyID)
	if key == "" {
		return fmt.Errorf("grok video billing claim key is invalid")
	}
	return s.cache.ReleaseGrokVideoBilled(ctx, key)
}

// Use the task identity, not a polling request ID, for durable billing deduplication.
func StableGrokVideoBillingRequestID(taskRequestID string) string {
	taskRequestID = strings.TrimSpace(taskRequestID)
	if taskRequestID == "" || strings.HasPrefix(taskRequestID, "grok-video:") {
		return taskRequestID
	}
	return "grok-video:" + taskRequestID
}
