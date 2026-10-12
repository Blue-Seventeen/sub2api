package service

import (
	"context"
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
)

// UsageIPOptionsRepository is optional so unrelated consumers need not implement candidates.
type UsageIPOptionsRepository interface {
	GetIPOptionsWithFilters(context.Context, usagestats.UsageLogFilters, string, int, int) (*usagestats.UsageIPOptions, error)
}

func (s *UsageService) GetIPOptionsWithFilters(ctx context.Context, filters usagestats.UsageLogFilters, search string, offset, limit int) (*usagestats.UsageIPOptions, error) {
	repo, ok := s.usageRepo.(UsageIPOptionsRepository)
	if !ok {
		return nil, fmt.Errorf("usage IP options are not supported by this repository")
	}
	filters.IPAddresses = nil
	return repo.GetIPOptionsWithFilters(ctx, filters, search, offset, limit)
}
