package service

import (
	"encoding/json"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestChannelMonitorV2BucketHealthKeepsConfiguredSampleTarget(t *testing.T) {
	thresholds := DefaultChannelMonitorV2HealthThresholds()
	thresholds.MinimumSample = 50
	p50 := int64(100)
	for _, n := range []int64{0, 1, 49, 50} {
		t.Run(fmtBucketSample(n), func(t *testing.T) {
			m := ChannelMonitorV2Metric{RequestCount: n, SuccessRequests: n, TTFT: ChannelMonitorV2Latency{SampleCount: n, P50Ms: &p50}, CacheRateDenominator: n, CacheRate: 0.5}
			h := ChannelMonitorV2BucketHealthForWithThresholds(m, thresholds)
			require.Equal(t, int64(50), h.MinimumSample)
			require.Equal(t, thresholds, h.Thresholds)
			require.Equal(t, n > 0 && n < 50, h.Provisional)
			if n == 0 {
				require.Nil(t, h.Score)
				require.Equal(t, "unknown", h.Overall)
			} else {
				require.NotNil(t, h.Score)
				require.Equal(t, "healthy", h.Overall)
			}
			summary := ChannelMonitorV2HealthForWithThresholds(m, thresholds)
			if n < 50 {
				require.Nil(t, summary.Score)
				require.Equal(t, "unknown", summary.Overall)
			}
		})
	}
}

func fmtBucketSample(n int64) string {
	return map[int64]string{0: "zero", 1: "one", 49: "forty-nine", 50: "fifty"}[n]
}

func TestChannelMonitorV2BucketMissingTTFTStaysUnknownAndSurvivesRedaction(t *testing.T) {
	m := ChannelMonitorV2Metric{RequestCount: 1, SuccessRequests: 1}
	h := ChannelMonitorV2BucketHealthForWithThresholds(m, DefaultChannelMonitorV2HealthThresholds())
	require.Equal(t, "unknown", h.TTFT)
	require.Nil(t, h.TTFTScore)
	require.True(t, h.Provisional)
	require.NotNil(t, h.Score)
	snap := &ChannelMonitorV2Snapshot{Metrics: m, Trend: []ChannelMonitorV2TrendPoint{{Metrics: m, Health: h}}}
	redactChannelMonitorV2Snapshot(snap, true)
	require.Zero(t, snap.Trend[0].Metrics.RequestCount)
	require.Zero(t, snap.Trend[0].Metrics.RPM)
	require.True(t, snap.Trend[0].Health.Provisional)
	b, err := json.Marshal(snap.Trend[0].Health)
	require.NoError(t, err)
	require.Contains(t, string(b), `"provisional":true`)
}

func TestChannelMonitorV2ConfigLegacyCatalogIsReadonly(t *testing.T) {
	cfg := ChannelMonitorV2Config{Platforms: []ChannelMonitorV2PlatformConfig{{Platform: "", Enabled: true, Models: []string{"injected"}}, {Platform: "", Enabled: false}}}
	require.NoError(t, normalizeChannelMonitorV2Config(&cfg))
	require.Empty(t, cfg.Platforms)
}

func TestChannelMonitorV2MetricDataMarkerSurvivesAllVolumeRedaction(t *testing.T) {
	m := ChannelMonitorV2Metric{HasData: true, RequestCount: 1, SuccessRequests: 1, RPM: 1, TPM: 12, TokenCount: 12}
	health := ChannelMonitorV2BucketHealthForWithThresholds(m, DefaultChannelMonitorV2HealthThresholds())
	snap := &ChannelMonitorV2Snapshot{Metrics: m, Health: ChannelMonitorV2HealthFor(m), Trend: []ChannelMonitorV2TrendPoint{{Metrics: m, Health: health}}}
	redactChannelMonitorV2Snapshot(snap, true)
	require.True(t, snap.Metrics.HasData)
	require.True(t, snap.Trend[0].Metrics.HasData)
	require.Zero(t, snap.Metrics.RequestCount)
	require.Zero(t, snap.Metrics.TokenCount)
	require.Zero(t, snap.Metrics.RPM)
	require.Zero(t, snap.Metrics.TPM)
	require.True(t, snap.Trend[0].Health.Provisional)
	require.NotNil(t, snap.Trend[0].Health.Score)
	require.Equal(t, "unknown", snap.Health.Overall)
	b, err := json.Marshal(ChannelMonitorV2Metric{})
	require.NoError(t, err)
	require.Contains(t, string(b), `"has_data":false`)
}

func TestChannelMonitorV2BucketPartialSignalsRetainCustomThresholds(t *testing.T) {
	thresholds := DefaultChannelMonitorV2HealthThresholds()
	thresholds.MinimumSample = 100
	p50 := int64(100)
	m := ChannelMonitorV2Metric{RequestCount: 100, TTFT: ChannelMonitorV2Latency{SampleCount: 1, P50Ms: &p50}}
	h := ChannelMonitorV2BucketHealthForWithThresholds(m, thresholds)
	require.True(t, h.Provisional)
	require.NotNil(t, h.TTFTScore)
	require.Equal(t, int64(100), h.MinimumSample)
	require.Equal(t, thresholds, h.Thresholds)
	summary := ChannelMonitorV2HealthForWithThresholds(m, thresholds)
	require.False(t, summary.Provisional)
	require.Nil(t, summary.TTFTScore)
	require.Equal(t, "unknown", summary.TTFT)
}

func TestChannelMonitorV2BucketProvisionalDoesNotRequireBlendedScore(t *testing.T) {
	thresholds := DefaultChannelMonitorV2HealthThresholds()
	thresholds.MinimumSample = 50
	thresholds.ErrorWeight = 0
	thresholds.TTFTWeight = 1
	thresholds.CacheWeight = 0
	metrics := ChannelMonitorV2Metric{RequestCount: 1, SuccessRequests: 1}
	health := ChannelMonitorV2BucketHealthForWithThresholds(metrics, thresholds)
	require.Nil(t, health.Score)
	require.Equal(t, "unknown", health.Overall)
	require.NotNil(t, health.ErrorRateScore)
	require.Equal(t, float64(100), *health.ErrorRateScore)
	require.Nil(t, health.TTFTScore)
	require.Equal(t, "unknown", health.TTFT)
	require.Nil(t, health.CacheScore)
	require.True(t, health.Provisional, "an available low-sample subscore must be provisional even with zero blended weight")
	require.Equal(t, thresholds, health.Thresholds)
	require.Equal(t, int64(50), health.MinimumSample)
	summary := ChannelMonitorV2HealthForWithThresholds(metrics, thresholds)
	require.False(t, summary.Provisional)
	require.Nil(t, summary.ErrorRateScore)
	require.Nil(t, summary.Score)
	require.Equal(t, "unknown", summary.Overall)
}
