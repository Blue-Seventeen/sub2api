import type { BillingMode, ChannelTimePricing, PricingInterval } from '@/api/admin/channels'
import { REASONING_EFFORT_LEVELS, type ReasoningEffortLevel } from '@/constants/channel'

type TranslateFn = (key: string, params?: Record<string, unknown>) => string

export interface IntervalFormEntry {
  min_tokens: number
  max_tokens: number | null
  tier_label: string
  input_price: number | string | null
  output_price: number | string | null
  cache_write_price: number | string | null
  cache_write_1h_price?: number | string | null
  cache_read_price: number | string | null
  input_multiplier?: number | string | null
  output_multiplier?: number | string | null
  cache_write_multiplier?: number | string | null
  cache_read_multiplier?: number | string | null
  per_request_price: number | string | null
  sort_order: number
}

export interface PricingFormEntry {
  models: string[]
  billing_mode: BillingMode
  input_price: number | string | null
  output_price: number | string | null
  cache_write_price: number | string | null
  cache_write_1h_price?: number | string | null
  cache_read_price: number | string | null
  fast_multiplier?: number | string | null
  flex_multiplier?: number | string | null
  reasoning_effort_multipliers?: Record<string, number | string> | null
  image_input_price: number | string | null
  image_output_price: number | string | null
  per_request_price: number | string | null
  intervals: IntervalFormEntry[]
  time_pricing: TimePricingFormEntry
}

export function formReasoningEffortMultipliersToAPI(
  value: PricingFormEntry['reasoning_effort_multipliers'],
): Record<string, number> | null {
  if (!value) return null
  const result: Record<string, number> = {}
  for (const [effort, raw] of Object.entries(value)) {
    if (raw === '') continue
    const multiplier = Number(raw)
    if (Number.isFinite(multiplier) && multiplier > 0) result[effort] = multiplier
  }
  return Object.keys(result).length ? result : null
}

export function validateReasoningEffortMultipliers(
  value: PricingFormEntry['reasoning_effort_multipliers'],
  t: TranslateFn,
): string | null {
  if (!value) return null
  for (const [effort, raw] of Object.entries(value)) {
    if (!REASONING_EFFORT_LEVELS.includes(effort as ReasoningEffortLevel)) {
      return t('admin.channels.form.reasoningEffortLevelInvalid', { effort })
    }
    if (raw === '') continue
    const multiplier = Number(raw)
    if (!Number.isFinite(multiplier) || multiplier <= 0) {
      return t('admin.channels.form.reasoningEffortMultiplierPositive', { effort })
    }
  }
  return null
}

export interface TimePricingPeriodFormEntry {
  start_time: string
  end_time: string
  multiplier: number | string
}

export interface TimePricingFormEntry {
  timezone: string
  weekdays_only?: boolean
  periods: TimePricingPeriodFormEntry[]
}

export const COMMON_TIMEZONES = [
  'UTC', 'Asia/Shanghai', 'Asia/Tokyo', 'Asia/Seoul', 'Asia/Singapore', 'Asia/Kolkata',
  'Australia/Sydney', 'Europe/London', 'Europe/Paris', 'Europe/Berlin',
  'America/New_York', 'America/Chicago', 'America/Denver', 'America/Los_Angeles',
  'America/Toronto', 'America/Sao_Paulo', 'Pacific/Auckland', 'Pacific/Honolulu',
]

export function createDefaultTimePricingForm(): TimePricingFormEntry {
  return { timezone: 'Asia/Shanghai', weekdays_only: false, periods: [] }
}

export function apiTimePricingToForm(value: ChannelTimePricing | null | undefined): TimePricingFormEntry {
  return value ? {
    timezone: value.timezone || 'Asia/Shanghai',
    weekdays_only: value.weekdays_only === true,
    periods: (value.periods || []).map(period => ({
      ...period,
      start_time: /^\d{2}:\d{2}$/.test(period.start_time) ? `${period.start_time}:00` : period.start_time,
      end_time: /^\d{2}:\d{2}$/.test(period.end_time) ? `${period.end_time}:00` : period.end_time,
      multiplier: Number(period.multiplier).toFixed(2),
    })),
  } : createDefaultTimePricingForm()
}

export function formTimePricingToAPI(value: TimePricingFormEntry | null | undefined): ChannelTimePricing | null {
  if (!value?.periods?.length) return null
  return {
    timezone: typeof value.timezone === 'string' ? value.timezone.trim() : '',
    weekdays_only: value.weekdays_only,
    periods: value.periods.map(period => ({
      start_time: period.start_time,
      end_time: period.end_time,
      multiplier: Number(period.multiplier),
    })),
  }
}

