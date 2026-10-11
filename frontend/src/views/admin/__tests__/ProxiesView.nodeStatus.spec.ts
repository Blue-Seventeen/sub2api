import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, shallowMount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { baseCompile } from '@intlify/message-compiler'
import ProxiesView from '../ProxiesView.vue'
import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'
import type { Proxy, ProxyAutoProbeStatus } from '@/types'

const api = vi.hoisted(() => ({
  list: vi.fn(), getAllWithCount: vi.fn(), getActiveUsage: vi.fn(),
  getAutoProbeConfig: vi.fn(), getSnapshots: vi.fn(),
  refreshClashSubscription: vi.fn(), getClashSubscriptionStatus: vi.fn(), update: vi.fn()
}))
vi.mock('@/api/admin', () => ({ adminAPI: { proxies: api } }))
vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError: vi.fn(), showSuccess: vi.fn(), showInfo: vi.fn() })
}))

type NodeProxy = Proxy & { node_status?: 'active' | 'inactive' | 'source_missing' }
const makeProxy = (overrides: Partial<NodeProxy> = {}): NodeProxy => ({
  id: 9, name: 'HK node', protocol: 'socks5', host: '127.0.0.1', port: 1080,
  username: 'node-user', password: 'node-pass', status: 'active',
  source_type: 'mihomo_subscription', subscription_id: 3, node_status: 'active',
  runtime_status: { enabled: true, status: 'running', local_url: 'socks5://127.0.0.1:1080' },
  latency_status: 'success', latency_ms: 42,
  quality_status: 'healthy', quality_grade: 'A', quality_score: 100,
  quality_checked: 1720000000, quality_summary: 'Previous successful check',
  expires_at: null, fallback_mode: 'none', expiry_warn_days: 7,
  created_at: '2026-10-12T00:00:00Z', updated_at: '2026-10-12T00:00:00Z',
  ...overrides
})
const probeStatus = (overrides: Partial<ProxyAutoProbeStatus> = {}): ProxyAutoProbeStatus => ({
  enabled: false, default_interval_sec: 60, retry_interval_sec: 5, sticky_enabled: true,
  sticky_ttl_seconds: 604800, running: false, success_queue_count: 0,
  failed_queue_count: 0, recent_completions: [], ...overrides
})

// Vitest aliases vue-i18n to the runtime build, so supply precompiled messages.
const compileMessages = (messages: Record<string, unknown>): Record<string, unknown> => Object.fromEntries(
  Object.entries(messages).map(([key, value]) => [key, typeof value === 'string'
    ? new Function('return ' + baseCompile(value).code)()
    : compileMessages(value as Record<string, unknown>)])
)
const messages = { en: compileMessages(en), zh: compileMessages(zh) }

const mountView = (locale: 'en' | 'zh' = 'en') => shallowMount(ProxiesView, {
  global: {
    plugins: [createI18n({ legacy: false, locale, messages, warnHtmlMessage: false })],
    stubs: {
      AppLayout: { template: '<div><slot /></div>' },
      TablePageLayout: { template: '<div><slot name="filters" /><slot name="table" /></div>' },
      DataTable: {
        props: ['data'], emits: ['sort'],
        template: '<div><div v-for="row in data" :key="row.id" :data-row="row.id"><div data-cell="name"><slot name="cell-name" :row="row" :value="row.name" /></div><div data-cell="address"><slot name="cell-address" :row="row" /></div><div data-cell="latency"><slot name="cell-latency" :row="row" /></div><div data-cell="status"><slot name="cell-status" :row="row" :value="row.status" /></div><div data-cell="actions"><slot name="cell-actions" :row="row" /></div></div></div>'
      },
      BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /><slot name="footer" /></div>' },
      Select: {
        props: ['modelValue', 'options'], emits: ['update:modelValue'],
        template: `<select :value="modelValue" @change="$emit('update:modelValue', $event.target.value)"><option v-for="option in options" :key="option.value" :value="option.value">{{ option.label }}</option></select>`
      }
    }
  }
})
let wrapper: ReturnType<typeof mountView>
let proxy: NodeProxy
const clipboardWrite = vi.fn()
const row = () => wrapper.get('[data-row="9"]')
const copyButton = () => row().get<HTMLButtonElement>('button[title="Copy Proxy URL"]')
const editStatus = () => wrapper.findAll<HTMLSelectElement>('#edit-proxy-form select')
  .find(select => select.find('option[value="active"]').exists())!
const action = (label: string) => row().findAll('button').find(button => button.text() === label)!

