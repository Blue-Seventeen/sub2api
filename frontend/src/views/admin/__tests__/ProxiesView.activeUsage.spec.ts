import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, shallowMount } from '@vue/test-utils'
import ProxiesView from '../ProxiesView.vue'

const api = vi.hoisted(() => ({
  list: vi.fn(), getAllWithCount: vi.fn(), getAutoProbeConfig: vi.fn(),
  getActiveUsage: vi.fn(), getActiveUsageDetails: vi.fn(),
}))
vi.mock('@/api/admin', () => ({ adminAPI: { proxies: api } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError: vi.fn() }) }))
vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string, params?: { accounts: number; requests: number }) => {
    if (key === 'admin.proxies.activeUsageCounts') return `${params?.accounts} 账号 / ${params?.requests} 请求`
    if (key === 'admin.proxies.activeUsageUnknown') return '未知'
    return key
  } }),
}))
const mountView = () => shallowMount(ProxiesView, {
  global: { stubs: {
    AppLayout: { template: '<div><slot /></div>' },
    TablePageLayout: { template: '<div><slot name="table" /></div>' },
    DataTable: { props: ['data'], template: '<div><div v-for="row in data" :key="row.id" :data-row="row.id"><slot name="cell-active_egress_account_count" :row="row" :value="row.active_egress_account_count" /></div></div>' },
  } },
})
let wrapper: ReturnType<typeof mountView>
const visible = (value: boolean) => {
  Object.defineProperty(document, 'visibilityState', { configurable: true, value: value ? 'visible' : 'hidden' })
  document.dispatchEvent(new Event('visibilitychange'))
}
beforeEach(() => {
  vi.resetAllMocks()
  vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] })
  visible(true)
  api.list.mockResolvedValue({ items: [{ id: 9, name: 'Tokyo node', active_egress_account_count: 0 }], total: 1, pages: 1 })
  api.getAllWithCount.mockResolvedValue([])
  api.getAutoProbeConfig.mockResolvedValue({ enabled: false })
  api.getActiveUsage.mockResolvedValue({ '9': 0 })
  api.getActiveUsageDetails.mockResolvedValue({ '9': { account_count: 1, request_count: 3, status: 'ok' } })
})
afterEach(() => { wrapper?.unmount(); vi.useRealTimers(); visible(true) })

describe('proxy active usage details', () => {
  it('renders the distinct account and request counts from the details endpoint', async () => {
    wrapper = mountView()
    await flushPromises()
    expect(wrapper.get('[data-row="9"]').text()).toBe('1 账号 / 3 请求')
    expect(api.getActiveUsageDetails).toHaveBeenCalledWith([9])
    expect(api.getActiveUsage).not.toHaveBeenCalled()
  })

  it.each([
    { '9': { account_count: null, request_count: null, status: 'unavailable' } },
    {},
  ])('shows unknown instead of a stale list count when details are unavailable', async details => {
    api.getActiveUsageDetails.mockResolvedValue(details)
    wrapper = mountView()
    await flushPromises()
    expect(wrapper.get('[data-row="9"]').text()).toBe('未知')
  })

  it('replaces previous counts with unknown after a refresh failure', async () => {
    const debug = vi.spyOn(console, 'debug').mockImplementation(() => {})
    wrapper = mountView()
    await flushPromises()
    api.getActiveUsageDetails.mockRejectedValueOnce(new Error('offline'))
    await vi.advanceTimersByTimeAsync(2000)
    await flushPromises()
    expect(wrapper.get('[data-row="9"]').text()).toBe('未知')
    debug.mockRestore()
  })

  it('polls every two seconds only while visible and refreshes immediately when shown', async () => {
    wrapper = mountView()
    await flushPromises()
    expect(api.getActiveUsageDetails).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(1999)
    expect(api.getActiveUsageDetails).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(1)
    expect(api.getActiveUsageDetails).toHaveBeenCalledTimes(2)
    visible(false)
    await vi.advanceTimersByTimeAsync(6000)
    expect(api.getActiveUsageDetails).toHaveBeenCalledTimes(2)
    visible(true)
    await flushPromises()
    expect(api.getActiveUsageDetails).toHaveBeenCalledTimes(3)
    wrapper.unmount()
    await vi.advanceTimersByTimeAsync(4000)
    expect(api.getActiveUsageDetails).toHaveBeenCalledTimes(3)
  })

  it('does not issue details requests while mounted in a hidden tab', async () => {
    visible(false)
    wrapper = mountView()
    await flushPromises()
    await vi.advanceTimersByTimeAsync(4000)
    expect(api.getActiveUsageDetails).not.toHaveBeenCalled()
    expect(wrapper.get('[data-row="9"]').text()).toBe('未知')
    visible(true)
    await flushPromises()
    expect(wrapper.get('[data-row="9"]').text()).toBe('1 账号 / 3 请求')
  })

  it('keeps a single request in flight when polling takes longer than the interval', async () => {
    let finish!: (details: object) => void
    api.getActiveUsageDetails.mockReturnValueOnce(new Promise(resolve => { finish = resolve }))
    wrapper = mountView()
    await flushPromises()
    await vi.advanceTimersByTimeAsync(6000)
    expect(api.getActiveUsageDetails).toHaveBeenCalledTimes(1)
    expect(wrapper.get('[data-row="9"]').text()).toBe('未知')
    finish({ '9': { account_count: 2, request_count: 3, status: 'ok' } })
    await flushPromises()
    expect(wrapper.get('[data-row="9"]').text()).toBe('2 账号 / 3 请求')
    await vi.advanceTimersByTimeAsync(2000)
    expect(api.getActiveUsageDetails).toHaveBeenCalledTimes(2)
  })

  it('discards old-page details and loads details for newly displayed rows', async () => {
    let finishOldPage!: (details: object) => void
    api.getActiveUsageDetails.mockReturnValueOnce(new Promise(resolve => { finishOldPage = resolve }))
    wrapper = mountView()
    await flushPromises()
    api.list.mockResolvedValueOnce({ items: [{ id: 10, name: 'Next page' }], total: 2, pages: 2 })
    await (wrapper.vm as any).loadProxies()
    expect(wrapper.get('[data-row="10"]').text()).toBe('未知')
    api.getActiveUsageDetails.mockResolvedValueOnce({ '10': { account_count: 0, request_count: 0, status: 'ok' } })
    finishOldPage({ '9': { account_count: 1, request_count: 3, status: 'ok' } })
    await flushPromises()
    expect(api.getActiveUsageDetails).toHaveBeenLastCalledWith([10])
    expect(wrapper.get('[data-row="10"]').text()).toBe('0 账号 / 0 请求')
  })
})