export function isValidTimePricingMultiplier(value: number | string): boolean {
  const numericValue = Number(value)
  return /^\d+(?:\.\d{1,2})?$/.test(String(value)) && Number.isFinite(numericValue) && numericValue > 0
}

export function isValidPositiveMultiplier(value: number | string | null | undefined): boolean {
  if (value === null || value === undefined || value === '') return true
  const numericValue = Number(value)
  return Number.isFinite(numericValue) && numericValue > 0
}

function timeSeconds(value: string, isEnd = false): number {
  if (isEnd && value === '00:00:00') return 24 * 60 * 60
  const [hours, minutes, seconds = 0] = value.split(':').map(Number)
  return hours * 3600 + minutes * 60 + seconds
}

export function validateTimePricing(value: TimePricingFormEntry, t: TranslateFn): string | null {
  if (!value?.periods?.length) return null
  if (typeof value.timezone !== 'string' || value.timezone.trim() === '') return t('admin.channels.timePricingValidation.timezone')
  try {
    new Intl.DateTimeFormat('en-US', { timeZone: value.timezone })
  } catch {
    return t('admin.channels.timePricingValidation.timezone')
  }
  const ranges: Array<{ start: number; end: number }> = []
  for (const period of value.periods) {
    if (!/^\d{2}:\d{2}:\d{2}$/.test(period.start_time) || !/^\d{2}:\d{2}:\d{2}$/.test(period.end_time)) {
      return t('admin.channels.timePricingValidation.format')
    }
    const start = timeSeconds(period.start_time)
    const end = timeSeconds(period.end_time, true)
    if (period.start_time === period.end_time) return t('admin.channels.timePricingValidation.range')
    if (start >= end) return t('admin.channels.timePricingValidation.range')
    if (!isValidTimePricingMultiplier(period.multiplier)) return t('admin.channels.timePricingValidation.multiplier')
    ranges.push({ start, end })
  }
  ranges.sort((left, right) => left.start - right.start)
  for (let index = 1; index < ranges.length; index += 1) {
    if (ranges[index].start < ranges[index - 1].end) return t('admin.channels.timePricingValidation.overlap')
  }
  return null
}

export function formatTimezoneOffset(timezone: string, at = new Date()): string {
  try {
    const part = new Intl.DateTimeFormat('en-US', { timeZone: timezone, timeZoneName: 'shortOffset' })
      .formatToParts(at).find(item => item.type === 'timeZoneName')?.value
    return part && part !== 'GMT' ? part.replace('GMT', 'UTC') : 'UTC+00:00'
  } catch {
    return ''
  }
}

// 价格转换：后端存 per-token，前端显示 per-MTok ($/1M tokens)
const MTOK = 1_000_000

export function toNullableNumber(val: number | string | null | undefined): number | null {
  if (val === null || val === undefined || val === '') return null
  const num = Number(val)
  return isNaN(num) ? null : num
}

/** 前端显示值($/MTok) → 后端存储值(per-token) */
export function mTokToPerToken(val: number | string | null | undefined): number | null {
  const num = toNullableNumber(val)
  return num === null ? null : parseFloat((num / MTOK).toPrecision(10))
}

/** 后端存储值(per-token) → 前端显示值($/MTok) */
export function perTokenToMTok(val: number | null | undefined): number | null {
  if (val === null || val === undefined) return null
  // toPrecision(10) 消除 IEEE 754 浮点乘法精度误差，如 5e-8 * 1e6 = 0.04999...96 → 0.05
  return parseFloat((val * MTOK).toPrecision(10))
}

export function apiIntervalsToForm(intervals: PricingInterval[]): IntervalFormEntry[] {
  return (intervals || []).map(iv => ({
    min_tokens: iv.min_tokens,
    max_tokens: iv.max_tokens,
    tier_label: iv.tier_label || '',
    input_price: perTokenToMTok(iv.input_price),
    output_price: perTokenToMTok(iv.output_price),
    cache_write_price: perTokenToMTok(iv.cache_write_price),
    cache_write_1h_price: perTokenToMTok(iv.cache_write_1h_price),
    cache_read_price: perTokenToMTok(iv.cache_read_price),
    per_request_price: iv.per_request_price,
    sort_order: iv.sort_order,
    input_multiplier: toNullableNumber(iv.input_multiplier),
    output_multiplier: toNullableNumber(iv.output_multiplier),
    cache_write_multiplier: toNullableNumber(iv.cache_write_multiplier),
    cache_read_multiplier: toNullableNumber(iv.cache_read_multiplier)
  }))
}

