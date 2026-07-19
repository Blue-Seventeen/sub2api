import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import UsageView from '../UsageView.vue'

const {
  query,
  getStats,
  getDashboardModels,
  getDashboardSnapshotV2,
  list,
  getAvailable,
  showError,
  showWarning,
  showSuccess,
  showInfo,
} = vi.hoisted(() => ({
  query: vi.fn(),
  getStats: vi.fn(),
  getDashboardModels: vi.fn(),
  getDashboardSnapshotV2: vi.fn(),
  list: vi.fn(),
  getAvailable: vi.fn(),
  showError: vi.fn(),
  showWarning: vi.fn(),
  showSuccess: vi.fn(),
  showInfo: vi.fn(),
}))

const messages: Record<string, string> = {
  'admin.dashboard.timeRange': 'Time range',
  'admin.dashboard.granularity': 'Granularity',
  'admin.dashboard.day': 'Day',
  'admin.dashboard.hour': 'Hour',
  'admin.users.columnSettings': 'Columns',
  'admin.usage.group': 'Group',
  'admin.usage.billingType': 'Billing type',
  'admin.usage.billingMode': 'Billing mode',
  'admin.usage.allTypes': 'All types',
  'admin.usage.allBillingTypes': 'All billing types',
  'admin.usage.billingTypeBalance': 'Balance',
  'admin.usage.billingTypeSubscription': 'Subscription',
  'admin.usage.allBillingModes': 'All billing modes',
  'admin.usage.billingModeToken': 'Token',
  'admin.usage.billingModePerRequest': 'Per request',
  'admin.usage.billingModeImage': 'Image',
  'admin.usage.billingModeDuration': 'Duration',
  'admin.usage.billingModeCharacter': 'Character',
  'admin.usage.allGroups': 'All groups',
  'admin.usage.allModels': 'All models',
  'usage.allApiKeys': 'All API Keys',
  'usage.apiKeyFilter': 'API Key',
  'usage.model': 'Model',
  'usage.type': 'Type',
  'usage.ws': 'WS',
  'usage.stream': 'Stream',
  'usage.sync': 'Sync',
  'usage.exporting': 'Exporting',
  'usage.exportCsv': 'Export CSV',
  'usage.failedToLoad': 'Failed to load',
  'usage.noDataToExport': 'No data',
  'usage.preparingExport': 'Preparing export',
  'usage.exportSuccess': 'Export success',
  'usage.exportFailed': 'Export failed',
  'common.refresh': 'Refresh',
  'common.reset': 'Reset',
}

vi.mock('@/api', () => ({
  usageAPI: {
    query,
    getStats,
    getDashboardModels,
    getDashboardSnapshotV2,
  },
  keysAPI: {
    list,
  },
  userGroupsAPI: {
    getAvailable,
  },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError, showWarning, showSuccess, showInfo }),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => messages[key] ?? key,
    }),
  }
})

const simpleStub = { template: '<div><slot /></div>' }
const namedStub = (name: string, props: string[] = []) => ({
  name,
  props,
  template: `<div data-test="${name}" />`,
})

const usageLog = {
  id: 1,
  request_id: 'req-user-export',
  actual_cost: 0.092883,
  real_actual_cost: 0.081,
  total_cost: 0.092883,
  rate_multiplier: 1,
  service_tier: 'priority',
  input_cost: 0.020285,
  output_cost: 0.00303,
  cache_creation_cost: 0.000001,
  cache_read_cost: 0.069568,
  input_tokens: 4057,
  output_tokens: 200,
  image_output_tokens: 150,
  image_output_cost: 0.0123,
  cache_creation_tokens: 4,
  cache_read_tokens: 278272,
  cache_creation_5m_tokens: 0,
  cache_creation_1h_tokens: 0,
  image_count: 0,
  image_size: null,
  billable_duration_seconds: 12,
  billable_character_count: 345,
  first_token_ms: 12,
  duration_ms: 345,
  created_at: '2026-03-08T00:00:00Z',
  model: 'gpt-5.4',
  reasoning_effort: null,
  api_key: { name: 'demo-key' },
  billing_mode: 'token',
  request_type: 'sync',
  stream: false,
}

