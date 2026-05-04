/**
 * Centralized platform color definitions.
 *
 * All components that need platform-specific styling should import from here
 * instead of defining their own color mappings.
 */

export type Platform = 'anthropic' | 'openai' | 'antigravity' | 'gemini' | 'zhipu' | 'deepseek' | 'volcengine' | 'ali' | 'moonshot' | 'perplexity' | 'mistral' | 'siliconflow' | 'xai' | 'openrouter' | 'suno' | 'kling' | 'midjourney'

// ── Badge (bg + text + border, for inline badges with border) ───────
const BADGE: Record<Platform, string> = {
  anthropic: 'bg-orange-500/10 text-orange-600 border-orange-500/30 dark:text-orange-400',
  openai: 'bg-green-500/10 text-green-600 border-green-500/30 dark:text-green-400',
  antigravity: 'bg-purple-500/10 text-purple-600 border-purple-500/30 dark:text-purple-400',
  gemini: 'bg-blue-500/10 text-blue-600 border-blue-500/30 dark:text-blue-400',
  zhipu: 'bg-emerald-500/10 text-emerald-600 border-emerald-500/30 dark:text-emerald-400',
  deepseek: 'bg-cyan-500/10 text-cyan-600 border-cyan-500/30 dark:text-cyan-400',
  volcengine: 'bg-rose-500/10 text-rose-600 border-rose-500/30 dark:text-rose-400',
  ali: 'bg-amber-500/10 text-amber-600 border-amber-500/30 dark:text-amber-400',
  moonshot: 'bg-fuchsia-500/10 text-fuchsia-600 border-fuchsia-500/30 dark:text-fuchsia-400',
  perplexity: 'bg-sky-500/10 text-sky-600 border-sky-500/30 dark:text-sky-400',
  mistral: 'bg-violet-500/10 text-violet-600 border-violet-500/30 dark:text-violet-400',
  siliconflow: 'bg-teal-500/10 text-teal-600 border-teal-500/30 dark:text-teal-400',
  xai: 'bg-neutral-500/10 text-neutral-700 border-neutral-500/30 dark:text-neutral-300',
  openrouter: 'bg-indigo-500/10 text-indigo-600 border-indigo-500/30 dark:text-indigo-400',
  suno: 'bg-yellow-500/10 text-yellow-700 border-yellow-500/30 dark:text-yellow-300',
  kling: 'bg-red-500/10 text-red-600 border-red-500/30 dark:text-red-400',
  midjourney: 'bg-slate-500/10 text-slate-600 border-slate-500/30 dark:text-slate-300',
}
const BADGE_DEFAULT = 'bg-slate-500/10 text-slate-600 border-slate-500/30 dark:text-slate-400'

// ── Light badge (softer bg, no border) ──────────────────────────────
const BADGE_LIGHT: Record<Platform, string> = {
  anthropic: 'bg-orange-500/10 text-orange-600 dark:bg-orange-500/10 dark:text-orange-300',
  openai: 'bg-green-500/10 text-green-600 dark:bg-green-500/10 dark:text-green-300',
  antigravity: 'bg-purple-500/10 text-purple-600 dark:bg-purple-500/10 dark:text-purple-300',
  gemini: 'bg-blue-500/10 text-blue-600 dark:bg-blue-500/10 dark:text-blue-300',
  zhipu: 'bg-emerald-500/10 text-emerald-600 dark:bg-emerald-500/10 dark:text-emerald-300',
  deepseek: 'bg-cyan-500/10 text-cyan-600 dark:bg-cyan-500/10 dark:text-cyan-300',
  volcengine: 'bg-rose-500/10 text-rose-600 dark:bg-rose-500/10 dark:text-rose-300',
  ali: 'bg-amber-500/10 text-amber-600 dark:bg-amber-500/10 dark:text-amber-300',
  moonshot: 'bg-fuchsia-500/10 text-fuchsia-600 dark:bg-fuchsia-500/10 dark:text-fuchsia-300',
  perplexity: 'bg-sky-500/10 text-sky-600 dark:bg-sky-500/10 dark:text-sky-300',
  mistral: 'bg-violet-500/10 text-violet-600 dark:bg-violet-500/10 dark:text-violet-300',
  siliconflow: 'bg-teal-500/10 text-teal-600 dark:bg-teal-500/10 dark:text-teal-300',
  xai: 'bg-neutral-500/10 text-neutral-700 dark:bg-neutral-500/10 dark:text-neutral-300',
  openrouter: 'bg-indigo-500/10 text-indigo-600 dark:bg-indigo-500/10 dark:text-indigo-300',
  suno: 'bg-yellow-500/10 text-yellow-700 dark:bg-yellow-500/10 dark:text-yellow-300',
  kling: 'bg-red-500/10 text-red-600 dark:bg-red-500/10 dark:text-red-300',
  midjourney: 'bg-slate-500/10 text-slate-600 dark:bg-slate-500/10 dark:text-slate-300',
}

