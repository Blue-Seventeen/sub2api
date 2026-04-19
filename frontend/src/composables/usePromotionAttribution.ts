import { promotionAPI } from '@/api/promotion'

const PROMOTION_REF_STORAGE_KEY = 'promotion_ref_code'

function safeSessionStorage(): Storage | null {
  try {
    return window.sessionStorage
  } catch {
    return null
  }
}

export function storePromotionRef(code: string): void {
  const storage = safeSessionStorage()
  const normalized = String(code || '').trim().toUpperCase()
  if (!storage || !normalized) return
  storage.setItem(PROMOTION_REF_STORAGE_KEY, normalized)
}

export function getStoredPromotionRef(): string {
  const storage = safeSessionStorage()
  return storage?.getItem(PROMOTION_REF_STORAGE_KEY)?.trim().toUpperCase() || ''
}

export function clearStoredPromotionRef(): void {
  const storage = safeSessionStorage()
  storage?.removeItem(PROMOTION_REF_STORAGE_KEY)
}

export async function tryBindStoredPromotionRef(): Promise<boolean> {
  const code = getStoredPromotionRef()
  if (!code) return false
  try {
    await promotionAPI.bindReferrer(code)
    return true
  } catch {
    return false
  } finally {
    clearStoredPromotionRef()
  }
}
