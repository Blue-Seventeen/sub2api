import { flushPromises, shallowMount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import OpsErrorDetailModal from '../OpsErrorDetailModal.vue'
import type { OpsErrorDetail } from '@/api/admin/ops'
import type { UsageProxyRoute } from '@/types'

const mocks = vi.hoisted(() => ({
  getRequestErrorDetail: vi.fn(),
  listRequestErrorUpstreamErrors: vi.fn()
}))

vi.mock('@/api/admin/ops', () => ({
  opsAPI: {
    getRequestErrorDetail: mocks.getRequestErrorDetail,
    getUpstreamErrorDetail: vi.fn(),
    listRequestErrorUpstreamErrors: mocks.listRequestErrorUpstreamErrors
  }
}))

vi.mock('@/stores', () => ({
  useAppStore: () => ({ showError: vi.fn() })
}))

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key })
  }
})

describe('OpsErrorDetailModal', () => {
  beforeEach(() => {
    mocks.getRequestErrorDetail.mockReset()
    mocks.listRequestErrorUpstreamErrors.mockReset()
    mocks.listRequestErrorUpstreamErrors.mockResolvedValue({ items: [] })
  })

  it.each<{ name: string; routes: UsageProxyRoute[] | null | undefined; expected: string }>([
    { name: 'missing historical routes', routes: undefined, expected: 'admin.usage.proxyRouteUnknown' },
    { name: 'null historical routes', routes: null, expected: 'admin.usage.proxyRouteUnknown' },
    {
      name: 'ordered proxy nodes with IPv6',
      routes: [
        { kind: 'proxy', name: 'Tokyo node', host: 'proxy.example.test', port: 1080 },
        { kind: 'proxy', name: 'IPv6 backup', host: '2001:db8::8', port: 2080 },
      ],
      expected: 'Tokyo node (proxy.example.test:1080) → IPv6 backup ([2001:db8::8]:2080)',
    },
  ])('shows $name in administrator error details', async ({ routes, expected }) => {
    const detail: OpsErrorDetail = {
      id: 1, created_at: '2026-10-24T00:00:00Z', phase: 'request', type: 'upstream_error',
      error_owner: 'provider', error_source: 'gateway', severity: 'P1', status_code: 502,
      platform: 'openai', model: 'gpt-5.6', resolved: false, client_request_id: '', request_id: 'rid-1',
      message: 'upstream failed', error_body: '', user_email: '', account_name: 'account', group_name: 'group',
      is_business_limited: false, proxy_routes: routes,
    }
    mocks.getRequestErrorDetail.mockResolvedValue(detail)
    const wrapper = shallowMount(OpsErrorDetailModal, {
      props: { show: true, errorId: 1, errorType: 'request' },
      global: { stubs: { BaseDialog: { template: '<div><slot /></div>' }, Icon: true } },
    })
    await flushPromises()

    expect(wrapper.text()).toContain('admin.usage.proxyNode')
    expect(wrapper.text()).toContain(expected)
    wrapper.unmount()
  })

  it('prioritizes upstream root cause and deduplicates diagnostic payloads', async () => {
    mocks.getRequestErrorDetail.mockResolvedValue({
      id: 1,
      created_at: '2026-08-19T00:00:00Z',
      phase: 'request',
      type: 'upstream_error',
      error_owner: 'provider',
      error_source: 'gateway',
      severity: 'P1',
      status_code: 502,
      upstream_status_code: 429,
      platform: 'openai',
      model: 'gpt-5.6',
      resolved: false,
      request_id: 'rid-1',
      message: 'All available accounts exhausted',
      error_body: '{"error":"same"}',
      upstream_error_message: 'provider rate limit exhausted',
      upstream_error_detail: '{"error":"same"}',
      upstream_errors: '[]',
      account_name: 'account',
      group_name: 'group',
      is_business_limited: false
    })

    const wrapper = shallowMount(OpsErrorDetailModal, {
      props: { show: true, errorId: 1, errorType: 'request' },
      global: {
        stubs: {
          BaseDialog: { template: '<div><slot /></div>' },
          Icon: true
        }
      }
    })
    await flushPromises()

    expect(wrapper.text()).toContain('provider rate limit exhausted')
    expect(wrapper.text()).toContain('admin.ops.errorDetail.upstreamStatus')
    expect(wrapper.text()).toContain('429')
    expect(wrapper.findAll('pre')).toHaveLength(2)
    expect(wrapper.text()).not.toContain('admin.ops.errorDetail.payloads.upstream_detail')
  })
})