// ── Border ──────────────────────────────────────────────────────────
const BORDER: Record<Platform, string> = {
  anthropic: 'border-orange-500/20 dark:border-orange-500/20',
  openai: 'border-green-500/20 dark:border-green-500/20',
  antigravity: 'border-purple-500/20 dark:border-purple-500/20',
  gemini: 'border-blue-500/20 dark:border-blue-500/20',
  zhipu: 'border-emerald-500/20 dark:border-emerald-500/20',
  deepseek: 'border-cyan-500/20 dark:border-cyan-500/20',
  volcengine: 'border-rose-500/20 dark:border-rose-500/20',
  ali: 'border-amber-500/20 dark:border-amber-500/20',
  moonshot: 'border-fuchsia-500/20 dark:border-fuchsia-500/20',
  perplexity: 'border-sky-500/20 dark:border-sky-500/20',
  mistral: 'border-violet-500/20 dark:border-violet-500/20',
  siliconflow: 'border-teal-500/20 dark:border-teal-500/20',
  xai: 'border-neutral-500/20 dark:border-neutral-500/20',
  openrouter: 'border-indigo-500/20 dark:border-indigo-500/20',
  suno: 'border-yellow-500/20 dark:border-yellow-500/20',
  kling: 'border-red-500/20 dark:border-red-500/20',
  midjourney: 'border-slate-500/20 dark:border-slate-500/20',
}
const BORDER_DEFAULT = 'border-gray-200 dark:border-dark-700'

// ── Accent bar (gradient) ───────────────────────────────────────────
const ACCENT_BAR: Record<Platform, string> = {
  anthropic: 'bg-gradient-to-r from-orange-400 to-orange-500',
  openai: 'bg-gradient-to-r from-emerald-400 to-emerald-500',
  antigravity: 'bg-gradient-to-r from-purple-400 to-purple-500',
  gemini: 'bg-gradient-to-r from-blue-400 to-blue-500',
  zhipu: 'bg-gradient-to-r from-emerald-400 to-emerald-500',
  deepseek: 'bg-gradient-to-r from-cyan-400 to-cyan-500',
  volcengine: 'bg-gradient-to-r from-rose-400 to-rose-500',
  ali: 'bg-gradient-to-r from-amber-400 to-amber-500',
  moonshot: 'bg-gradient-to-r from-fuchsia-400 to-fuchsia-500',
  perplexity: 'bg-gradient-to-r from-sky-400 to-sky-500',
  mistral: 'bg-gradient-to-r from-violet-400 to-violet-500',
  siliconflow: 'bg-gradient-to-r from-teal-400 to-teal-500',
  xai: 'bg-gradient-to-r from-neutral-400 to-neutral-600',
  openrouter: 'bg-gradient-to-r from-indigo-400 to-indigo-500',
  suno: 'bg-gradient-to-r from-yellow-400 to-amber-500',
  kling: 'bg-gradient-to-r from-red-400 to-rose-500',
  midjourney: 'bg-gradient-to-r from-slate-400 to-slate-600',
}
const ACCENT_BAR_DEFAULT = 'bg-gradient-to-r from-primary-400 to-primary-500'

// ── Text (price, icon) ─────────────────────────────────────────────
const TEXT: Record<Platform, string> = {
  anthropic: 'text-orange-600 dark:text-orange-400',
  openai: 'text-emerald-600 dark:text-emerald-400',
  antigravity: 'text-purple-600 dark:text-purple-400',
  gemini: 'text-blue-600 dark:text-blue-400',
  zhipu: 'text-emerald-600 dark:text-emerald-400',
  deepseek: 'text-cyan-600 dark:text-cyan-400',
  volcengine: 'text-rose-600 dark:text-rose-400',
  ali: 'text-amber-600 dark:text-amber-400',
  moonshot: 'text-fuchsia-600 dark:text-fuchsia-400',
  perplexity: 'text-sky-600 dark:text-sky-400',
  mistral: 'text-violet-600 dark:text-violet-400',
  siliconflow: 'text-teal-600 dark:text-teal-400',
  xai: 'text-neutral-700 dark:text-neutral-300',
  openrouter: 'text-indigo-600 dark:text-indigo-400',
  suno: 'text-yellow-700 dark:text-yellow-300',
  kling: 'text-red-600 dark:text-red-400',
  midjourney: 'text-slate-600 dark:text-slate-300',
}
const TEXT_DEFAULT = 'text-primary-600 dark:text-primary-400'

