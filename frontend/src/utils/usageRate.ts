import { formatMultiplier } from './formatters'

export function getAdminDisplayBaseRateMultiplier(
  rateMultiplier?: number | null,
  unifiedRateMultiplier?: number | null
): number {
  const finalRate = Number.isFinite(rateMultiplier) ? Number(rateMultiplier) : 1

  if (unifiedRateMultiplier == null || !Number.isFinite(unifiedRateMultiplier)) {
    return finalRate
  }

  if (unifiedRateMultiplier === 0) {
    return 0
  }

  if (unifiedRateMultiplier < 0) {
    return finalRate
  }

  return finalRate / unifiedRateMultiplier
}

export function formatAdminDisplayBaseRateMultiplier(
  rateMultiplier?: number | null,
  unifiedRateMultiplier?: number | null
): string {
  const baseRate = getAdminDisplayBaseRateMultiplier(rateMultiplier, unifiedRateMultiplier)
  if (baseRate === 0) {
    return '0.00'
  }
  return formatMultiplier(baseRate)
}
