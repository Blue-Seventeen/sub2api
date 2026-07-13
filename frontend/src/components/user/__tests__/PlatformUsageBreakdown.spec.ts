import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

import PlatformUsageBreakdown from '../PlatformUsageBreakdown.vue'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key,
    }),
  }
})

describe('PlatformUsageBreakdown', () => {
  it('prefers real cost values when available', () => {
    const wrapper = mount(PlatformUsageBreakdown, {
      props: {
        today: 0.5,
        total: 1.9,
        byPlatform: [
          {
            platform: 'openai',
            today_actual_cost: 0.3,
            total_actual_cost: 1.5,
            real_today_actual_cost: 0.2,
            real_total_actual_cost: 1.2,
          },
          {
            platform: 'anthropic',
            today_actual_cost: 0.2,
            total_actual_cost: 0.4,
            real_today_actual_cost: 0.1,
            real_total_actual_cost: 0.7,
          },
        ],
      },
      global: {
        stubs: {
          Icon: true,
        },
      },
    })

    expect(wrapper.text()).toContain('$0.2000 / $1.2000')
    expect(wrapper.text()).toContain('$0.1000 / $0.7000')
    expect(wrapper.text()).not.toContain('$0.3000 / $1.5000')
    expect(wrapper.text()).not.toContain('$0.2000 / $0.4000')
  })

  it('falls back to actual cost values when real costs are absent', () => {
    const wrapper = mount(PlatformUsageBreakdown, {
      props: {
        today: 0.5,
        total: 1.9,
        byPlatform: [
          {
            platform: 'openai',
            today_actual_cost: 0.3,
            total_actual_cost: 1.5,
          },
        ],
      },
      global: {
        stubs: {
          Icon: true,
        },
      },
    })

    expect(wrapper.text()).toContain('$0.3000 / $1.5000')
  })
})
