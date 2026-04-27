import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import AccountStatsModal from '../AccountStatsModal.vue'
import type { Account, AccountUsageStatsResponse } from '@/types'

const { getStats } = vi.hoisted(() => ({
  getStats: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      getStats
    }
  }
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  const messages: Record<string, string> = {
    'admin.accounts.stats.noData': 'No usage data available for this account',
    'admin.accounts.stats.loadFailed': 'Failed to load account usage statistics',
    'admin.accounts.stats.totalRequests': '30-Day Total Requests',
    'admin.accounts.stats.totalCalls': 'Accumulated calls',
    'admin.accounts.stats.totalCost': '30-Day Total Cost',
    'admin.accounts.stats.accumulatedCost': 'Accumulated cost',
    'admin.accounts.stats.avgDailyCost': 'Average daily cost',
    'admin.accounts.stats.basedOnActualDays': 'Based on {days} actual usage days',
    'admin.accounts.stats.avgDailyRequests': 'Average daily requests',
    'admin.accounts.stats.requests': 'Requests',
    'admin.accounts.usageStatistics': 'Usage Statistics',
    'admin.accounts.last30DaysUsage': 'Last 30 days usage',
    'usage.accountBilled': 'Account billed',
    'usage.userBilled': 'User billed',
    'common.close': 'Close'
  }
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, unknown>) => {
        let msg = messages[key] ?? key
        if (params) {
          for (const [paramKey, value] of Object.entries(params)) {
            msg = msg.replace(`{${paramKey}}`, String(value))
          }
        }
        return msg
      }
    })
  }
})

vi.mock('chart.js', () => ({
  Chart: { register: vi.fn() },
  CategoryScale: {},
  LinearScale: {},
  PointElement: {},
  LineElement: {},
  Title: {},
  Tooltip: {},
  Legend: {},
  Filler: {}
}))

vi.mock('vue-chartjs', () => ({
  Line: { template: '<div data-test="line-chart" />' }
}))

function makeAccount(overrides: Partial<Account> = {}): Account {
  return {
    id: 123,
    name: 'test-account',
    platform: 'openai',
    type: 'oauth',
    proxy_id: null,
    concurrency: 1,
    priority: 50,
    status: 'active',
    error_message: null,
    last_used_at: null,
    expires_at: null,
    auto_pause_on_expired: true,
    created_at: '2026-04-01T00:00:00Z',
    updated_at: '2026-04-01T00:00:00Z',
    schedulable: true,
    rate_limited_at: null,
    rate_limit_reset_at: null,
    overload_until: null,
    temp_unschedulable_until: null,
    temp_unschedulable_reason: null,
    session_window_start: null,
    session_window_end: null,
    session_window_status: null,
    ...overrides
  }
}

function makeStats(requests: number): AccountUsageStatsResponse {
  return {
    history: requests > 0
      ? [
          {
            date: '2026-04-26',
            label: '04/26',
            requests,
            tokens: 300,
            cost: 0.1,
            actual_cost: 0.1,
            user_cost: 0.2
          }
        ]
      : [],
    summary: {
      days: 30,
      actual_days_used: requests > 0 ? 1 : 0,
      total_cost: requests > 0 ? 0.1 : 0,
      total_user_cost: requests > 0 ? 0.2 : 0,
      total_standard_cost: requests > 0 ? 0.1 : 0,
      total_requests: requests,
      total_tokens: requests > 0 ? 300 : 0,
      avg_daily_cost: requests > 0 ? 0.1 : 0,
      avg_daily_user_cost: requests > 0 ? 0.2 : 0,
      avg_daily_requests: requests,
      avg_daily_tokens: requests > 0 ? 300 : 0,
      avg_duration_ms: 100,
      today: null,
      highest_cost_day: null,
      highest_request_day: null
    },
    models: [],
    endpoints: [],
    upstream_endpoints: []
  }
}

function mountModal(account = makeAccount()) {
  return mount(AccountStatsModal, {
    props: {
      show: true,
      account
    },
    global: {
      stubs: {
        BaseDialog: {
          props: ['show'],
          template: '<section v-if="show"><slot /><slot name="footer" /></section>'
        },
        LoadingSpinner: { template: '<div data-test="loading" />' },
        ModelDistributionChart: { template: '<div data-test="model-chart" />' },
        EndpointDistributionChart: { template: '<div data-test="endpoint-chart" />' },
        Icon: { template: '<span data-test="icon" />' }
      }
    }
  })
}

describe('Admin AccountStatsModal', () => {
  beforeEach(() => {
    getStats.mockReset()
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('loads account stats immediately when mounted with show=true', async () => {
    getStats.mockResolvedValue(makeStats(2))

    const wrapper = mountModal()
    await flushPromises()

    expect(getStats).toHaveBeenCalledWith(123, 30)
    expect(wrapper.text()).toContain('30-Day Total Requests')
    expect(wrapper.text()).not.toContain('No usage data available for this account')
  })

  it('shows no-data only for a successful but empty stats response', async () => {
    getStats.mockResolvedValue(makeStats(0))

    const wrapper = mountModal()
    await flushPromises()

    expect(wrapper.text()).toContain('No usage data available for this account')
  })

  it('shows an error state instead of no-data when stats loading fails', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    getStats.mockRejectedValue({ message: 'database timeout' })

    const wrapper = mountModal()
    await flushPromises()

    expect(wrapper.text()).toContain('Failed to load account usage statistics')
    expect(wrapper.text()).toContain('database timeout')
    expect(wrapper.text()).not.toContain('No usage data available for this account')
  })
})
