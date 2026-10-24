/** Same-origin invalidation only: never include group, model, or credential data. */
export const MONITOR_CATALOG_EVENT = 'sub2api:monitor-v2-catalog-changed'
export const MONITOR_CATALOG_STORAGE_KEY = 'sub2api:monitor-v2-catalog-version'

export function notifyMonitorCatalogChanged(): void {
  if (typeof window === 'undefined') return
  window.dispatchEvent(new Event(MONITOR_CATALOG_EVENT))
  try {
    const marker = `${Date.now()}-${globalThis.crypto?.randomUUID?.() ?? Math.random().toString(36).slice(2)}`
    window.localStorage.setItem(MONITOR_CATALOG_STORAGE_KEY, marker)
  } catch {
    // Same-tab updates still work when browser storage is blocked or full.
  }
}

/** Coalesce a burst of edits across this tab and same-origin tabs. */
export function subscribeMonitorCatalogChanges(callback: () => void): () => void {
  if (typeof window === 'undefined') return () => {}
  let timer: number | undefined
  const queue = () => {
    if (timer !== undefined) window.clearTimeout(timer)
    timer = window.setTimeout(() => {
      timer = undefined
      callback()
    }, 100)
  }
  const storageChanged = (event: StorageEvent) => {
    if (event.key === MONITOR_CATALOG_STORAGE_KEY && event.newValue !== null && event.newValue !== event.oldValue) queue()
  }
  window.addEventListener(MONITOR_CATALOG_EVENT, queue)
  window.addEventListener('storage', storageChanged)
  return () => {
    window.removeEventListener(MONITOR_CATALOG_EVENT, queue)
    window.removeEventListener('storage', storageChanged)
    if (timer !== undefined) window.clearTimeout(timer)
  }
}