function mountUsageView() {
  return mount(UsageView, {
    global: {
      stubs: {
        AppLayout: simpleStub,
        Pagination: true,
        Select: true,
        DateRangePicker: true,
        Icon: true,
        UsageStatsCards: namedStub('UsageStatsCards', ['costDisplayMode']),
        UsageTable: namedStub('UsageTable', ['costDisplayMode']),
        ModelDistributionChart: namedStub('ModelDistributionChart', ['costDisplayMode']),
        GroupDistributionChart: namedStub('GroupDistributionChart', ['costDisplayMode']),
        EndpointDistributionChart: namedStub('EndpointDistributionChart', ['costDisplayMode']),
        TokenUsageTrend: namedStub('TokenUsageTrend', ['costDisplayMode']),
      },
    },
  })
}

function stubCsvDownload() {
  let csvContent = ''
  const OriginalBlob = globalThis.Blob
  vi.stubGlobal('Blob', vi.fn((parts: BlobPart[], options?: BlobPropertyBag) => {
    csvContent = parts.map((part) => String(part)).join('')
    return new OriginalBlob(parts, options)
  }))
  const originalCreateObjectURL = window.URL.createObjectURL
  const originalRevokeObjectURL = window.URL.revokeObjectURL
  window.URL.createObjectURL = vi.fn(() => 'blob:usage-export') as typeof window.URL.createObjectURL
  window.URL.revokeObjectURL = vi.fn(() => {}) as typeof window.URL.revokeObjectURL
  const clickSpy = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})

  return {
    getCsv: () => csvContent,
    restore: () => {
      window.URL.createObjectURL = originalCreateObjectURL
      window.URL.revokeObjectURL = originalRevokeObjectURL
      vi.unstubAllGlobals()
      clickSpy.mockRestore()
    },
    clickSpy,
  }
}