// ── Icon (check mark etc.) ──────────────────────────────────────────
const ICON: Record<Platform, string> = {
  anthropic: 'text-orange-500 dark:text-orange-400',
  openai: 'text-emerald-500 dark:text-emerald-400',
  antigravity: 'text-purple-500 dark:text-purple-400',
  gemini: 'text-blue-500 dark:text-blue-400',
  zhipu: 'text-emerald-500 dark:text-emerald-400',
  deepseek: 'text-cyan-500 dark:text-cyan-400',
  volcengine: 'text-rose-500 dark:text-rose-400',
  ali: 'text-amber-500 dark:text-amber-400',
  moonshot: 'text-fuchsia-500 dark:text-fuchsia-400',
  perplexity: 'text-sky-500 dark:text-sky-400',
  mistral: 'text-violet-500 dark:text-violet-400',
  siliconflow: 'text-teal-500 dark:text-teal-400',
  xai: 'text-neutral-600 dark:text-neutral-300',
  openrouter: 'text-indigo-500 dark:text-indigo-400',
  suno: 'text-yellow-500 dark:text-yellow-300',
  kling: 'text-red-500 dark:text-red-400',
  midjourney: 'text-slate-500 dark:text-slate-300',
}
const ICON_DEFAULT = 'text-primary-500 dark:text-primary-400'

const DOT: Record<Platform, string> = {
  anthropic: 'bg-orange-500',
  openai: 'bg-emerald-500',
  antigravity: 'bg-purple-500',
  gemini: 'bg-blue-500',
  zhipu: 'bg-emerald-500',
  deepseek: 'bg-cyan-500',
  volcengine: 'bg-rose-500',
  ali: 'bg-amber-500',
  moonshot: 'bg-fuchsia-500',
  perplexity: 'bg-sky-500',
  mistral: 'bg-violet-500',
  siliconflow: 'bg-teal-500',
  xai: 'bg-neutral-500',
  openrouter: 'bg-indigo-500',
  suno: 'bg-yellow-500',
  kling: 'bg-red-500',
  midjourney: 'bg-slate-500',
}
const DOT_DEFAULT = 'bg-gray-400'

// ── Button (solid bg) ───────────────────────────────────────────────
const BUTTON: Record<Platform, string> = {
  anthropic: 'bg-orange-500 text-white hover:bg-orange-600 active:bg-orange-700 dark:bg-orange-500/80 dark:hover:bg-orange-500',
  openai: 'bg-green-600 text-white hover:bg-green-700 active:bg-green-800 dark:bg-green-600/80 dark:hover:bg-green-600',
  antigravity: 'bg-purple-500 text-white hover:bg-purple-600 active:bg-purple-700 dark:bg-purple-500/80 dark:hover:bg-purple-500',
  gemini: 'bg-blue-500 text-white hover:bg-blue-600 active:bg-blue-700 dark:bg-blue-500/80 dark:hover:bg-blue-500',
  zhipu: 'bg-emerald-500 text-white hover:bg-emerald-600 active:bg-emerald-700 dark:bg-emerald-500/80 dark:hover:bg-emerald-500',
  deepseek: 'bg-cyan-500 text-white hover:bg-cyan-600 active:bg-cyan-700 dark:bg-cyan-500/80 dark:hover:bg-cyan-500',
  volcengine: 'bg-rose-500 text-white hover:bg-rose-600 active:bg-rose-700 dark:bg-rose-500/80 dark:hover:bg-rose-500',
  ali: 'bg-amber-500 text-white hover:bg-amber-600 active:bg-amber-700 dark:bg-amber-500/80 dark:hover:bg-amber-500',
  moonshot: 'bg-fuchsia-500 text-white hover:bg-fuchsia-600 active:bg-fuchsia-700 dark:bg-fuchsia-500/80 dark:hover:bg-fuchsia-500',
  perplexity: 'bg-sky-500 text-white hover:bg-sky-600 active:bg-sky-700 dark:bg-sky-500/80 dark:hover:bg-sky-500',
  mistral: 'bg-violet-500 text-white hover:bg-violet-600 active:bg-violet-700 dark:bg-violet-500/80 dark:hover:bg-violet-500',
  siliconflow: 'bg-teal-500 text-white hover:bg-teal-600 active:bg-teal-700 dark:bg-teal-500/80 dark:hover:bg-teal-500',
  xai: 'bg-neutral-700 text-white hover:bg-neutral-800 active:bg-neutral-900 dark:bg-neutral-600/80 dark:hover:bg-neutral-600',
  openrouter: 'bg-indigo-500 text-white hover:bg-indigo-600 active:bg-indigo-700 dark:bg-indigo-500/80 dark:hover:bg-indigo-500',
  suno: 'bg-yellow-500 text-white hover:bg-yellow-600 active:bg-yellow-700 dark:bg-yellow-500/80 dark:hover:bg-yellow-500',
  kling: 'bg-red-500 text-white hover:bg-red-600 active:bg-red-700 dark:bg-red-500/80 dark:hover:bg-red-500',
  midjourney: 'bg-slate-600 text-white hover:bg-slate-700 active:bg-slate-800 dark:bg-slate-600/80 dark:hover:bg-slate-600',
}
const BUTTON_DEFAULT = 'bg-primary-500 text-white hover:bg-primary-600 dark:bg-primary-600 dark:hover:bg-primary-500'

