import { describe, it, expect, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import OpsErrorLogTable from '../OpsErrorLogTable.vue'
import zhLocale from '@/i18n/locales/zh'
import enLocale from '@/i18n/locales/en'
import type { OpsErrorLog } from '@/api/admin/ops'
import type { UsageProxyRoute } from '@/types'

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  }
})

const TooltipStub = { template: '<div><slot /></div>' }
const PaginationStub = { template: '<div class="pagination-stub" />' }

function mountTable(row: Partial<OpsErrorLog>) {
  const base = {
    id: 1,
    created_at: '2026-06-05T23:59:50Z',
    phase: 'upstream',
    type: '',
    error_owner: 'provider',
    error_source: 'upstream_http',
    severity: 'error',
    status_code: 529,
    platform: 'anthropic',
    model: 'claude-opus-4-8',
    resolved: false,
    client_request_id: '',
    request_id: 'req-1',
    message: 'boom',
    user_email: '',
    account_name: '',
    group_name: '',
    ...row,
  } as OpsErrorLog

  return mount(OpsErrorLogTable, {
    props: { rows: [base], total: 1, loading: false, page: 1, pageSize: 20 },
    global: { stubs: { 'el-tooltip': TooltipStub, Pagination: PaginationStub } },
  })
}

describe('OpsErrorLogTable user/api-key/account columns', () => {
  // 回归:上游错误行(phase=upstream, owner=provider)以前在单一「用户」列里只显示账号、
  // 丢失用户;现在用户/API Key/账号各占独立列,三者同时可见。
  it('renders user, api key and account in separate columns for an upstream row', () => {
    const wrapper = mountTable({
      user_id: 2,
      user_email: 'alice@test.com',
      api_key_id: 5,
      api_key_name: 'my-key',
      account_id: 9,
      account_name: 'acct-A',
    })

    const text = wrapper.text()
    expect(text).toContain('alice@test.com') // 用户列(上游行也显示用户)
    expect(text).toContain('my-key') // API Key 列
    expect(text).toContain('acct-A') // 账号列
  })

  it('shows the deleted badge for a soft-deleted api key', () => {
    const wrapper = mountTable({
      api_key_id: 5,
      api_key_name: 'old-key',
      api_key_deleted: true,
    })

    expect(wrapper.text()).toContain('old-key')
    expect(wrapper.text()).toContain('admin.ops.errorLog.keyDeletedBadge')
  })
})

describe('OpsErrorLogTable column order', () => {
  it('puts time and response content first for ops without changing time sorting', async () => {
    const wrapper = mountTable({})
    await wrapper.setProps({ summaryFirst: true })

    const headers = wrapper.findAll('thead th')
    expect(headers.slice(0, 3).map((header) => header.text())).toEqual([
      'admin.ops.errorLog.time',
      'admin.ops.errorLog.message',
      'admin.ops.errorLog.user',
    ])
    expect(wrapper.findAll('tbody td')[1].text()).toBe('boom')

    await headers[0].trigger('click')
    expect(wrapper.emitted('sort')).toEqual([['created_at', 'asc']])
    wrapper.unmount()
  })

  it('preserves the usage column order and visibility by default', async () => {
    const wrapper = mountTable({})
    await wrapper.setProps({ visibleColumnKeys: ['created_at', 'user', 'message'] })

    expect(wrapper.findAll('thead th').map((header) => header.text())).toEqual([
      'admin.ops.errorLog.user',
      'admin.ops.errorLog.message',
      'admin.ops.errorLog.time',
    ])
    wrapper.unmount()
  })
})

describe('OpsErrorLogTable proxy routes', () => {
  it.each<{ name: string; routes: UsageProxyRoute[] | null | undefined; expected: string }>([
    { name: 'missing historical routes', routes: undefined, expected: 'admin.usage.proxyRouteUnknown' },
    { name: 'null historical routes', routes: null, expected: 'admin.usage.proxyRouteUnknown' },
    { name: 'empty historical routes', routes: [], expected: 'admin.usage.proxyRouteUnknown' },
    { name: 'explicit unknown routes', routes: [{ kind: 'unknown' }], expected: 'admin.usage.proxyRouteUnknown' },
    { name: 'direct and system proxy routes', routes: [{ kind: 'direct' }, { kind: 'system_proxy' }], expected: 'admin.usage.proxyRouteDirect → admin.usage.proxyRouteSystem' },
    {
      name: 'ordered proxy nodes with IPv6',
      routes: [
        { kind: 'proxy', name: 'Tokyo node', host: 'proxy.example.test', port: 1080 },
        { kind: 'proxy', name: 'IPv6 backup', host: '2001:db8::8', port: 2080 },
      ],
      expected: 'Tokyo node (proxy.example.test:1080) → IPv6 backup ([2001:db8::8]:2080)',
    },
  ])('shows $name in the default administrator table', ({ routes, expected }) => {
    const wrapper = mountTable({ proxy_routes: routes })
    const headers = wrapper.findAll('thead th').map((header) => header.text())
    const columnIndex = headers.indexOf('admin.usage.proxyNode')

    expect(columnIndex).toBeGreaterThanOrEqual(0)
    expect(wrapper.findAll('tbody td')[columnIndex].text()).toBe(expected)
    wrapper.unmount()
  })

  it('honors proxy route visibility without dropping the row', async () => {
    const wrapper = mountTable({ proxy_routes: [{ kind: 'proxy', name: 'Tokyo node', host: 'proxy.example.test', port: 1080 }] })
    await wrapper.setProps({ visibleColumnKeys: ['message', 'proxy_routes'] })
    expect(wrapper.findAll('thead th').map((header) => header.text())).toEqual(['admin.ops.errorLog.message', 'admin.usage.proxyNode'])
    expect(wrapper.text()).toContain('Tokyo node (proxy.example.test:1080)')

    await wrapper.setProps({ visibleColumnKeys: ['message'] })
    expect(wrapper.text()).not.toContain('Tokyo node')
    expect(wrapper.findAll('tbody td').map((cell) => cell.text())).toEqual(['boom'])
    wrapper.unmount()
  })
})

// 防回归:组件用 admin.ops.errorLog.* 命名空间。若 i18n 键写错命名空间(如误放到
// errorDetail),真实 vue-i18n 会回退返回 key 本身 → 界面显示原始路径字符串。
// 这里用真实 locale 校验键确实可解析(返回译文而非 key)。
// 防回归:组件用 admin.ops.errorLog.* 命名空间。若键写错命名空间(如误放到
// errorDetail),界面会显示原始路径字符串而非译文。vitest 的 vue-i18n 为 runtime-only
// (无消息编译器,t() 对任何键都回退返回 key),故直接校验 locale 对象的命名空间含这些键。
describe('OpsErrorLogTable i18n keys exist in the errorLog namespace', () => {
  const locales: Record<string, any> = { zh: zhLocale, en: enLocale }
  for (const [name, msgs] of Object.entries(locales)) {
    it(`has apiKey & keyDeletedBadge for ${name}`, () => {
      const errorLog = msgs?.admin?.ops?.errorLog
      expect(errorLog?.apiKey).toBeTruthy()
      expect(errorLog?.keyDeletedBadge).toBeTruthy()
    })
  }
})
