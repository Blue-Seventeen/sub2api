import fs from 'node:fs'
import path from 'node:path'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import OpsConcurrencyCard from '../OpsConcurrencyCard.vue'

const mockGetConcurrencyStats = vi.fn()
const mockGetAccountAvailabilityStats = vi.fn()
const mockGetUserConcurrencyStats = vi.fn()

vi.mock('@/api/admin/ops', () => ({
  opsAPI: {
    getConcurrencyStats: (...args: any[]) => mockGetConcurrencyStats(...args),
    getAccountAvailabilityStats: (...args: any[]) => mockGetAccountAvailabilityStats(...args),
    getUserConcurrencyStats: (...args: any[]) => mockGetUserConcurrencyStats(...args),
  },
}))

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()

  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, any>) => {
        if (key === 'admin.ops.concurrency.totalRows') return `Total ${params?.count ?? 0}`
        if (key === 'admin.ops.concurrency.queued') return `Queue ${params?.count ?? 0}`
        return key
      },
    }),
  }
})

function makeUsers(count: number) {
  return Object.fromEntries(
    Array.from({ length: count }, (_, index) => {
      const id = index + 1
      return [
        String(id),
        {
          user_id: id,
          user_email: `user-${id}@example.com`,
          username: '',
          current_in_use: id % 7,
          max_capacity: 10,
          load_percentage: (id % 7) * 10,
          waiting_in_queue: id % 3,
        },
      ]
    })
  )
}

describe('OpsConcurrencyCard layout', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockGetConcurrencyStats.mockResolvedValue({
      enabled: true,
      platform: {
        openai: {
          platform: 'openai',
          current_in_use: 1,
          max_capacity: 10,
          load_percentage: 10,
          waiting_in_queue: 0,
        },
      },
      group: {},
      account: {},
    })
    mockGetAccountAvailabilityStats.mockResolvedValue({
      enabled: true,
      platform: {
        openai: {
          platform: 'openai',
          total_accounts: 2,
          available_count: 2,
          rate_limit_count: 0,
          error_count: 0,
        },
      },
      group: {},
      account: {},
    })
    mockGetUserConcurrencyStats.mockResolvedValue({
      enabled: true,
      user: makeUsers(40),
    })
  })

  it('keeps the dashboard grid cell at a fixed height instead of allowing concurrency content to stretch the row', () => {
    const source = fs.readFileSync(path.resolve(process.cwd(), 'src/views/admin/ops/OpsDashboard.vue'), 'utf8')

    expect(source).toContain('class="h-[360px] min-h-0 lg:col-span-1"')
    expect(source).not.toContain('class="lg:col-span-1 min-h-[360px]"')
  })

  it('uses an internal scroll container for long user concurrency lists', async () => {
    const wrapper = mount(OpsConcurrencyCard, {
      props: {
        refreshToken: 0,
      },
    })

    await flushPromises()

    const card = wrapper.find('.rounded-3xl')
    expect(card.classes()).toEqual(expect.arrayContaining(['h-full', 'min-h-0', 'flex-col']))

    const platformScroller = wrapper.find('.custom-scrollbar')
    expect(platformScroller.classes()).toEqual(expect.arrayContaining(['min-h-0', 'flex-1', 'overflow-y-auto']))
    expect(platformScroller.classes()).not.toContain('h-[280px]')

    await wrapper.findAll('button')[0].trigger('click')
    await flushPromises()

    expect(mockGetUserConcurrencyStats).toHaveBeenCalledTimes(1)
    expect(wrapper.text()).toContain('user-40@example.com')

    const userScroller = wrapper.find('.custom-scrollbar')
    expect(userScroller.classes()).toEqual(expect.arrayContaining(['min-h-0', 'flex-1', 'overflow-y-auto']))
    expect(userScroller.classes()).not.toContain('h-[280px]')
  })
})
