package service

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
)

// GetGroupStats returns the current API key and cumulative usage statistics for
// one group. It stays outside the public AdminService interface so older
// handler test doubles remain source-compatible during the upgrade.
func (s *adminServiceImpl) GetGroupStats(ctx context.Context, groupID int64) (map[string]any, error) {
	if groupID <= 0 {
		return nil, ErrGroupNotFound
	}
	if s == nil || s.entClient == nil || s.usageService == nil {
		return nil, ErrServiceUnavailable
	}

	var totalAPIKeys, activeAPIKeys int64
	rows, err := s.entClient.QueryContext(ctx, `
		SELECT
			COUNT(k.id) AS total_api_keys,
			COUNT(k.id) FILTER (WHERE k.status = $2) AS active_api_keys
		FROM groups g
		LEFT JOIN api_keys k ON k.group_id = g.id AND k.deleted_at IS NULL
		WHERE g.id = $1 AND g.deleted_at IS NULL
		GROUP BY g.id
	`, groupID, StatusActive)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, err
		}
		return nil, ErrGroupNotFound
	}
	if err := rows.Scan(&totalAPIKeys, &activeAPIKeys); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	usage, err := s.usageService.GetStatsWithFilters(ctx, usagestats.UsageLogFilters{
		GroupID: groupID,
	})
	if err != nil {
		return nil, err
	}
	if usage == nil {
		usage = &usagestats.UsageStats{}
	}

	return map[string]any{
		"total_api_keys":         totalAPIKeys,
		"active_api_keys":        activeAPIKeys,
		"total_requests":         usage.TotalRequests,
		"total_cost":             usage.TotalCost,
		"total_actual_cost":      usage.TotalActualCost,
		"real_total_actual_cost": usage.RealTotalActualCost,
		"total_tokens":           usage.TotalTokens,
		"avg_duration_ms":        usage.AverageDurationMs,
	}, nil
}