beforeEach(() => {
  vi.resetAllMocks()
  vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] })
  Object.defineProperty(window, 'isSecureContext', { configurable: true, value: true })
  Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText: clipboardWrite } })
  clipboardWrite.mockResolvedValue(undefined)
  proxy = makeProxy()
  api.list.mockImplementation(async () => ({ items: [{ ...proxy }], total: 1, pages: 1 }))
  api.getAllWithCount.mockResolvedValue([])
  api.getActiveUsage.mockResolvedValue({})
  api.getAutoProbeConfig.mockResolvedValue(probeStatus())
  api.getSnapshots.mockResolvedValue([])
  api.refreshClashSubscription.mockResolvedValue({})
  api.getClashSubscriptionStatus.mockResolvedValue(proxy.runtime_status)
  api.update.mockResolvedValue(proxy)
})
afterEach(() => {
  wrapper?.unmount()
  vi.useRealTimers()
})

describe('managed proxy node availability', () => {
  it.each([
    ['en', 'source_missing', 'Node missing from subscription'],
    ['zh', 'source_missing', '订阅节点已缺失'],
    ['en', 'inactive', 'Node disabled']
  ] as const)('shows the %s %s node state ahead of running and cached health', async (locale, status, label) => {
    proxy.node_status = status
    wrapper = mountView(locale)
    await flushPromises()

    expect(row().get('[data-cell="name"]').text()).toContain(label)
    expect(row().get('[data-cell="status"]').text()).toBe(label)
    expect(row().get('[data-cell="status"] .badge').classes()).not.toContain('badge-success')
    expect(row().get('[data-cell="latency"]').text()).not.toContain('42ms')
    expect(row().get('[data-cell="latency"]').text()).not.toContain('A/100')
    expect(row().get('[data-cell="latency"]').text()).not.toContain(locale === 'zh' ? '优质' : 'Healthy')
    expect(row().get('[data-cell="name"]').text()).not.toContain(locale === 'zh' ? '运行中' : 'Running')
  })

  it.each(['source_missing', 'inactive'] as const)('blocks URL copying and format menus for a %s node', async status => {
    proxy.node_status = status
    wrapper = mountView()
    await flushPromises()

    expect(copyButton().element.disabled).toBe(true)
    copyButton().element.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    copyButton().element.dispatchEvent(new MouseEvent('contextmenu', { bubbles: true }))
    await flushPromises()

    expect(clipboardWrite).not.toHaveBeenCalled()
    expect(row().get('[data-cell="address"]').findAll('button')).toHaveLength(1)
    expect((action('Edit').element as HTMLButtonElement).disabled).toBe(false)
    expect((action('Test Connection').element as HTMLButtonElement).disabled).toBe(false)
    expect((action('Quality Check').element as HTMLButtonElement).disabled).toBe(false)
  })

  it.each([
    ['active managed', { node_status: 'active' }],
    ['legacy managed', { node_status: undefined }],
    ['manual', { source_type: 'manual', node_status: undefined, runtime_status: undefined }]
  ] as const)('keeps cached health and copying for an %s proxy', async (_, overrides) => {
    proxy = makeProxy(overrides)
    wrapper = mountView()
    await flushPromises()

    expect(row().get('[data-cell="latency"]').text()).toContain('42ms')
    expect(row().get('[data-cell="latency"]').text()).toContain('A/100')
    expect(copyButton().element.disabled).toBe(false)
    await copyButton().trigger('click')
    expect(clipboardWrite).toHaveBeenCalledWith('socks5://node-user:node-pass@127.0.0.1:1080')
  })

  it('blocks a URL whose managed process has stopped even when its old local URL remains', async () => {
    proxy.runtime_status = { enabled: true, status: 'stopped', local_url: 'socks5://127.0.0.1:1080' }
    wrapper = mountView()
    await flushPromises()
    expect(copyButton().element.disabled).toBe(true)
  })

  it('refreshes node availability through probe snapshots and allows a returned node again', async () => {
    api.getAutoProbeConfig.mockResolvedValue(probeStatus({ enabled: true }))
    wrapper = mountView()
    await flushPromises()
    const completion = { seq: 1, proxy_id: 9, finished_at: '2026-10-12T00:01:00Z', success: false, quality_status: 'failed' }
    api.getAutoProbeConfig.mockResolvedValue(probeStatus({ enabled: true, recent_completions: [completion] }))
    api.getSnapshots.mockResolvedValue([makeProxy({ node_status: 'source_missing' })])

    await vi.advanceTimersByTimeAsync(5000)
    await flushPromises()

    expect(row().get('[data-cell="status"]').text()).toBe('Node missing from subscription')
    expect(row().get('[data-cell="latency"]').text()).not.toContain('A/100')
    expect(copyButton().element.disabled).toBe(true)

    api.getAutoProbeConfig.mockResolvedValue(probeStatus({ enabled: true, recent_completions: [{ ...completion, seq: 2, success: true }] }))
    api.getSnapshots.mockResolvedValue([makeProxy({ node_status: 'active', latency_ms: 75 })])
    await vi.advanceTimersByTimeAsync(5000)
    await flushPromises()

    expect(row().get('[data-cell="status"]').text()).toBe('Active')
    expect(row().get('[data-cell="latency"]').text()).toContain('75ms')
    expect(copyButton().element.disabled).toBe(false)
  })

  it('revokes an open copy menu when a snapshot makes the node unavailable', async () => {
    api.getAutoProbeConfig.mockResolvedValue(probeStatus({ enabled: true }))
    wrapper = mountView()
    await flushPromises()
    await copyButton().trigger('contextmenu')
    const formatButton = row().get('[data-cell="address"]').findAll('button')[1]
    expect(formatButton.text()).toContain('node-user:node-pass')
    api.getAutoProbeConfig.mockResolvedValue(probeStatus({ enabled: true, recent_completions: [
      { seq: 1, proxy_id: 9, finished_at: '2026-10-12T00:01:00Z', success: false, quality_status: 'failed' }
    ] }))
    api.getSnapshots.mockResolvedValue([makeProxy({ node_status: 'source_missing' })])
    await vi.advanceTimersByTimeAsync(5000)
    await flushPromises()

    expect(row().get('[data-cell="address"]').findAll('button')).toHaveLength(1)
    // An already queued menu click must recheck the current row before copying.
    await formatButton.trigger('click')
    expect(clipboardWrite).not.toHaveBeenCalled()
  })

  it('retains known missing node state when an older snapshot omits it', async () => {
    proxy.node_status = 'source_missing'
    api.getAutoProbeConfig.mockResolvedValue(probeStatus({ enabled: true }))
    wrapper = mountView()
    await flushPromises()
    api.getAutoProbeConfig.mockResolvedValue(probeStatus({ enabled: true, recent_completions: [
      { seq: 1, proxy_id: 9, finished_at: '2026-10-12T00:01:00Z', success: true, quality_status: 'healthy' }
    ] }))
    const snapshot = makeProxy()
    delete snapshot.node_status
    api.getSnapshots.mockResolvedValue([snapshot])
    await vi.advanceTimersByTimeAsync(5000)
    await flushPromises()

    expect(row().get('[data-cell="status"]').text()).toBe('Node missing from subscription')
    expect(copyButton().element.disabled).toBe(true)
  })

  it('keeps missing node state after subscription refresh polling reports running', async () => {
    proxy = makeProxy({ node_status: 'source_missing', runtime_status: { enabled: true, status: 'starting' } })
    api.getClashSubscriptionStatus.mockResolvedValueOnce({ enabled: true, status: 'starting' })
      .mockResolvedValue({ enabled: true, status: 'running', local_url: 'socks5://127.0.0.1:1080' })
    wrapper = mountView()
    await flushPromises()
    await action('Refresh').trigger('click')
    await flushPromises()

    expect(row().get('[data-cell="name"]').text()).toContain('Node missing from subscription')
    expect(row().get('[data-cell="name"]').text()).not.toContain('Running')
    expect(copyButton().element.disabled).toBe(true)
  })

  it('preserves an inactive node when editing its name and permits explicit reactivation', async () => {
    proxy.node_status = 'inactive'
    wrapper = mountView()
    await flushPromises()
    await action('Edit').trigger('click')
    const status = editStatus()
    expect(status.element.value).toBe('inactive')
    await wrapper.get('#edit-proxy-form').trigger('submit')
    await flushPromises()
    expect(api.update).toHaveBeenLastCalledWith(9, { name: 'HK node', status: 'inactive' })

    await action('Edit').trigger('click')
    await editStatus().setValue('active')
    const enabled = makeProxy({ ...proxy, status: 'active', node_status: 'active' })
    api.update.mockResolvedValue(enabled)
    api.list.mockResolvedValue({ items: [enabled], total: 1, pages: 1 })
    await wrapper.get('#edit-proxy-form').trigger('submit')
    await flushPromises()

    expect(api.update).toHaveBeenLastCalledWith(9, { name: 'HK node', status: 'active' })
    expect(row().get('[data-cell="status"]').text()).toBe('Active')
    expect(copyButton().element.disabled).toBe(false)
  })
})
