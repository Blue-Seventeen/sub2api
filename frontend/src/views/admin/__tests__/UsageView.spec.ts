import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import UsageView from '../UsageView.vue'

const { list, getStats, getSnapshotV2, getModelStats, getById, adminUsageList, saveAsMock } = vi.hoisted(() => {
  vi.stubGlobal('localStorage', {
    getItem: vi.fn(() => null),
    setItem: vi.fn(),
    removeItem: vi.fn(),
  })

  return {
    list: vi.fn(),
    getStats: vi.fn(),
    getSnapshotV2: vi.fn(),
    getModelStats: vi.fn(),
    getById: vi.fn(),
    adminUsageList: vi.fn(),
    saveAsMock: vi.fn(),
  }
})

const messages: Record<string, string> = {
  'admin.dashboard.timeRange': 'Time Range',
  'admin.dashboard.day': 'Day',
  'admin.dashboard.hour': 'Hour',
  'admin.usage.failedToLoadUser': 'Failed to load user',
}

const readBlobText = (blob: Blob): Promise<string> => new Promise((resolve, reject) => {
  const reader = new FileReader()
  reader.onload = () => resolve(String(reader.result))
  reader.onerror = () => reject(reader.error)
  reader.readAsText(blob)
})

const formatLocalDate = (date: Date): string => {
  const year = date.getFullYear()
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  return `${year}-${month}-${day}`
}

vi.mock('@/api/admin', () => ({
  adminAPI: {
    usage: {
      list,
      getStats,
    },
    dashboard: {
      getSnapshotV2,
      getModelStats,
    },
    users: {
      getById,
    },
  },
}))

vi.mock('@/api/admin/usage', () => ({
  adminUsageAPI: {
    list: adminUsageList,
  },
}))

vi.mock('file-saver', () => ({
  saveAs: saveAsMock,
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: vi.fn(),
    showWarning: vi.fn(),
    showSuccess: vi.fn(),
    showInfo: vi.fn(),
  }),
}))

