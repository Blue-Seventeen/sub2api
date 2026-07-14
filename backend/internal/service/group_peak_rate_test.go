package service

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
)

func init() {
	// 测试固定全局时区为 UTC，确保判定可复现。
	_ = timezone.Init("UTC")
}

func newPeakGroup(enabled bool, start, end string, mult float64) *Group {
	return &Group{
		SubscriptionType:   "subscription",
		PeakRateEnabled:    enabled,
		PeakStart:          start,
		PeakEnd:            end,
		PeakRateMultiplier: mult,
	}
}

func at(hour, min int) time.Time {
	return time.Date(2026, 6, 29, hour, min, 0, 0, time.UTC)
}

func TestPeakMultiplierAt_DisabledOrUnconfigured(t *testing.T) {
	cases := []struct {
		name string
		g    *Group
	}{
		{"disabled", newPeakGroup(false, "14:00", "18:00", 3.0)},
		{"empty start", newPeakGroup(true, "", "18:00", 3.0)},
		{"empty end", newPeakGroup(true, "14:00", "", 3.0)},
		{"invalid start>=end", newPeakGroup(true, "18:00", "14:00", 3.0)},
		{"equal start==end", newPeakGroup(true, "14:00", "14:00", 3.0)},
		{"malformed start", newPeakGroup(true, "99:99", "18:00", 3.0)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.g.PeakMultiplierAt(at(15, 0)); got != 1.0 {
				t.Fatalf("expect 1.0, got %v", got)
			}
		})
	}
}

func TestPeakMultiplierAt_NilReceiver(t *testing.T) {
	var g *Group
	if got := g.PeakMultiplierAt(at(15, 0)); got != 1.0 {
		t.Fatalf("expect 1.0, got %v", got)
	}
}

func TestPeakMultiplierAt_Boundaries(t *testing.T) {
	g := newPeakGroup(true, "14:00", "18:00", 3.0)
	cases := []struct {
		t    time.Time
		want float64
	}{
		{at(13, 59), 1.0},
		{at(14, 0), 3.0},
		{at(15, 30), 3.0},
		{at(17, 59), 3.0},
		{at(18, 0), 1.0},
		{at(23, 0), 1.0},
	}
	for _, c := range cases {
		t.Run(c.t.Format("15:04"), func(t *testing.T) {
			if got := g.PeakMultiplierAt(c.t); got != c.want {
				t.Fatalf("at %s: expect %v, got %v", c.t.Format("15:04"), c.want, got)
			}
		})
	}
}

func TestPeakMultiplierAt_MultipleWindows(t *testing.T) {
	g := &Group{
		SubscriptionType: "standard",
		PeakRateEnabled:  true,
		PeakRateWindows: []PeakRateWindow{
			{Start: "18:00", End: "22:00", Multiplier: 2.0},
			{Start: "09:00", End: "12:00", Multiplier: 1.5},
		},
	}
	cases := []struct {
		name string
		t    time.Time
		want float64
	}{
		{"before first window", at(8, 59), 1.0},
		{"first window start inclusive", at(9, 0), 1.5},
		{"first window inside", at(11, 59), 1.5},
		{"first window end exclusive", at(12, 0), 1.0},
		{"second window start inclusive", at(18, 0), 2.0},
		{"second window inside", at(21, 59), 2.0},
		{"second window end exclusive", at(22, 0), 1.0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := g.PeakMultiplierAt(c.t); got != c.want {
				t.Fatalf("at %s: expect %v, got %v", c.t.Format("15:04"), c.want, got)
			}
		})
	}
}

