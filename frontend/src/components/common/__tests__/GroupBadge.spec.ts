import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

import GroupBadge from '../GroupBadge.vue'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, unknown>) => {
        if (key === 'admin.users.daysRemaining') return `${params?.days} days`
        if (key === 'admin.users.expired') return 'Expired'
        if (key === 'groups.subscription') return 'Subscription'
        return key
      },
    }),
  }
})

describe('GroupBadge', () => {
  it('highlights effective rate when it differs from the default rate', () => {
    const wrapper = mount(GroupBadge, {
      props: {
        name: 'vip-group',
        platform: 'openai',
        rateMultiplier: 1.2,
        effectiveRateMultiplier: 1.8,
      },
      global: {
        stubs: {
          PlatformIcon: true,
        },
      },
    })

    const text = wrapper.text()
    expect(text).toContain('vip-group')
    expect(text).toContain('1.2x')
    expect(text).toContain('1.8x')
  })

  it('shows only the default rate when effective rate matches default rate', () => {
    const wrapper = mount(GroupBadge, {
      props: {
        name: 'default-group',
        platform: 'openai',
        rateMultiplier: 1.5,
        effectiveRateMultiplier: 1.5,
      },
      global: {
        stubs: {
          PlatformIcon: true,
        },
      },
    })

    expect(wrapper.text()).toContain('1.5x')
    expect(wrapper.find('.line-through').exists()).toBe(false)
  })
})