export function formIntervalsToAPI(intervals: IntervalFormEntry[]): PricingInterval[] {
  return (intervals || []).map(iv => ({
    min_tokens: iv.min_tokens,
    max_tokens: iv.max_tokens,
    tier_label: iv.tier_label,
    input_price: mTokToPerToken(iv.input_price),
    output_price: mTokToPerToken(iv.output_price),
    cache_write_price: mTokToPerToken(iv.cache_write_price),
    cache_write_1h_price: mTokToPerToken(iv.cache_write_1h_price),
    cache_read_price: mTokToPerToken(iv.cache_read_price),
    per_request_price: toNullableNumber(iv.per_request_price),
    sort_order: iv.sort_order,
    input_multiplier: toNullableNumber(iv.input_multiplier),
    output_multiplier: toNullableNumber(iv.output_multiplier),
    cache_write_multiplier: toNullableNumber(iv.cache_write_multiplier),
    cache_read_multiplier: toNullableNumber(iv.cache_read_multiplier)
  }))
}

// ── 模型模式冲突检测 ──────────────────────────────────────

interface ModelPattern {
  pattern: string
  prefix: string  // lowercase, 通配符去掉尾部 *
  wildcard: boolean
}

function toModelPattern(model: string): ModelPattern {
  const lower = model.toLowerCase()
  const wildcard = lower.endsWith('*')
  return {
    pattern: model,
    prefix: wildcard ? lower.slice(0, -1) : lower,
    wildcard,
  }
}

function patternsConflict(a: ModelPattern, b: ModelPattern): boolean {
  if (!a.wildcard && !b.wildcard) return a.prefix === b.prefix
  if (a.wildcard && !b.wildcard) return b.prefix.startsWith(a.prefix)
  if (!a.wildcard && b.wildcard) return a.prefix.startsWith(b.prefix)
  // 双通配符：任一前缀是另一前缀的前缀即冲突
  return a.prefix.startsWith(b.prefix) || b.prefix.startsWith(a.prefix)
}

/** 检测模型模式列表中的冲突，返回冲突的两个模式名；无冲突返回 null */
export function findModelConflict(models: string[]): [string, string] | null {
  const patterns = models.map(toModelPattern)
  for (let i = 0; i < patterns.length; i++) {
    for (let j = i + 1; j < patterns.length; j++) {
      if (patternsConflict(patterns[i], patterns[j])) {
        return [patterns[i].pattern, patterns[j].pattern]
      }
    }
  }
  return null
}

// ── 区间校验 ──────────────────────────────────────────────

/** 校验区间列表的合法性，返回错误消息；通过则返回 null
 *
 * mode 决定区间语义：
 * - token：区间是上下文 token 数分段 (min, max]，不能重叠，无上限段必须放最后
 * - per_request / image：区间是按 tier_label 分层（1K/2K/4K 等），后端按 label
 *   匹配，不依赖 min/max，因此跳过重叠 / last-unlimited 校验
 */
export function validateIntervals(
  intervals: IntervalFormEntry[],
  mode: BillingMode,
  t: TranslateFn,
): string | null {
  if (!intervals || intervals.length === 0) return null

  // 按 min_tokens 排序（不修改原数组）
  const sorted = [...intervals].sort((a, b) => a.min_tokens - b.min_tokens)

  for (let i = 0; i < sorted.length; i++) {
    const err = validateSingleInterval(sorted[i], i, t)
    if (err) return err
  }

  // per_request / image 模式按 tier_label 匹配，不做 token 区间重叠校验
  if (mode !== 'token') return null
  return checkIntervalOverlap(sorted, t)
}

function intervalValidationMessage(
  t: TranslateFn,
  key: string,
  params: Record<string, unknown>,
): string {
  return t(`admin.channels.intervalValidation.${key}`, params)
}

function intervalPriceLabel(t: TranslateFn, key: string): string {
  return t(`admin.channels.intervalValidation.price.${key}`)
}

function validateSingleInterval(iv: IntervalFormEntry, idx: number, t: TranslateFn): string | null {
  const index = idx + 1
  if (iv.min_tokens < 0) {
    return intervalValidationMessage(
      t,
      'negativeMin',
      { index, value: iv.min_tokens },
    )
  }
  if (iv.max_tokens != null) {
    if (iv.max_tokens <= 0) {
      return intervalValidationMessage(
        t,
        'maxPositive',
        { index, value: iv.max_tokens },
      )
    }
    if (iv.max_tokens <= iv.min_tokens) {
      return intervalValidationMessage(
        t,
        'maxGreaterThanMin',
        { index, max: iv.max_tokens, min: iv.min_tokens },
      )
    }
  }
  return validateIntervalPrices(iv, idx, t)
}