func TestPeakRateWindowsForRead_CanonicalizesLegacyClockFormat(t *testing.T) {
	g := &Group{
		SubscriptionType: "standard",
		PeakRateEnabled:  true,
		PeakRateWindows: []PeakRateWindow{
			{Start: "9:00", End: "12:00", Multiplier: 1.5},
		},
	}

	windows := PeakRateWindowsForRead(g.PeakRateWindows, g.PeakStart, g.PeakEnd, g.PeakRateMultiplier)
	if len(windows) != 1 || windows[0].Start != "09:00" || windows[0].End != "12:00" {
		t.Fatalf("expected canonical HH:MM window, got %+v", windows)
	}
	if got := g.PeakMultiplierAt(at(9, 30)); got != 1.5 {
		t.Fatalf("legacy clock window should remain billable: got %v, want 1.5", got)
	}
}

func TestPeakRateWindowsForRead_LegacyFirstWindowOverridesStaleJSON(t *testing.T) {
	g := &Group{
		SubscriptionType: "standard",
		PeakRateEnabled:  true,
		PeakRateWindows: []PeakRateWindow{
			{Start: "09:00", End: "12:00", Multiplier: 1.5},
			{Start: "18:00", End: "22:00", Multiplier: 2.0},
		},
		PeakStart:          "10:00",
		PeakEnd:            "11:00",
		PeakRateMultiplier: 1.8,
	}

	windows := PeakRateWindowsForRead(g.PeakRateWindows, g.PeakStart, g.PeakEnd, g.PeakRateMultiplier)
	if len(windows) != 2 || windows[0].Start != "10:00" || windows[0].End != "11:00" || windows[0].Multiplier != 1.8 {
		t.Fatalf("legacy first window should override stale JSON first window, got %+v", windows)
	}
	if got := g.PeakMultiplierAt(at(9, 30)); got != 1.0 {
		t.Fatalf("stale JSON first window should not apply: got %v, want 1.0", got)
	}
	if got := g.PeakMultiplierAt(at(10, 30)); got != 1.8 {
		t.Fatalf("legacy first window should apply: got %v, want 1.8", got)
	}
	if got := g.PeakMultiplierAt(at(19, 0)); got != 2.0 {
		t.Fatalf("non-overlapping second JSON window should remain: got %v, want 2.0", got)
	}
}

func TestPeakRateWindowsForRead_LegacyFirstWindowFallsBackWhenMergeOverlaps(t *testing.T) {
	windows := PeakRateWindowsForRead([]PeakRateWindow{
		{Start: "09:00", End: "12:00", Multiplier: 1.5},
		{Start: "18:00", End: "22:00", Multiplier: 2.0},
	}, "19:00", "20:00", 1.8)

	if len(windows) != 1 || windows[0].Start != "19:00" || windows[0].End != "20:00" || windows[0].Multiplier != 1.8 {
		t.Fatalf("overlapping rollback edit should fall back to legacy-only window, got %+v", windows)
	}
}

func TestPeakMultiplierAt_RespectsTimezoneLocation(t *testing.T) {
	// 全局时区为 UTC。北京 15:00 = UTC 07:00，不在 [14:00,18:00)。
	nonUTC := time.Date(2026, 6, 29, 15, 0, 0, 0, mustLoad("Asia/Shanghai"))
	g := newPeakGroup(true, "14:00", "18:00", 3.0)
	if got := g.PeakMultiplierAt(nonUTC); got != 1.0 {
		t.Fatalf("expect 1.0 (converted to UTC 07:00), got %v", got)
	}
}

