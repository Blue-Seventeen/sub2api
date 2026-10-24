import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get } = vi.hoisted(() => ({ get: vi.fn() }))
vi.mock('@/api/client', () => ({ apiClient: { get } }))

import { getActiveUsage, getActiveUsageDetails, getAll, getAllWithCount, list } from '@/api/admin/proxies'

describe('proxy live-usage API compatibility', () => {
  beforeEach(() => get.mockReset())

  it('keeps legacy counts on the legacy route and requests nullable details separately', async () => {
    get.mockResolvedValueOnce({ data: { '9': 1 } })
      .mockResolvedValueOnce({ data: { '9': { account_count: null, request_count: null, status: 'unavailable' } } })
    await expect(getActiveUsage([9])).resolves.toEqual({ '9': 1 })
    await expect(getActiveUsageDetails([9, 10])).resolves.toEqual({
      '9': { account_count: null, request_count: null, status: 'unavailable' },
    })
    expect(get.mock.calls).toEqual([
      ['/admin/proxies/active-usage', { params: { ids: '9' } }],
      ['/admin/proxies/active-usage/details', { params: { ids: '9,10' } }],
    ])
  })

  it('skips the details request when no proxy rows are visible', async () => {
    await expect(getActiveUsageDetails([])).resolves.toEqual({})
    expect(get).not.toHaveBeenCalled()
  })
})

describe.each([
  { name: 'paginated list', load: () => list(), wrap: (items: unknown[]) => ({ items, total: items.length, pages: 1 }) },
  { name: 'account selector', load: getAll, wrap: (items: unknown[]) => items },
  { name: 'account selector with counts', load: getAllWithCount, wrap: (items: unknown[]) => items },
])('$name', ({ load, wrap }) => {
  beforeEach(() => { get.mockReset() })

  it.each(['', undefined, null, {}, { items: null }, { items: {} }])(
    'rejects an empty or malformed successful response: %j',
    async (data) => {
      get.mockResolvedValue({ data })
      await expect(load()).rejects.toThrow('Invalid proxy list response')
    },
  )

  it.each([{ items: [] }, { items: [{ id: 1, name: 'valid proxy' }] }])('preserves valid rows: %j', async ({ items }) => {
    const data = wrap(items)
    get.mockResolvedValue({ data })
    await expect(load()).resolves.toBe(data)
  })
})
