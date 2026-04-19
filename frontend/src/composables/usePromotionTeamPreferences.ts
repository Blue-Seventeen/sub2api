const PAGE_SIZE_KEY = 'promotion_team_page_size'
const SORT_BY_KEY = 'promotion_team_sort_by'
const SORT_ORDER_KEY = 'promotion_team_sort_order'

export type PromotionTeamSortBy = 'today_contribution' | 'total_contribution' | 'joined_at' | 'activated_at'
export type PromotionTeamSortOrder = 'asc' | 'desc'

function safeStorage(): Storage | null {
  try {
    return window.localStorage
  } catch {
    return null
  }
}

export function getPromotionTeamPageSize(defaultValue = 10): number {
  const raw = safeStorage()?.getItem(PAGE_SIZE_KEY)
  const size = Number(raw)
  if (!Number.isFinite(size) || size <= 0) return defaultValue
  return size
}

export function setPromotionTeamPageSize(value: number): void {
  safeStorage()?.setItem(PAGE_SIZE_KEY, String(Math.max(1, Math.floor(value))))
}

export function getPromotionTeamSortBy(defaultValue: PromotionTeamSortBy = 'today_contribution'): PromotionTeamSortBy {
  const raw = safeStorage()?.getItem(SORT_BY_KEY)
  switch (raw) {
    case 'total_contribution':
    case 'joined_at':
    case 'activated_at':
    case 'today_contribution':
      return raw
    default:
      return defaultValue
  }
}

export function setPromotionTeamSortBy(value: PromotionTeamSortBy): void {
  safeStorage()?.setItem(SORT_BY_KEY, value)
}

export function getPromotionTeamSortOrder(defaultValue: PromotionTeamSortOrder = 'desc'): PromotionTeamSortOrder {
  const raw = safeStorage()?.getItem(SORT_ORDER_KEY)
  return raw === 'asc' ? 'asc' : defaultValue
}

export function setPromotionTeamSortOrder(value: PromotionTeamSortOrder): void {
  safeStorage()?.setItem(SORT_ORDER_KEY, value)
}