func mustLoad(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

func TestValidatePeakRateConfig(t *testing.T) {
	cases := []struct {
		name    string
		subType string
		enabled bool
		start   string
		end     string
		mult    float64
		wantErr bool
	}{
		{"disabled passes through", "subscription", false, "", "", 0, false},
		{"subscription enabled valid", "subscription", true, "14:00", "18:00", 3.0, false},
		{"standard enabled accepted", "standard", true, "14:00", "18:00", 3.0, false},
		{"empty type accepted", "", true, "14:00", "18:00", 3.0, false},
		{"standard disabled passes", "standard", false, "", "", 0, false},
		{"enabled empty start", "subscription", true, "", "18:00", 1.0, true},
		{"enabled empty end", "subscription", true, "14:00", "", 1.0, true},
		{"enabled malformed start", "subscription", true, "99:99", "18:00", 1.0, true},
		{"enabled malformed end", "subscription", true, "14:00", "25:00", 1.0, true},
		{"enabled equal start==end", "subscription", true, "14:00", "14:00", 1.0, true},
		{"enabled cross-day rejected", "subscription", true, "22:00", "02:00", 1.0, true},
		{"enabled negative multiplier", "subscription", true, "14:00", "18:00", -0.5, true},
		{"enabled zero multiplier allowed", "subscription", true, "14:00", "18:00", 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidatePeakRateConfig(c.subType, c.enabled, c.start, c.end, c.mult)
			if c.wantErr && err == nil {
				t.Fatalf("expect error, got nil")
			}
			if !c.wantErr && err != nil {
				t.Fatalf("expect no error, got %v", err)
			}
		})
	}
}

func TestValidatePeakRateWindows(t *testing.T) {
	normalized, err := NormalizePeakRateWindows(true, []PeakRateWindow{
		{Start: "18:00", End: "22:00", Multiplier: 2.0},
		{Start: "09:00", End: "12:00", Multiplier: 1.5},
	})
	if err != nil {
		t.Fatalf("valid unsorted windows should pass: %v", err)
	}
	if len(normalized) != 2 || normalized[0].Start != "09:00" || normalized[1].Start != "18:00" {
		t.Fatalf("windows should be sorted by start time, got %+v", normalized)
	}

	validAdjacent := []PeakRateWindow{
		{Start: "09:00", End: "10:00", Multiplier: 1.5},
		{Start: "10:00", End: "11:00", Multiplier: 2.0},
	}
	if err := ValidatePeakRateWindows(true, validAdjacent); err != nil {
		t.Fatalf("adjacent [start,end) windows should be allowed: %v", err)
	}

	cases := []struct {
		name    string
		windows []PeakRateWindow
		wantErr bool
	}{
		{
			name: "overlap rejected",
			windows: []PeakRateWindow{
				{Start: "09:00", End: "10:30", Multiplier: 1.5},
				{Start: "10:00", End: "11:00", Multiplier: 2.0},
			},
			wantErr: true,
		},
		{
			name:    "cross-day rejected",
			windows: []PeakRateWindow{{Start: "22:00", End: "02:00", Multiplier: 1.5}},
			wantErr: true,
		},
		{
			name:    "negative multiplier rejected",
			windows: []PeakRateWindow{{Start: "09:00", End: "10:00", Multiplier: -0.1}},
			wantErr: true,
		},
		{
			name:    "zero multiplier allowed",
			windows: []PeakRateWindow{{Start: "09:00", End: "10:00", Multiplier: 0}},
			wantErr: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidatePeakRateWindows(true, c.windows)
			if c.wantErr && err == nil {
				t.Fatalf("expect error, got nil")
			}
			if !c.wantErr && err != nil {
				t.Fatalf("expect no error, got %v", err)
			}
		})
	}

	legacy, err := NormalizePeakRateWindows(true, []PeakRateWindow{
		{Start: "9:00", End: "10:00", Multiplier: 1.5},
	})
	if err != nil {
		t.Fatalf("single-digit legacy hour should pass: %v", err)
	}
	if len(legacy) != 1 || legacy[0].Start != "09:00" || legacy[0].End != "10:00" {
		t.Fatalf("legacy hour should be canonicalized, got %+v", legacy)
	}

	tooMany := make([]PeakRateWindow, MaxPeakRateWindows+1)
	for i := range tooMany {
		tooMany[i] = PeakRateWindow{Start: "09:00", End: "10:00", Multiplier: 1}
	}
	if err := ValidatePeakRateWindows(true, tooMany); err == nil {
		t.Fatalf("more than %d windows should be rejected", MaxPeakRateWindows)
	}
}