vi.mock('@/utils/format', () => ({
  formatReasoningEffort: (value: string | null | undefined) => value ?? '-',
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

vi.mock('vue-router', () => ({
  useRoute: () => ({
    query: {}
  })
}))

const AppLayoutStub = { template: '<div><slot /></div>' }
const UsageFiltersStub = { template: '<div><slot name="after-reset" /></div>' }
const ModelDistributionChartStub = {
  props: ['metric'],
  emits: ['update:metric'],
  template: `
    <div data-test="model-chart">
      <span class="metric">{{ metric }}</span>
      <button class="switch-metric" @click="$emit('update:metric', 'actual_cost')">switch</button>
    </div>
  `,
}
const GroupDistributionChartStub = {
  props: ['metric'],
  emits: ['update:metric'],
  template: `
    <div data-test="group-chart">
      <span class="metric">{{ metric }}</span>
      <button class="switch-metric" @click="$emit('update:metric', 'actual_cost')">switch</button>
    </div>
  `,
}

describe('admin UsageView distribution metric toggles', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    list.mockReset()
    adminUsageList.mockReset()
    saveAsMock.mockReset()
    getStats.mockReset()
    getSnapshotV2.mockReset()
    getModelStats.mockReset()
    getById.mockReset()

    list.mockResolvedValue({
      items: [],
      total: 0,
      pages: 0,
    })
    getStats.mockResolvedValue({
      total_requests: 0,
      total_input_tokens: 0,
      total_output_tokens: 0,
      total_cache_tokens: 0,
      total_tokens: 0,
      total_cost: 0,
      total_actual_cost: 0,
      average_duration_ms: 0,
    })
    getSnapshotV2.mockResolvedValue({
      trend: [],
      models: [],
      groups: [],
    })
    getModelStats.mockResolvedValue({
      models: [],
    })
    adminUsageList.mockResolvedValue({
      items: [],
      total: 0,
      pages: 0,
    })
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('keeps model and group metric toggles independent without refetching chart data', async () => {
    const wrapper = mount(UsageView, {
      global: {
        stubs: {
          AppLayout: AppLayoutStub,
          UsageStatsCards: true,
          UsageFilters: UsageFiltersStub,
          UsageTable: true,
          UsageExportProgress: true,
          UsageCleanupDialog: true,
          UserBalanceHistoryModal: true,
          Pagination: true,
          Select: true,
          DateRangePicker: true,
          Icon: true,
          TokenUsageTrend: true,
          ModelDistributionChart: ModelDistributionChartStub,
          GroupDistributionChart: GroupDistributionChartStub,
        },
      },
    })

    vi.advanceTimersByTime(120)
    await flushPromises()

    expect(getSnapshotV2).toHaveBeenCalledTimes(1)
    const now = new Date()
    const yesterday = new Date(now.getTime() - 24 * 60 * 60 * 1000)
    expect(getSnapshotV2).toHaveBeenCalledWith(expect.objectContaining({
      start_date: formatLocalDate(yesterday),
      end_date: formatLocalDate(now),
      granularity: 'hour'
    }))

    const modelChart = wrapper.find('[data-test="model-chart"]')
    const groupChart = wrapper.find('[data-test="group-chart"]')

    expect(modelChart.find('.metric').text()).toBe('tokens')
    expect(groupChart.find('.metric').text()).toBe('tokens')

    await modelChart.find('.switch-metric').trigger('click')
    await flushPromises()

    expect(modelChart.find('.metric').text()).toBe('actual_cost')
    expect(groupChart.find('.metric').text()).toBe('tokens')
    expect(getSnapshotV2).toHaveBeenCalledTimes(1)

    await groupChart.find('.switch-metric').trigger('click')
    await flushPromises()

    expect(modelChart.find('.metric').text()).toBe('actual_cost')
    expect(groupChart.find('.metric').text()).toBe('actual_cost')
    expect(getSnapshotV2).toHaveBeenCalledTimes(1)
  })

  it('requests exact export total only on the first page', async () => {
    const makeLog = (id: number) => ({
      id,
      created_at: `2026-04-29T00:${String(id % 60).padStart(2, '0')}:00Z`,
      model: id === 101 ? '=cmd|test' : 'gpt-test',
      api_key: { name: id === 101 ? '+api-key' : 'key' },
      account: { name: id === 101 ? '@account' : 'account' },
      request_id: id === 101 ? '-request' : `req-${id}`,
      user_agent: id === 101 ? '\tagent' : 'agent',
      input_tokens: 1,
      output_tokens: 2,
      cache_read_tokens: 0,
      cache_creation_tokens: 0,
      input_cost: 0,
      output_cost: 0,
      cache_read_cost: 0,
      cache_creation_cost: 0,
      total_cost: 0,
      actual_cost: 0,
      duration_ms: 10,
    })

    adminUsageList
      .mockResolvedValueOnce({
        items: Array.from({ length: 100 }, (_, idx) => makeLog(idx + 1)),
        total: 101,
        pages: 2,
      })
      .mockResolvedValueOnce({
        items: [makeLog(101)],
        total: 101,
        pages: 2,
      })

    const ExportUsageFiltersStub = {
      emits: ['export'],
      template: '<button class="export-usage" @click="$emit(\'export\')">export</button>',
    }

    const wrapper = mount(UsageView, {
      global: {
        stubs: {
          AppLayout: AppLayoutStub,
          UsageStatsCards: true,
          UsageFilters: ExportUsageFiltersStub,
          UsageTable: true,
          UsageExportProgress: true,
          UsageCleanupDialog: true,
          UserBalanceHistoryModal: true,
          Pagination: true,
          Select: true,
          DateRangePicker: true,
          Icon: true,
          TokenUsageTrend: true,
          ModelDistributionChart: ModelDistributionChartStub,
          GroupDistributionChart: GroupDistributionChartStub,
          EndpointDistributionChart: true,
        },
      },
    })

    vi.advanceTimersByTime(120)
    await flushPromises()

    await wrapper.find('.export-usage').trigger('click')
    await flushPromises()
    await flushPromises()

    expect(adminUsageList).toHaveBeenCalledTimes(2)
    expect(adminUsageList.mock.calls[0][0]).toEqual(expect.objectContaining({
      page: 1,
      page_size: 100,
      exact_total: true,
    }))
    expect(adminUsageList.mock.calls[1][0]).toEqual(expect.objectContaining({
      page: 2,
      page_size: 100,
      exact_total: false,
    }))
    expect(saveAsMock).toHaveBeenCalledTimes(1)
    expect(saveAsMock.mock.calls[0][1]).toMatch(/\.csv$/)
    expect(saveAsMock.mock.calls[0][0]).toBeInstanceOf(Blob)
    vi.useRealTimers()
    const csvText = await readBlobText(saveAsMock.mock.calls[0][0])
    expect(csvText).toContain("'+api-key")
    expect(csvText).toContain("'@account")
    expect(csvText).toContain("'=cmd|test")
    expect(csvText).toContain("'-request")
    expect(csvText).toContain("'\tagent")
  })
})
