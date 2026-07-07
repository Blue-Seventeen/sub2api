import { describe, expect, it } from 'vitest'

import {
  formatPeakRateWindow,
  hasPeakRate,
  peakRateWindowsForDisplay,
  serverTimezoneLabel,
} from '../peak-rate'

describe('peak-rate utils', () => {
  it('formats multiple peak windows with server timezone label', () => {
    const fields = {
      peak_rate_enabled: true,
      peak_rate_windows: [
        { start: '09:00', end: '12:00', multiplier: 1.5 },
        { start: '18:00', end: '22:00', multiplier: 2 },
      ],
    }

    expect(hasPeakRate(fields)).toBe(true)
    const label = formatPeakRateWindow(fields, serverTimezoneLabel('+08:00'))
    expect(label).toContain('09:00-12:00')
    expect(label).toContain('1.5')
    expect(label).toContain('18:00-22:00')
    expect(label).toContain('2')
    expect(label).toContain('(UTC+08:00)')
  })

  it('falls back to legacy single-window fields', () => {
    const fields = {
      peak_rate_enabled: true,
      peak_start: '14:00',
      peak_end: '18:00',
      peak_rate_multiplier: 3,
    }

    expect(peakRateWindowsForDisplay(fields)).toEqual([
      { start: '14:00', end: '18:00', multiplier: 3 },
    ])
    const label = formatPeakRateWindow(fields)
    expect(label).toContain('14:00-18:00')
    expect(label).toContain('3')
  })

  it('preserves zero multipliers in peak windows', () => {
    const fields = {
      peak_rate_enabled: true,
      peak_rate_windows: [{ start: '00:00', end: '01:00', multiplier: 0 }],
    }

    expect(peakRateWindowsForDisplay(fields)).toEqual([
      { start: '00:00', end: '01:00', multiplier: 0 },
    ])
    expect(formatPeakRateWindow(fields)).toContain('0')
  })

  it('hides peak windows when disabled or incomplete', () => {
    expect(hasPeakRate({
      peak_rate_enabled: false,
      peak_rate_windows: [{ start: '09:00', end: '12:00', multiplier: 1.5 }],
    })).toBe(false)
    expect(peakRateWindowsForDisplay({
      peak_rate_enabled: true,
      peak_rate_windows: [{ start: '09:00', end: '', multiplier: 1.5 }],
    })).toEqual([])
  })
})
