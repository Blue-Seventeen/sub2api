import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, shallowMount } from '@vue/test-utils'
import type { MonitorDimensions, MonitorFilter } from '@/api/channelMonitorV2'
import FilterMultiSelect from '@/features/channel-monitor-v2/FilterMultiSelect.vue'

enableAutoUnmount(afterEach)
const navigation = vi.hoisted(() => ({ query: {} as Record<string, string> }))

const api = vi.hoisted(() => ({
  getDimensions: vi.fn(), getSnapshot: vi.fn(), getMatrix: vi.fn(),
  getModels: vi.fn(), getErrors: vi.fn(), getUsers: vi.fn(),
}))
vi.mock('@/api/channelMonitorV2', () => api)
vi.mock('vue-router', () => ({ useRoute: () => ({ query: navigation.query }), useRouter: () => ({ replace: vi.fn() }) }))
vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key, te: () => false, locale: { value: 'zh' } }),
}))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ isAdmin: true }) }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError: vi.fn() }) }))
vi.mock('@/utils/featureFlags', () => ({ isChannelMonitorThroughputHidden: () => false, isChannelMonitorUserRankingHidden: () => false }))
import ChannelStatusV2View from '../ChannelStatusV2View.vue'

describe('monitor catalog changes while page is open', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.clearAllMocks()
    navigation.query = {}
    api.getDimensions.mockResolvedValue({ platforms: [], groups: [], models: [] })
    const latency = { sample_count: 0, p50_ms: null, p90_ms: null, p95_ms: null, avg_ms: null }
    api.getSnapshot.mockResolvedValue({ config: { refresh_interval_seconds: 300 }, coverage: {}, metrics: {
      success_requests: 0, error_requests: 0, request_count: 0, token_count: 0, rpm: 0, tpm: 0,
      error_rate: 0, cache_rate: 0, ttft: latency, duration: latency,
    }, health: { overall: 'unknown' } })
    api.getMatrix.mockResolvedValue({ coverage: {}, items: [] })
    api.getModels.mockResolvedValue({ items: [] })
  })
  afterEach(() => vi.useRealTimers())

  it('coalesces same-tab and other-tab notifications into one full reload, then unsubscribes', async () => {
    const wrapper = shallowMount(ChannelStatusV2View, { global: { renderStubDefaultSlot: true } })
    await flushPromises()
    expect(api.getDimensions).toHaveBeenCalledTimes(1)
    window.dispatchEvent(new Event('sub2api:monitor-v2-catalog-changed'))
    window.dispatchEvent(new Event('sub2api:monitor-v2-catalog-changed'))
    window.dispatchEvent(new StorageEvent('storage', { key: 'sub2api:monitor-v2-catalog-version', newValue: '123-marker', storageArea: localStorage }))
    await vi.advanceTimersByTimeAsync(100)
    await flushPromises()
    expect(api.getDimensions).toHaveBeenCalledTimes(2)
    expect(api.getSnapshot).toHaveBeenCalledTimes(2)
    expect(api.getMatrix).toHaveBeenCalledTimes(2)
    expect(api.getModels).toHaveBeenCalledTimes(2)
    // Unrelated browser settings must not trigger requests.
    window.dispatchEvent(new StorageEvent('storage', { key: 'unrelated', newValue: '1' }))
    await vi.advanceTimersByTimeAsync(100)
    expect(api.getSnapshot).toHaveBeenCalledTimes(2)
    window.dispatchEvent(new Event('sub2api:monitor-v2-catalog-changed'))
    wrapper.unmount()
    await vi.advanceTimersByTimeAsync(300000)
    window.dispatchEvent(new Event('sub2api:monitor-v2-catalog-changed'))
    await vi.advanceTimersByTimeAsync(100)
    expect(api.getSnapshot).toHaveBeenCalledTimes(2)
  })

  it('labels exact models without traffic as pending instead of claiming 100% success', async () => {
    const snapshot = await api.getSnapshot()
    api.getModels.mockResolvedValue({ items: [{ platform: 'openai', model: 'no-traffic-model', metrics: { ...snapshot.metrics, has_data: false }, health: { overall: 'unknown' } }] })
    const wrapper = shallowMount(ChannelStatusV2View, { global: { renderStubDefaultSlot: true } })
    await flushPromises()
    const row = wrapper.find('tbody tr')
    expect(row.text()).toContain('no-traffic-model')
    expect(row.text()).toContain('channelMonitorV2.pendingData')
    expect(row.text()).not.toContain('100.0%')
    wrapper.unmount()
  })

  function scopedCatalog(availableGroupIds = [1, 2, 3]) {
    const allGroups = [
      { id: 1, name: 'A', platform: 'openai' },
      { id: 2, name: 'B', platform: 'openai' },
      { id: 3, name: 'C', platform: 'anthropic' },
    ]
    const allModels = [
      { value: 'model-a', label: 'model-a', platform: 'openai', groupId: 1 },
      { value: 'model-b', label: 'model-b', platform: 'openai', groupId: 2 },
      { value: 'model-c', label: 'model-c', platform: 'anthropic', groupId: 3 },
    ]
    const groups = allGroups.filter(group => availableGroupIds.includes(group.id))
    const models = allModels.filter(model => availableGroupIds.includes(model.groupId))
    api.getDimensions.mockImplementation(async (filter: MonitorFilter): Promise<MonitorDimensions> => ({
      platforms: [{ value: 'openai', label: 'OpenAI' }, { value: 'anthropic', label: 'Anthropic' }],
      groups: groups.filter(group => (!filter.groupIds.length || filter.groupIds.includes(group.id)) &&
        (!filter.platforms.length || filter.platforms.includes(group.platform))),
      models: models.filter(model => (!filter.groupIds.length || filter.groupIds.includes(model.groupId)) &&
        (!filter.platforms.length || filter.platforms.includes(model.platform)) &&
        (!filter.models.length || filter.models.includes(model.value))),
    }))
    return models
  }

  function mountView() {
    return shallowMount(ChannelStatusV2View, { global: { renderStubDefaultSlot: true } })
  }

  function picker(wrapper: ReturnType<typeof mountView>, index: number) {
    return wrapper.findAllComponents(FilterMultiSelect)[index]
  }

  function optionValues(wrapper: ReturnType<typeof mountView>, index: number) {
    return picker(wrapper, index).props('options').map((option: { value: string }) => option.value)
  }

  it('scopes same-platform disjoint models to A/B while retaining both authorized group choices', async () => {
    scopedCatalog()
    navigation.query = { platform: 'openai', group: '1' }
    const wrapper = mountView()
    await flushPromises()
    expect(optionValues(wrapper, 2)).toEqual(['model-a'])
    expect(optionValues(wrapper, 1)).toEqual(['1', '2'])
    picker(wrapper, 1).vm.$emit('update:modelValue', ['2'])
    await flushPromises()
    expect(optionValues(wrapper, 2)).toEqual(['model-b'])
    expect(optionValues(wrapper, 1)).toEqual(['1', '2'])
    picker(wrapper, 1).vm.$emit('update:modelValue', ['1'])
    await flushPromises()
    expect(optionValues(wrapper, 2)).toEqual(['model-a'])
  })

  it('refreshes the model directory on platform switches and does not narrow its own picker by selected model', async () => {
    scopedCatalog()
    navigation.query = { platform: 'openai', model: 'model-a' }
    const wrapper = mountView()
    await flushPromises()
    expect(optionValues(wrapper, 2)).toEqual(['model-a', 'model-b'])
    const directoryCalls = api.getDimensions.mock.calls.length
    picker(wrapper, 2).vm.$emit('update:modelValue', ['model-b'])
    await flushPromises()
    expect(optionValues(wrapper, 2)).toEqual(['model-a', 'model-b'])
    expect(api.getDimensions).toHaveBeenCalledTimes(directoryCalls)
    picker(wrapper, 0).vm.$emit('update:modelValue', ['anthropic'])
    await flushPromises()
    expect(optionValues(wrapper, 2)).toEqual(['model-c'])
    expect(picker(wrapper, 2).props('modelValue')).toEqual([])
    expect(api.getDimensions.mock.calls.some(([filter]) => filter.platforms.includes('anthropic') && filter.models.length === 0)).toBe(true)
  })

  it('keeps an empty selected scope empty without inheriting the all-group model directory', async () => {
    const models = scopedCatalog()
    navigation.query = { group: '2', model: 'model-b' }
    const wrapper = mountView()
    await flushPromises()
    expect(optionValues(wrapper, 2)).toEqual(['model-b'])
    models.splice(1, 1)
    window.dispatchEvent(new Event('sub2api:monitor-v2-catalog-changed'))
    await vi.advanceTimersByTimeAsync(100)
    await flushPromises()
    expect(optionValues(wrapper, 2)).toEqual([])
    expect(picker(wrapper, 1).props('modelValue')).toEqual(['2'])
    expect(picker(wrapper, 2).props('modelValue')).toEqual([])
    expect(optionValues(wrapper, 1)).toEqual(['1', '2', '3'])
  })

  it('offers all eligible models without a selection and returns to them after clearing a group', async () => {
    scopedCatalog()
    const wrapper = mountView()
    await flushPromises()
    expect(optionValues(wrapper, 2)).toEqual(['model-a', 'model-b', 'model-c'])
    picker(wrapper, 1).vm.$emit('update:modelValue', ['1'])
    await flushPromises()
    expect(optionValues(wrapper, 2)).toEqual(['model-a'])
    picker(wrapper, 1).vm.$emit('update:modelValue', [])
    await flushPromises()
    expect(optionValues(wrapper, 2)).toEqual(['model-a', 'model-b', 'model-c'])
  })

  it('keeps the sole authorized group selectable while using its model directory', async () => {
    scopedCatalog([2])
    const wrapper = mountView()
    await flushPromises()
    expect(optionValues(wrapper, 1)).toEqual(['2'])
    expect(optionValues(wrapper, 2)).toEqual(['model-b'])
    picker(wrapper, 1).vm.$emit('update:modelValue', ['2'])
    await flushPromises()
    expect(optionValues(wrapper, 1)).toEqual(['2'])
    expect(optionValues(wrapper, 2)).toEqual(['model-b'])
  })

  it('finishes a pending scoped directory when a model selection cancels the previous request', async () => {
    scopedCatalog()
    const provideDirectory = api.getDimensions.getMockImplementation()!
    const pending: Array<{ resolve: (value: MonitorDimensions) => void; filter: MonitorFilter; signal: AbortSignal }> = []
    api.getDimensions.mockImplementation((filter: MonitorFilter, _admin: boolean, signal: AbortSignal) => {
      if (filter.groupIds.includes(2)) return new Promise<MonitorDimensions>(resolve => pending.push({ resolve, filter, signal }))
      return provideDirectory(filter)
    })
    navigation.query = { platform: 'openai', group: '1' }
    const wrapper = mountView()
    await flushPromises()
    picker(wrapper, 1).vm.$emit('update:modelValue', ['2'])
    await flushPromises()
    picker(wrapper, 2).vm.$emit('update:modelValue', ['model-a'])
    await flushPromises()
    expect(pending[0].signal.aborted).toBe(true)
    for (const request of [...pending].reverse()) request.resolve(await provideDirectory(request.filter))
    await flushPromises()
    expect(optionValues(wrapper, 2)).toEqual(['model-b'])
    expect(picker(wrapper, 2).props('modelValue')).toEqual([])
    expect(optionValues(wrapper, 1)).toEqual(['1', '2'])
  })

  it('ignores a late directory response after switching back to the previous group', async () => {
    scopedCatalog()
    const provideDirectory = api.getDimensions.getMockImplementation()!
    let finishB!: (value: MonitorDimensions) => void
    const delayedB = new Promise<MonitorDimensions>(resolve => { finishB = resolve })
    api.getDimensions.mockImplementation((filter: MonitorFilter) => filter.groupIds.includes(2) ? delayedB : provideDirectory(filter))
    navigation.query = { platform: 'openai', group: '1' }
    const wrapper = mountView()
    await flushPromises()
    picker(wrapper, 1).vm.$emit('update:modelValue', ['2'])
    await flushPromises()
    picker(wrapper, 1).vm.$emit('update:modelValue', ['1'])
    await flushPromises()
    finishB(await provideDirectory({ range: '90m', platforms: ['openai'], groupIds: [2], models: [] }))
    await flushPromises()
    expect(optionValues(wrapper, 2)).toEqual(['model-a'])
  })

  it.each(['manual', 'poll', 'notification'] as const)('refreshes the selected model directory on %s refresh', async (kind) => {
    const models = scopedCatalog()
    navigation.query = { platform: 'openai', group: '2' }
    const wrapper = mountView()
    await flushPromises()
    const callsBefore = api.getDimensions.mock.calls.length
    models[1] = { ...models[1], value: 'model-b-new', label: 'model-b-new' }
    if (kind === 'manual') await wrapper.get('button[title="common.refresh"]').trigger('click')
    if (kind === 'poll') await vi.advanceTimersByTimeAsync(300000)
    if (kind === 'notification') {
      window.dispatchEvent(new Event('sub2api:monitor-v2-catalog-changed'))
      window.dispatchEvent(new Event('sub2api:monitor-v2-catalog-changed'))
      window.dispatchEvent(new StorageEvent('storage', { key: 'sub2api:monitor-v2-catalog-version', newValue: '456-marker', storageArea: localStorage }))
      await vi.advanceTimersByTimeAsync(100)
    }
    await flushPromises()
    expect(optionValues(wrapper, 2)).toEqual(['model-b-new'])
    expect(optionValues(wrapper, 1)).toEqual(['1', '2'])
    expect(api.getDimensions.mock.calls.length - callsBefore).toBe(2)
    expect(api.getDimensions.mock.calls.slice(callsBefore).some(([filter]) =>
      filter.groupIds.join(',') === '2' && filter.platforms.join(',') === 'openai' && filter.models.length === 0)).toBe(true)
    expect(api.getSnapshot).toHaveBeenCalledTimes(2)
  })
})