// ── Discount badge ──────────────────────────────────────────────────
const DISCOUNT: Record<Platform, string> = {
  anthropic: 'bg-orange-100 text-orange-700 dark:bg-orange-900/40 dark:text-orange-300',
  openai: 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300',
  antigravity: 'bg-purple-100 text-purple-700 dark:bg-purple-900/40 dark:text-purple-300',
  gemini: 'bg-blue-100 text-blue-700 dark:bg-blue-900/40 dark:text-blue-300',
  zhipu: 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300',
  deepseek: 'bg-cyan-100 text-cyan-700 dark:bg-cyan-900/40 dark:text-cyan-300',
  volcengine: 'bg-rose-100 text-rose-700 dark:bg-rose-900/40 dark:text-rose-300',
  ali: 'bg-amber-100 text-amber-700 dark:bg-amber-900/40 dark:text-amber-300',
  moonshot: 'bg-fuchsia-100 text-fuchsia-700 dark:bg-fuchsia-900/40 dark:text-fuchsia-300',
  perplexity: 'bg-sky-100 text-sky-700 dark:bg-sky-900/40 dark:text-sky-300',
  mistral: 'bg-violet-100 text-violet-700 dark:bg-violet-900/40 dark:text-violet-300',
  siliconflow: 'bg-teal-100 text-teal-700 dark:bg-teal-900/40 dark:text-teal-300',
  xai: 'bg-neutral-100 text-neutral-700 dark:bg-neutral-900/40 dark:text-neutral-300',
  openrouter: 'bg-indigo-100 text-indigo-700 dark:bg-indigo-900/40 dark:text-indigo-300',
  suno: 'bg-yellow-100 text-yellow-700 dark:bg-yellow-900/40 dark:text-yellow-300',
  kling: 'bg-red-100 text-red-700 dark:bg-red-900/40 dark:text-red-300',
  midjourney: 'bg-slate-100 text-slate-700 dark:bg-slate-900/40 dark:text-slate-300',
}
const DISCOUNT_DEFAULT = 'bg-red-100 text-red-700 dark:bg-red-900/40 dark:text-red-300'

// ── Header gradient (subscription confirm) ─────────────────────────
const GRADIENT: Record<Platform, string> = {
  anthropic: 'from-orange-500 to-orange-600',
  openai: 'from-emerald-500 to-emerald-600',
  antigravity: 'from-purple-500 to-purple-600',
  gemini: 'from-blue-500 to-blue-600',
  zhipu: 'from-emerald-500 to-emerald-600',
  deepseek: 'from-cyan-500 to-cyan-600',
  volcengine: 'from-rose-500 to-rose-600',
  ali: 'from-amber-500 to-amber-600',
  moonshot: 'from-fuchsia-500 to-fuchsia-600',
  perplexity: 'from-sky-500 to-sky-600',
  mistral: 'from-violet-500 to-violet-600',
  siliconflow: 'from-teal-500 to-teal-600',
  xai: 'from-neutral-500 to-neutral-700',
  openrouter: 'from-indigo-500 to-indigo-600',
  suno: 'from-yellow-500 to-amber-600',
  kling: 'from-red-500 to-rose-600',
  midjourney: 'from-slate-500 to-slate-700',
}
const GRADIENT_DEFAULT = 'from-primary-500 to-primary-600'

