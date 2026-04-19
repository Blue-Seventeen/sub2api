type AdminPromotionPageSizeKey = 'relations' | 'commissions' | 'scripts'

function safeStorage(): Storage | null {
  try {
    return window.localStorage
  } catch {
    return null
  }
}

function storageKey(key: AdminPromotionPageSizeKey) {
  return `admin_promotion_${key}_page_size`
}

export function getAdminPromotionPageSize(key: AdminPromotionPageSizeKey, defaultValue: number): number {
  const raw = safeStorage()?.getItem(storageKey(key))
  const size = Number(raw)
  if (!Number.isFinite(size) || size <= 0) return defaultValue
  return Math.max(1, Math.floor(size))
}

export function setAdminPromotionPageSize(key: AdminPromotionPageSizeKey, value: number): void {
  safeStorage()?.setItem(storageKey(key), String(Math.max(1, Math.floor(value))))
}
