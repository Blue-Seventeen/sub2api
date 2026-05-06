import { describe, expect, it } from 'vitest'
import {
  formatCompactCurrencyAmount,
  formatCostAmount,
  formatCurrencyAmount,
  getDisplayCurrencySymbol,
  normalizeDisplayCurrencySymbol,
  setDisplayCurrencySymbol,
  truncateDisplayCurrencySymbol
} from '@/utils/format'

describe('display currency formatting', () => {
  it('normalizes configured display currency symbols', () => {
    expect(normalizeDisplayCurrencySymbol(' ¥ ')).toBe('¥')
    expect(normalizeDisplayCurrencySymbol('RMB')).toBe('RMB')
    expect(normalizeDisplayCurrencySymbol('')).toBe('$')
    expect(normalizeDisplayCurrencySymbol('123456789')).toBe('$')
    expect(normalizeDisplayCurrencySymbol('bad\nsymbol')).toBe('$')
  })

  it('uses Unicode code point length for symbol limits', () => {
    const nonBmpSymbol = '\u{1F4B0}'
    expect(truncateDisplayCurrencySymbol(nonBmpSymbol.repeat(9))).toBe(nonBmpSymbol.repeat(8))
    expect(normalizeDisplayCurrencySymbol(nonBmpSymbol.repeat(8))).toBe(nonBmpSymbol.repeat(8))
    expect(normalizeDisplayCurrencySymbol(nonBmpSymbol.repeat(9))).toBe('$')
  })

  it('formats money with the active display symbol', () => {
    setDisplayCurrencySymbol('¥')
    expect(getDisplayCurrencySymbol()).toBe('¥')
    expect(formatCurrencyAmount(12.3)).toBe('¥12.30')
    expect(formatCostAmount(0.123456)).toBe('¥0.1235')

    setDisplayCurrencySymbol('RMB')
    expect(formatCurrencyAmount(5, 0)).toBe('RMB5')
    expect(formatCompactCurrencyAmount(12_300)).toBe('RMB12.3K')

    setDisplayCurrencySymbol('$')
  })
})
