package service

// effectiveUnifiedMultiplier returns the effective unified multiplier for a user.
// 当未启用时返回 1；允许 0；非法负值按 1 兜底。
func effectiveUnifiedMultiplier(user *User) float64 {
	if user == nil {
		return 1
	}
	return user.EffectiveUnifiedRateMultiplier()
}

// NormalizePersistedUnifiedRateMultiplier normalizes the stored unified multiplier.
// 为兼容历史/未填写场景：
//   - 负值按 1 兜底
//   - 未启用且值为 0 时按默认 1 处理
//   - 启用时允许 0
func NormalizePersistedUnifiedRateMultiplier(enabled bool, multiplier float64) float64 {
	if multiplier < 0 {
		return 1
	}
	if !enabled && multiplier == 0 {
		return 1
	}
	return multiplier
}

// finalRateFromBaseMultiplier converts a base group multiplier to the effective
// user-facing final multiplier by applying the user's unified multiplier.
func finalRateFromBaseMultiplier(baseMultiplier float64, user *User) float64 {
	if baseMultiplier < 0 {
		baseMultiplier = 1
	}
	return baseMultiplier * effectiveUnifiedMultiplier(user)
}

// realCostFromBase converts standard/base cost to admin real billed cost.
// 规则：
//   - 统一倍率未启用/非 0：真实费用 = 标准费用 × 基础倍率
//   - 统一倍率为 0：真实费用直接记 0，避免后续出现除 0/误解
func realCostFromBase(baseCost float64, baseMultiplier float64, user *User) float64 {
	if baseCost <= 0 {
		return 0
	}
	if baseMultiplier < 0 {
		baseMultiplier = 1
	}
	if effectiveUnifiedMultiplier(user) == 0 {
		return 0
	}
	return baseCost * baseMultiplier
}

func realCostFromActualRate(actualCost, finalRateMultiplier, realRateMultiplier float64, user *User) float64 {
	if actualCost <= 0 {
		return 0
	}
	if effectiveUnifiedMultiplier(user) == 0 {
		return 0
	}
	if finalRateMultiplier <= 0 {
		return 0
	}
	if realRateMultiplier < 0 {
		realRateMultiplier = 0
	}
	return actualCost * realRateMultiplier / finalRateMultiplier
}

func usageRateMultiplierForRealCost(useImageRate bool, textMultiplier, imageMultiplier float64) float64 {
	if useImageRate {
		return imageMultiplier
	}
	return textMultiplier
}

func applyRealActualCostFromRates(cost *CostBreakdown, useImageRate bool, finalTextMultiplier, finalImageMultiplier, realTextMultiplier, realImageMultiplier float64, user *User) {
	if cost == nil {
		return
	}
	finalRate := usageRateMultiplierForRealCost(useImageRate, finalTextMultiplier, finalImageMultiplier)
	realRate := usageRateMultiplierForRealCost(useImageRate, realTextMultiplier, realImageMultiplier)
	cost.RealActualCost = realCostFromActualRate(cost.ActualCost, finalRate, realRate, user)
}

func costBillingMode(cost *CostBreakdown) string {
	if cost == nil {
		return ""
	}
	return cost.BillingMode
}

func usageUsesImageMultiplier(imageCount int, billingMode string, requestCount, taskCount, durationSeconds, characterCount int) bool {
	if imageCount <= 0 {
		return false
	}
	switch billingMode {
	case string(BillingModeImage):
		return true
	case string(BillingModePerRequest):
		return requestCount <= 0 && taskCount <= 0 && durationSeconds <= 0 && characterCount <= 0
	default:
		return false
	}
}
