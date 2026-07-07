/**
 * 高峰时段倍率的共享展示逻辑。
 *
 * 高峰窗口由后端按服务器全局时区判定（Group.PeakMultiplierAt），
 * 前端展示必须带上服务器时区标注（来自公共设置 server_utc_offset），
 * 避免用户按浏览器本地时间误读计费窗口。
 */

export interface PeakRateFields {
  peak_rate_enabled?: boolean
  peak_start?: string
  peak_end?: string
  peak_rate_multiplier?: number
  peak_rate_windows?: PeakRateWindow[] | null
}

export interface PeakRateWindow {
  start: string
  end: string
  multiplier: number
}

export type PeakRateDisplayMode = 'compact' | 'full'

export function hasPeakRate(fields?: PeakRateFields | null): boolean {
  if (!fields?.peak_rate_enabled) return false
  return peakRateWindowsForDisplay(fields).length > 0
}

/** "+08:00" → "UTC+08:00"；旧缓存无该字段时返回空串，调用方降级为不带时区标注 */
export function serverTimezoneLabel(utcOffset?: string | null): string {
  return utcOffset ? `UTC${utcOffset}` : ''
}

/** "14:00-18:00 ×2 (UTC+08:00)"，tzLabel 为空时省略括号部分 */
export function formatPeakRateWindow(
  fields: PeakRateFields | null | undefined,
  tzLabel?: string
): string {
  if (!hasPeakRate(fields) || !fields) return ''
  const base = peakRateWindowsForDisplay(fields)
    .map((window) => `${window.start}-${window.end} ×${window.multiplier ?? 1}`)
    .join('; ')
  return tzLabel ? `${base} (${tzLabel})` : base
}

export function peakRateWindowsForDisplay(fields: PeakRateFields | null | undefined): PeakRateWindow[] {
  if (!fields) return []
  if (Array.isArray(fields.peak_rate_windows) && fields.peak_rate_windows.length > 0) {
    return fields.peak_rate_windows.filter((window) => Boolean(window.start && window.end))
  }
  if (fields.peak_start && fields.peak_end) {
    return [{
      start: fields.peak_start,
      end: fields.peak_end,
      multiplier: fields.peak_rate_multiplier ?? 1
    }]
  }
  return []
}
