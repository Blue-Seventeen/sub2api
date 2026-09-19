package service

import "context"

// ListPlazaGroups preserves the pre-v0.2.4 service entry point while routing
// aggregation through ModelPlazaService.
func (s *ChannelService) ListPlazaGroups(ctx context.Context) ([]PlazaGroup, error) {
	if s == nil {
		return nil, nil
	}
	return NewModelPlazaService(s.repo, s.groupRepo, s.pricingService, nil, nil).ListGroups(ctx)
}