function validateIntervalPrices(iv: IntervalFormEntry, idx: number, t: TranslateFn): string | null {
  const index = idx + 1
  const prices: [string, number | string | null][] = [
    ['inputPrice', iv.input_price],
    ['outputPrice', iv.output_price],
    ['cacheWritePrice', iv.cache_write_price],
    ['cacheReadPrice', iv.cache_read_price],
    ['perRequestPrice', iv.per_request_price],
  ]
  for (const [key, val] of prices) {
    if (val != null && val !== '' && Number(val) < 0) {
      const field = intervalPriceLabel(t, key)
      return intervalValidationMessage(
        t,
        'negativePrice',
        { index, field },
      )
    }
  }
  const multipliers: [string, number | string | null | undefined][] = [
    ['inputMultiplier', iv.input_multiplier],
    ['outputMultiplier', iv.output_multiplier],
    ['cacheWriteMultiplier', iv.cache_write_multiplier],
    ['cacheReadMultiplier', iv.cache_read_multiplier],
  ]
  for (const [key, val] of multipliers) {
    if (!isValidPositiveMultiplier(val)) return intervalValidationMessage(t, 'multiplierPositive', { index, field: intervalPriceLabel(t, key) })
  }
  return null
}

function checkIntervalOverlap(sorted: IntervalFormEntry[], t: TranslateFn): string | null {
  for (let i = 0; i < sorted.length; i++) {
    // 无上限区间必须是最后一个
    if (sorted[i].max_tokens == null && i < sorted.length - 1) {
      return intervalValidationMessage(
        t,
        'unboundedLast',
        { index: i + 1 },
      )
    }
    if (i === 0) continue
    const prev = sorted[i - 1]
    // (min, max] 语义：前一个区间上界 > 当前区间下界则重叠
    if (prev.max_tokens == null || prev.max_tokens > sorted[i].min_tokens) {
      const prevMax = prev.max_tokens == null ? '∞' : String(prev.max_tokens)
      return intervalValidationMessage(
        t,
        'overlap',
        { previousIndex: i, currentIndex: i + 1, previousMax: prevMax, currentMin: sorted[i].min_tokens },
      )
    }
  }
  return null
}

/** 平台对应的模型 tag 样式（背景+文字） */
export function getPlatformTagClass(platform: string): string {
  switch (platform) {
    case 'anthropic': return 'bg-orange-100 text-orange-700 dark:bg-orange-900/30 dark:text-orange-400'
    case 'openai': return 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-400'
    case 'gemini': return 'bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-400'
    case 'antigravity': return 'bg-purple-100 text-purple-700 dark:bg-purple-900/30 dark:text-purple-400'
    case 'grok': return 'bg-slate-100 text-slate-700 dark:bg-slate-800 dark:text-slate-300'
    case 'zhipu': return 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-400'
    case 'deepseek': return 'bg-cyan-100 text-cyan-700 dark:bg-cyan-900/30 dark:text-cyan-400'
    case 'volcengine': return 'bg-rose-100 text-rose-700 dark:bg-rose-900/30 dark:text-rose-400'
    case 'ali': return 'bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-400'
    case 'moonshot': return 'bg-fuchsia-100 text-fuchsia-700 dark:bg-fuchsia-900/30 dark:text-fuchsia-400'
    case 'perplexity': return 'bg-sky-100 text-sky-700 dark:bg-sky-900/30 dark:text-sky-400'
    case 'mistral': return 'bg-violet-100 text-violet-700 dark:bg-violet-900/30 dark:text-violet-400'
    case 'siliconflow': return 'bg-teal-100 text-teal-700 dark:bg-teal-900/30 dark:text-teal-400'
    case 'openrouter': return 'bg-indigo-100 text-indigo-700 dark:bg-indigo-900/30 dark:text-indigo-400'
    case 'suno': return 'bg-yellow-100 text-yellow-700 dark:bg-yellow-900/30 dark:text-yellow-300'
    case 'kling': return 'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-400'
    case 'midjourney': return 'bg-slate-100 text-slate-700 dark:bg-slate-900/30 dark:text-slate-300'
    default: return 'bg-gray-100 text-gray-700 dark:bg-gray-900/30 dark:text-gray-400'
  }
}

/** 平台对应的模型文字色（仅 text-*，用于 input/text 场景）— 与 getPlatformTagClass 同色系 */
export function getPlatformTextClass(platform: string): string {
  switch (platform) {
    case 'anthropic': return 'text-orange-700 dark:text-orange-400'
    case 'openai': return 'text-emerald-700 dark:text-emerald-400'
    case 'gemini': return 'text-blue-700 dark:text-blue-400'
    case 'antigravity': return 'text-purple-700 dark:text-purple-400'
    case 'grok': return 'text-slate-700 dark:text-slate-300'
    default: return ''
  }
}
