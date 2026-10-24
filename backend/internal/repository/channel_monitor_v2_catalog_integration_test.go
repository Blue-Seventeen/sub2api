//go:build integration

package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestChannelMonitorV2ObservedCatalogPostgresBoundary(t *testing.T) {
	ctx := context.Background()
	var groupID int64
	err := integrationDB.QueryRowContext(ctx, `INSERT INTO groups (name,platform,status,models_list_config)
		VALUES ($1,'openai','active','{"enabled":true,"models":["catalog-*"]}') RETURNING id`,
		fmt.Sprintf("catalog-boundary-%d", time.Now().UnixNano())).Scan(&groupID)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, cleanupErr := integrationDB.ExecContext(ctx, `DELETE FROM channel_monitor_v2_metrics_rollup WHERE group_id=$1`, groupID)
		require.NoError(t, cleanupErr)
		_, cleanupErr = integrationDB.ExecContext(ctx, `DELETE FROM groups WHERE id=$1`, groupID)
		require.NoError(t, cleanupErr)
	})
	day := time.Now().UTC().Truncate(24 * time.Hour)
	start := day.Add(3*time.Hour + 21*time.Minute)
	end := start.Add(17 * time.Minute)
	for _, seconds := range []int{300, 3600, 43200, 86400} {
		bucket := day.Add(time.Duration(int(start.Sub(day).Seconds())/seconds*seconds) * time.Second)
		_, err = integrationDB.ExecContext(ctx, `INSERT INTO channel_monitor_v2_metrics_rollup
			(bucket_start,bucket_seconds,platform,group_id,model,success_requests) VALUES ($1,$2,'openai',$3,'catalog-observed',1)`, bucket, seconds, groupID)
		require.NoError(t, err)
	}
	// End-at-start, future, and forbidden names must not enter the directory.
	for _, row := range []struct {
		at    time.Time
		model string
	}{
		{start.Add(-24 * time.Hour), "catalog-expired"},
		{end.Add(time.Hour), "catalog-future"},
		{day, "forbidden"},
		{day, "catalog-*"},
	} {
		_, err = integrationDB.ExecContext(ctx, `INSERT INTO channel_monitor_v2_metrics_rollup
			(bucket_start,bucket_seconds,platform,group_id,model,success_requests) VALUES ($1,86400,'openai',$2,$3,1)`, row.at, groupID, row.model)
		require.NoError(t, err)
	}
	repo := &channelMonitorV2Repository{db: integrationDB}
	ctx, filter, cfg, err := repo.resolveChannelMonitorV2Scope(ctx,
		service.ChannelMonitorV2Filter{Start: start, End: end}, service.ChannelMonitorV2Config{GroupIDs: []int64{groupID}})
	require.NoError(t, err)
	require.NoError(t, repo.expandChannelMonitorV2ObservedCatalog(ctx, filter, &cfg))
	require.Len(t, cfg.Platforms, 1)
	require.Equal(t, []string{"catalog-observed"}, cfg.Platforms[0].Models)
}