// ── Header text (light text on gradient bg) ────────────────────────
const GRADIENT_TEXT: Record<Platform, string> = {
  anthropic: 'text-orange-100',
  openai: 'text-emerald-100',
  antigravity: 'text-purple-100',
  gemini: 'text-blue-100',
  zhipu: 'text-emerald-100',
  deepseek: 'text-cyan-100',
  volcengine: 'text-rose-100',
  ali: 'text-amber-100',
  moonshot: 'text-fuchsia-100',
  perplexity: 'text-sky-100',
  mistral: 'text-violet-100',
  siliconflow: 'text-teal-100',
  xai: 'text-neutral-100',
  openrouter: 'text-indigo-100',
  suno: 'text-yellow-100',
  kling: 'text-red-100',
  midjourney: 'text-slate-100',
}
const GRADIENT_TEXT_DEFAULT = 'text-primary-100'

const GRADIENT_SUBTEXT: Record<Platform, string> = {
  anthropic: 'text-orange-200',
  openai: 'text-emerald-200',
  antigravity: 'text-purple-200',
  gemini: 'text-blue-200',
  zhipu: 'text-emerald-200',
  deepseek: 'text-cyan-200',
  volcengine: 'text-rose-200',
  ali: 'text-amber-200',
  moonshot: 'text-fuchsia-200',
  perplexity: 'text-sky-200',
  mistral: 'text-violet-200',
  siliconflow: 'text-teal-200',
  xai: 'text-neutral-200',
  openrouter: 'text-indigo-200',
  suno: 'text-yellow-200',
  kling: 'text-red-200',
  midjourney: 'text-slate-200',
}
const GRADIENT_SUBTEXT_DEFAULT = 'text-primary-200'

// ── Public API ──────────────────────────────────────────────────────

function isPlatform(p: string): p is Platform {
  return ['anthropic', 'openai', 'antigravity', 'gemini', 'zhipu', 'deepseek', 'volcengine', 'ali', 'moonshot', 'perplexity', 'mistral', 'siliconflow', 'xai', 'openrouter', 'suno', 'kling', 'midjourney'].includes(p)
}

export function platformBadgeClass(p: string): string {
  return isPlatform(p) ? BADGE[p] : BADGE_DEFAULT
}

export function platformBadgeLightClass(p: string): string {
  return isPlatform(p) ? BADGE_LIGHT[p] : BADGE_DEFAULT
}

export function platformBorderClass(p: string): string {
  return isPlatform(p) ? BORDER[p] : BORDER_DEFAULT
}

export function platformAccentBarClass(p: string): string {
  return isPlatform(p) ? ACCENT_BAR[p] : ACCENT_BAR_DEFAULT
}

export function platformTextClass(p: string): string {
  return isPlatform(p) ? TEXT[p] : TEXT_DEFAULT
}

export function platformIconClass(p: string): string {
  return isPlatform(p) ? ICON[p] : ICON_DEFAULT
}

export function platformDotClass(p: string): string {
  return isPlatform(p) ? DOT[p] : DOT_DEFAULT
}

export function platformButtonClass(p: string): string {
  return isPlatform(p) ? BUTTON[p] : BUTTON_DEFAULT
}

export function platformDiscountClass(p: string): string {
  return isPlatform(p) ? DISCOUNT[p] : DISCOUNT_DEFAULT
}

export function platformGradientClass(p: string): string {
  return isPlatform(p) ? GRADIENT[p] : GRADIENT_DEFAULT
}

export function platformGradientTextClass(p: string): string {
  return isPlatform(p) ? GRADIENT_TEXT[p] : GRADIENT_TEXT_DEFAULT
}

export function platformGradientSubtextClass(p: string): string {
  return isPlatform(p) ? GRADIENT_SUBTEXT[p] : GRADIENT_SUBTEXT_DEFAULT
}

export function platformLabel(p: string): string {
  switch (p) {
    case 'anthropic': return 'Anthropic'
    case 'openai': return 'OpenAI'
    case 'antigravity': return 'Antigravity'
    case 'gemini': return 'Gemini'
    case 'zhipu': return 'GLM/智谱'
    case 'deepseek': return 'DeepSeek'
    case 'volcengine': return '火山方舟/豆包'
    case 'ali': return 'Qwen/阿里'
    case 'moonshot': return 'Kimi/月之暗面'
    case 'perplexity': return 'Perplexity'
    case 'mistral': return 'Mistral'
    case 'siliconflow': return 'SiliconFlow'
    case 'xai': return 'xAI'
    case 'openrouter': return 'OpenRouter'
    case 'suno': return 'Suno'
    case 'kling': return 'Kling'
    case 'midjourney': return 'Midjourney'
    default: return p || 'API'
  }
}
