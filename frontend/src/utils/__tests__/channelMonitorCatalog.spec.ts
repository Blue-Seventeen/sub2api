import { afterEach, describe, expect, it, vi } from 'vitest'
import { MONITOR_CATALOG_EVENT, MONITOR_CATALOG_STORAGE_KEY, notifyMonitorCatalogChanged, subscribeMonitorCatalogChanges } from '../channelMonitorCatalog'

afterEach(() => {
  vi.restoreAllMocks()
  vi.useRealTimers()
})

describe('same-origin monitor catalog invalidation', () => {
  it('still updates this tab when browser storage is unavailable', async () => {
    vi.useFakeTimers()
    const callback = vi.fn()
    const unsubscribe = subscribeMonitorCatalogChanges(callback)
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('storage blocked') })
    expect(() => notifyMonitorCatalogChanged()).not.toThrow()
    await vi.advanceTimersByTimeAsync(100)
    expect(callback).toHaveBeenCalledOnce()
    unsubscribe()
  })

  it('ignores unrelated/removal/same-value storage changes and cancels queued callbacks on cleanup', async () => {
    vi.useFakeTimers()
    const callback = vi.fn()
    const unsubscribe = subscribeMonitorCatalogChanges(callback)
    for (const change of [
      { key: 'unrelated', newValue: '1' },
      { key: MONITOR_CATALOG_STORAGE_KEY, newValue: null },
      { key: MONITOR_CATALOG_STORAGE_KEY, newValue: 'same', oldValue: 'same' },
    ]) window.dispatchEvent(new StorageEvent('storage', change))
    await vi.advanceTimersByTimeAsync(100)
    expect(callback).not.toHaveBeenCalled()
    window.dispatchEvent(new Event(MONITOR_CATALOG_EVENT))
    unsubscribe()
    await vi.advanceTimersByTimeAsync(100)
    window.dispatchEvent(new Event(MONITOR_CATALOG_EVENT))
    await vi.advanceTimersByTimeAsync(100)
    expect(callback).not.toHaveBeenCalled()
  })
})
