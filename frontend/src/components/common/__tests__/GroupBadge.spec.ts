import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createPinia } from 'pinia'

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
        if (key === 'common.peakRateCompactSingle') return `Peak x${params?.multiplier}`
        if (key === 'common.peakRateCompactMultiple') return `Peak ${params?.count} windows`
        if (key === 'common.peakRateTooltip') return `Peak rate: ${params?.window}`
        return key
      },
    }),
  }
})

const mountBadge = (props: Record<string, unknown>) => mount(GroupBadge, {
  props,
  global: {
    plugins: [createPinia()],
    stubs: {
      PlatformIcon: true,
    },
  },
})

describe('GroupBadge', () => {
  it('highlights effective rate when it differs from the default rate', () => {
    const wrapper = mountBadge({
      name: 'vip-group',
      platform: 'openai',
      rateMultiplier: 1.2,
      effectiveRateMultiplier: 1.8,
    })

    const text = wrapper.text()
    expect(text).toContain('vip-group')
    expect(text).toContain('1.2x')
    expect(text).toContain('1.8x')
  })

  it('shows only the default rate when effective rate matches default rate', () => {
    const wrapper = mountBadge({
      name: 'default-group',
      platform: 'openai',
      rateMultiplier: 1.5,
      effectiveRateMultiplier: 1.5,
    })

    expect(wrapper.text()).toContain('1.5x')
    expect(wrapper.find('.line-through').exists()).toBe(false)
  })

  it('shows a compact single peak window with a full tooltip by default', () => {
    const wrapper = mountBadge({
      name: 'peak-group',
      platform: 'openai',
      rateMultiplier: 1,
      peakRateEnabled: true,
      peakRateWindows: [{ start: '09:00', end: '12:00', multiplier: 1.5 }],
    })

    expect(wrapper.text()).toContain('Peak x1.5')
    expect(wrapper.text()).not.toContain('09:00-12:00')

    const peakBadge = wrapper.find('[title*="09:00-12:00"]')
    expect(peakBadge.exists()).toBe(true)
    expect(peakBadge.attributes('title')).toContain('Peak rate:')
  })

  it('summarizes multiple peak windows in compact mode', () => {
    const wrapper = mountBadge({
      name: 'multi-peak-group',
      platform: 'openai',
      rateMultiplier: 1,
      peakRateEnabled: true,
      peakRateWindows: [
        { start: '09:00', end: '12:00', multiplier: 1.5 },
        { start: '18:00', end: '22:00', multiplier: 2 },
      ],
    })

    expect(wrapper.text()).toContain('Peak 2 windows')
    expect(wrapper.text()).not.toContain('09:00-12:00')
    expect(wrapper.text()).not.toContain('18:00-22:00')

    const title = wrapper.find('[title*="09:00-12:00"]').attributes('title')
    expect(title).toContain('18:00-22:00')
  })

  it('shows full peak windows when full mode is requested', () => {
    const wrapper = mountBadge({
      name: 'full-peak-group',
      platform: 'openai',
      rateMultiplier: 1,
      peakRateEnabled: true,
      peakDisplayMode: 'full',
      peakRateWindows: [
        { start: '09:00', end: '12:00', multiplier: 1.5 },
        { start: '18:00', end: '22:00', multiplier: 2 },
      ],
    })

    expect(wrapper.text()).toContain('09:00-12:00')
    expect(wrapper.text()).toContain('18:00-22:00')
  })
})