describe('user UsageView', () => {
  beforeEach(() => {
    query.mockReset()
    getStats.mockReset()
    getDashboardModels.mockReset()
    getDashboardSnapshotV2.mockReset()
    list.mockReset()
    getAvailable.mockReset()
    showError.mockReset()
    showWarning.mockReset()
    showSuccess.mockReset()
    showInfo.mockReset()

    query.mockResolvedValue({ items: [usageLog], total: 1, pages: 1 })
    getStats.mockResolvedValue({
      total_requests: 1,
      total_input_tokens: 10,
      total_output_tokens: 20,
      total_cache_tokens: 0,
      total_tokens: 30,
      total_cost: 0.1,
      total_actual_cost: 0.08,
      real_total_actual_cost: 0.07,
      average_duration_ms: 12,
      endpoints: [],
      upstream_endpoints: [],
      endpoint_paths: [],
    })
    getDashboardModels.mockResolvedValue({
      models: [{ model: 'gpt-5.4', requests: 1, input_tokens: 10, output_tokens: 20, cache_creation_tokens: 0, cache_read_tokens: 0, total_tokens: 30, cost: 0.1, actual_cost: 0.08, real_actual_cost: 0.07 }],
      start_date: '2026-03-08',
      end_date: '2026-03-08',
    })
    getDashboardSnapshotV2.mockResolvedValue({
      generated_at: '2026-03-08T00:00:00Z',
      start_date: '2026-03-08',
      end_date: '2026-03-08',
      granularity: 'hour',
      trend: [],
      groups: [],
    })
    list.mockResolvedValue({ items: [{ id: 1, name: 'demo-key' }] })
    getAvailable.mockResolvedValue([{ id: 1, name: 'default' }])
  })

  it('loads logs, stats, model stats, and snapshot on first render', async () => {
    mountUsageView()
    await flushPromises()

    expect(query).toHaveBeenCalled()
    expect(getStats).toHaveBeenCalled()
    expect(getDashboardModels).toHaveBeenCalled()
    expect(getDashboardSnapshotV2).toHaveBeenCalledWith(expect.objectContaining({
      include_trend: true,
      include_model_stats: false,
      include_group_stats: true,
    }))
    expect(list).toHaveBeenCalledWith(1, 100)
    expect(getAvailable).toHaveBeenCalled()
  })

  it('passes display cost mode to user usage widgets', async () => {
    const wrapper = mountUsageView()
    await flushPromises()

    expect(wrapper.findComponent({ name: 'UsageStatsCards' }).props('costDisplayMode')).toBe('display')
    expect(wrapper.findComponent({ name: 'UsageTable' }).props('costDisplayMode')).toBe('display')
    expect(wrapper.findComponent({ name: 'ModelDistributionChart' }).props('costDisplayMode')).toBe('display')
    expect(wrapper.findComponent({ name: 'GroupDistributionChart' }).props('costDisplayMode')).toBe('display')
    expect(wrapper.findComponent({ name: 'EndpointDistributionChart' }).props('costDisplayMode')).toBe('display')
    expect(wrapper.findComponent({ name: 'TokenUsageTrend' }).props('costDisplayMode')).toBe('display')
  })

  it('exports csv with user-safe fields and custom billing columns', async () => {
    const wrapper = mountUsageView()
    await flushPromises()
    const csv = stubCsvDownload()

    await (wrapper.vm as any).exportToCSV()

    expect(query).toHaveBeenCalledWith(expect.objectContaining({
      page_size: 100,
      sort_by: 'created_at',
      sort_order: 'desc',
    }))
    expect(csv.clickSpy).toHaveBeenCalled()
    expect(showSuccess).toHaveBeenCalled()
    const csvContent = csv.getCsv()
    expect(csvContent.startsWith('\uFEFF')).toBe(true)
    expect(csvContent).not.toContain('IP Address')
    expect(csvContent).toContain('Image Output Tokens')
    expect(csvContent).toContain('Billable Duration Seconds')
    expect(csvContent).toContain('Billable Characters')
    expect(csvContent).toContain('Billed Cost')
    expect(csvContent).toContain('Original Cost')
    expect(csvContent).toContain('Image Output Cost')
    expect(csvContent).not.toContain('203.0.113.10')
    expect(csvContent).toContain(',4057,50,278272,4,150,12,345,')
    expect(csvContent).toContain('0.09288300')
    expect(csvContent).not.toContain('0.08100000')
    expect(csvContent).not.toContain('Upstream Endpoint')
    expect(csvContent).not.toContain('account_cost')
    expect(csvContent).not.toContain('account_rate_multiplier')

    csv.restore()
  })

  it('exports historical image rows with image billing mode derived from image_count', async () => {
    query.mockResolvedValue({
      items: [
        {
          ...usageLog,
          request_id: 'req-user-export-legacy-image',
          actual_cost: 0.2,
          total_cost: 0.2,
          input_tokens: 0,
          output_tokens: 0,
          image_output_tokens: 0,
          image_output_cost: 0,
          cache_creation_tokens: 0,
          cache_read_tokens: 0,
          image_count: 1,
          model: 'gpt-image-2',
          billing_mode: null,
        },
      ],
      total: 1,
      pages: 1,
    })
    const wrapper = mountUsageView()
    await flushPromises()
    const csv = stubCsvDownload()

    await (wrapper.vm as any).exportToCSV()

    expect(csv.getCsv()).toContain('Billing Mode')
    expect(csv.getCsv()).toContain('Image')
    expect(csv.getCsv()).not.toContain(',Token,0,0,0,0,')

    csv.restore()
  })
})
