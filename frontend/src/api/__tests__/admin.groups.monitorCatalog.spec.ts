import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const client = vi.hoisted(() => ({ post: vi.fn(), put: vi.fn(), delete: vi.fn() }))
vi.mock('@/api/client', () => ({ apiClient: client }))
import * as groups from '../admin/groups'
import { updateConfig, type MonitorConfig } from '../channelMonitorV2'

describe('monitor catalog invalidation after successful mutations', () => {
  const changed = vi.fn()
  beforeEach(() => {
    vi.clearAllMocks()
    Object.values(client).forEach((fn) => fn.mockResolvedValue({ data: { id: 42 } }))
    window.addEventListener('sub2api:monitor-v2-catalog-changed', changed)
  })
  afterEach(() => window.removeEventListener('sub2api:monitor-v2-catalog-changed', changed))

  const mutations = [
    ['create', () => groups.create({ name: 'new', platform: 'openai' } as Parameters<typeof groups.create>[0])],
    ['update', () => groups.update(42, { status: 'inactive' })],
    ['delete', () => groups.deleteGroup(42)],
    ['duplicate', () => groups.duplicate(42)],
    ['create route', () => groups.createCompositeRoute(42, {} as Parameters<typeof groups.createCompositeRoute>[1])],
    ['update route', () => groups.updateCompositeRoute(42, 7, {} as Parameters<typeof groups.updateCompositeRoute>[2])],
    ['delete route', () => groups.deleteCompositeRoute(42, 7)],
    ['monitor config', () => updateConfig({ group_ids: [42] } as MonitorConfig)],
  ] as const
  it.each(mutations)('%s notifies the current tab without business data or extra requests', async (_name, mutate) => {
    await mutate()
    expect(changed).toHaveBeenCalledTimes(1)
    const event = changed.mock.calls[0][0] as Event
    expect((event as CustomEvent).detail).toBeUndefined()
    expect(Object.values(client).reduce((sum, fn) => sum + fn.mock.calls.length, 0)).toBe(1)
    const version = localStorage.getItem('sub2api:monitor-v2-catalog-version')
    expect(version).toMatch(/^\d+-[\w.-]+$/)
    expect(version).not.toContain('openai')
  })
  it.each(mutations)('%s does not notify or change the marker when the mutation fails', async (_name, mutate) => {
    const versionBefore = localStorage.getItem('sub2api:monitor-v2-catalog-version')
    Object.values(client).forEach(fn => fn.mockRejectedValueOnce(new Error('save failed')))
    await expect(mutate()).rejects.toThrow('save failed')
    expect(changed).not.toHaveBeenCalled()
    expect(localStorage.getItem('sub2api:monitor-v2-catalog-version')).toBe(versionBefore)
    expect(Object.values(client).reduce((sum, fn) => sum + fn.mock.calls.length, 0)).toBe(1)
  })
})
