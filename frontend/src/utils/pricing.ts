import { getDisplayCurrencySymbol } from '@/utils/format'

/**
 * formatScaled formats a per-token or per-request display price scaled by `scale`.
 *
 * Uses toPrecision(10) then strips trailing zeros to avoid IEEE 754 display noise.
 */
export function formatScaled(value: number | null, scale: number): string {
  if (value == null) return '-'
  return `${getDisplayCurrencySymbol()}${(value * scale).toPrecision(10).replace(/\.?0+$/, '')}`
}