func TestPeakMultiplierAt_StandardTypeAppliesPeak(t *testing.T) {
	g := newPeakGroup(true, "14:00", "18:00", 3.0)
	g.SubscriptionType = "standard"
	if got := g.PeakMultiplierAt(at(15, 30)); got != 3.0 {
		t.Fatalf("standard group peak multiplier: got %v, want 3.0", got)
	}

	sub := newPeakGroup(true, "14:00", "18:00", 3.0)
	sub.SubscriptionType = "subscription"
	if got := sub.PeakMultiplierAt(at(15, 30)); got != 3.0 {
		t.Fatalf("subscription group peak multiplier: got %v, want 3.0", got)
	}
}

// TestPeakMultiplier_GatewayBillingSequence 调用 gateway_service.recordUsageCore 与
// openai_gateway_service.RecordUsage 共用的 computePeakAwareMultipliers，验证计费叠加顺序：
// 图片按次倍率基于基础倍率算出，并与 token 倍率一样叠加高峰因子。
// 若有人调换叠加顺序或遗漏高峰倍率，此测试会失败。
func TestPeakMultiplier_GatewayBillingSequence(t *testing.T) {
	const baseMultiplier = 0.8
	apiKey := &APIKey{Group: newPeakGroup(true, "14:00", "18:00", 3.0)}
	approxEq := func(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

	t.Run("peak hour amplifies token and image multipliers", func(t *testing.T) {
		now := at(15, 30) // 处于 [14:00, 18:00)
		tokenMultiplier, imageMultiplier := computePeakAwareMultipliers(apiKey, baseMultiplier, now)
		if want := baseMultiplier * 3.0; !approxEq(imageMultiplier, want) {
			t.Fatalf("image multiplier should include peak factor: got %v, want %v", imageMultiplier, want)
		}
		if want := baseMultiplier * 3.0; !approxEq(tokenMultiplier, want) {
			t.Fatalf("token multiplier should include peak factor: got %v, want %v", tokenMultiplier, want)
		}
	})

	t.Run("off-peak leaves both multipliers at base", func(t *testing.T) {
		now := at(20, 0)
		tokenMultiplier, imageMultiplier := computePeakAwareMultipliers(apiKey, baseMultiplier, now)
		if !approxEq(imageMultiplier, baseMultiplier) {
			t.Fatalf("image multiplier: got %v, want %v", imageMultiplier, baseMultiplier)
		}
		if !approxEq(tokenMultiplier, baseMultiplier) {
			t.Fatalf("token multiplier should equal base off-peak: got %v, want %v", tokenMultiplier, baseMultiplier)
		}
	})

	t.Run("image independent mode still applies peak", func(t *testing.T) {
		indGroup := newPeakGroup(true, "14:00", "18:00", 3.0)
		indGroup.ImageRateIndependent = true
		indGroup.ImageRateMultiplier = 0.5
		indKey := &APIKey{Group: indGroup}
		now := at(15, 30)
		tokenMultiplier, imageMultiplier := computePeakAwareMultipliers(indKey, baseMultiplier, now)
		if want := 0.5 * 3.0; !approxEq(imageMultiplier, want) {
			t.Fatalf("independent image multiplier should include peak factor: got %v, want %v", imageMultiplier, want)
		}
		if want := baseMultiplier * 3.0; !approxEq(tokenMultiplier, want) {
			t.Fatalf("token multiplier should include peak factor: got %v, want %v", tokenMultiplier, want)
		}
	})

	t.Run("nil api key degrades to base multipliers", func(t *testing.T) {
		now := at(15, 30)
		tokenMultiplier, imageMultiplier := computePeakAwareMultipliers(nil, baseMultiplier, now)
		if !approxEq(tokenMultiplier, baseMultiplier) {
			t.Fatalf("nil group token multiplier: got %v, want %v", tokenMultiplier, baseMultiplier)
		}
		if !approxEq(imageMultiplier, baseMultiplier) {
			t.Fatalf("nil group image multiplier: got %v, want %v", imageMultiplier, baseMultiplier)
		}
	})
}

func TestPeakMultiplier_AllBillingModesUsePeakAwareMultiplier(t *testing.T) {
	const baseMultiplier = 1.2
	const peakRate = 2.5
	apiKey := &APIKey{Group: &Group{
		SubscriptionType: "standard",
		PeakRateEnabled:  true,
		PeakRateWindows: []PeakRateWindow{{
			Start:      "14:00",
			End:        "18:00",
			Multiplier: peakRate,
		}},
	}}
	textMultiplier, imageMultiplier := computePeakAwareMultipliers(apiKey, baseMultiplier, at(15, 0))
	wantMultiplier := baseMultiplier * peakRate
	if math.Abs(textMultiplier-wantMultiplier) > 1e-9 {
		t.Fatalf("text multiplier = %v, want %v", textMultiplier, wantMultiplier)
	}
	if math.Abs(imageMultiplier-wantMultiplier) > 1e-9 {
		t.Fatalf("image multiplier = %v, want %v", imageMultiplier, wantMultiplier)
	}
	videoMultiplier := computePeakAwareVideoMultiplier(apiKey, baseMultiplier, at(15, 0))
	if math.Abs(videoMultiplier-wantMultiplier) > 1e-9 {
		t.Fatalf("video multiplier = %v, want %v", videoMultiplier, wantMultiplier)
	}
	apiKey.Group.VideoRateIndependent = true
	apiKey.Group.VideoRateMultiplier = 0.4
	videoMultiplier = computePeakAwareVideoMultiplier(apiKey, baseMultiplier, at(15, 0))
	if math.Abs(videoMultiplier-1.0) > 1e-9 {
		t.Fatalf("independent video multiplier = %v, want 1", videoMultiplier)
	}

	svc := &BillingService{}
	resolver := &ModelPricingResolver{}
	cases := []struct {
		name           string
		input          CostInput
		wantTotalCost  float64
		wantActualCost float64
	}{
		{
			name: "token",
			input: CostInput{
				Ctx:            context.Background(),
				Model:          "peak-token-model",
				Tokens:         UsageTokens{InputTokens: 10, OutputTokens: 5},
				RateMultiplier: textMultiplier,
				Resolver:       resolver,
				Resolved: &ResolvedPricing{
					Mode: BillingModeToken,
					BasePricing: &ModelPricing{
						InputPricePerToken:  0.001,
						OutputPricePerToken: 0.002,
					},
				},
			},
			wantTotalCost:  0.02,
			wantActualCost: 0.02 * wantMultiplier,
		},
		{
			name: "per request",
			input: CostInput{
				Ctx:            context.Background(),
				Model:          "peak-per-request-model",
				RequestCount:   3,
				RateMultiplier: textMultiplier,
				Resolver:       resolver,
				Resolved: &ResolvedPricing{
					Mode:                   BillingModePerRequest,
					DefaultPerRequestPrice: 0.04,
				},
			},
			wantTotalCost:  0.12,
			wantActualCost: 0.12 * wantMultiplier,
		},
		{
			name: "image",
			input: CostInput{
				Ctx:            context.Background(),
				Model:          "peak-image-model",
				RequestCount:   2,
				RateMultiplier: imageMultiplier,
				Resolver:       resolver,
				Resolved: &ResolvedPricing{
					Mode:                   BillingModeImage,
					DefaultPerRequestPrice: 0.10,
				},
			},
			wantTotalCost:  0.20,
			wantActualCost: 0.20 * wantMultiplier,
		},
		{
			name: "duration",
			input: CostInput{
				Ctx:             context.Background(),
				Model:           "peak-duration-model",
				DurationSeconds: 13,
				RateMultiplier:  textMultiplier,
				Resolver:        resolver,
				Resolved: &ResolvedPricing{
					Mode:                   BillingModeDuration,
					DefaultPerRequestPrice: 0.02,
				},
			},
			wantTotalCost:  0.26,
			wantActualCost: 0.26 * wantMultiplier,
		},
		{
			name: "character",
			input: CostInput{
				Ctx:            context.Background(),
				Model:          "peak-character-model",
				CharacterCount: 2500,
				RateMultiplier: textMultiplier,
				Resolver:       resolver,
				Resolved: &ResolvedPricing{
					Mode:                   BillingModeCharacter,
					DefaultPerRequestPrice: 0.03,
				},
			},
			wantTotalCost:  0.075,
			wantActualCost: 0.075 * wantMultiplier,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cost, err := svc.CalculateCostUnified(c.input)
			if err != nil {
				t.Fatalf("CalculateCostUnified error: %v", err)
			}
			if math.Abs(cost.TotalCost-c.wantTotalCost) > 1e-9 {
				t.Fatalf("total cost = %v, want %v", cost.TotalCost, c.wantTotalCost)
			}
			if math.Abs(cost.ActualCost-c.wantActualCost) > 1e-9 {
				t.Fatalf("actual cost = %v, want %v", cost.ActualCost, c.wantActualCost)
			}
		})
	}

	imagePrice2K := 0.20
	imageCost := svc.CalculateImageCost("peak-image-direct-model", "2K", 2, &ImagePriceConfig{
		Price2K: &imagePrice2K,
	}, imageMultiplier)
	if math.Abs(imageCost.TotalCost-0.40) > 1e-9 {
		t.Fatalf("direct image total cost = %v, want 0.40", imageCost.TotalCost)
	}
	if math.Abs(imageCost.ActualCost-0.40*wantMultiplier) > 1e-9 {
		t.Fatalf("direct image actual cost = %v, want %v", imageCost.ActualCost, 0.40*wantMultiplier)
	}
}

// TestPeakMultiplier_SnapshotRoundTrip prevents auth-cache snapshots from losing peak-rate fields.
func TestPeakMultiplier_SnapshotRoundTrip(t *testing.T) {
	apiKey := &APIKey{
		User: &User{ID: 1, Status: StatusActive, Role: RoleUser},
		Group: &Group{
			PeakRateEnabled: true,
			PeakRateWindows: []PeakRateWindow{
				{Start: "14:00", End: "18:00", Multiplier: 3.0},
				{Start: "20:00", End: "22:00", Multiplier: 2.0},
			},
		},
	}
	svc := &APIKeyService{}

	snapshot := svc.snapshotFromAPIKey(context.Background(), apiKey)
	if snapshot == nil || snapshot.Group == nil {
		t.Fatalf("snapshot or snapshot.Group must not be nil")
	}
	restored := svc.snapshotToAPIKey("k", snapshot)
	if restored.Group == nil {
		t.Fatalf("restored.Group must not be nil")
	}

	if !restored.Group.PeakRateEnabled ||
		restored.Group.PeakStart != "14:00" ||
		restored.Group.PeakEnd != "18:00" ||
		restored.Group.PeakRateMultiplier != 3.0 {
		t.Fatalf("peak fields lost in snapshot round-trip: %+v", restored.Group)
	}
	if len(restored.Group.PeakRateWindows) != 2 || restored.Group.PeakRateWindows[1].Start != "20:00" {
		t.Fatalf("peak windows lost in snapshot round-trip: %+v", restored.Group.PeakRateWindows)
	}
	if got := restored.Group.PeakMultiplierAt(at(15, 30)); got != 3.0 {
		t.Fatalf("peak hour multiplier after round-trip: got %v, want 3.0", got)
	}
	if got := restored.Group.PeakMultiplierAt(at(20, 30)); got != 2.0 {
		t.Fatalf("second peak window after round-trip: got %v, want 2.0", got)
	}
	if got := restored.Group.PeakMultiplierAt(at(22, 0)); got != 1.0 {
		t.Fatalf("off-peak multiplier after round-trip: got %v, want 1.0", got)
	}
}
