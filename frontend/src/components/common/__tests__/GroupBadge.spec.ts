import { afterEach, describe, expect, it, vi } from 'vitest'
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
        if (key === 'common.peakRateFormula') return `${params?.window} base x peak = ${params?.base} x ${params?.peak} = ${params?.final} final`
        if (key === 'common.peakRateImageNote') return '; all billing modes include the peak multiplier'
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
      Teleport: true,
    },
  },
})

describe('GroupBadge', () => {
  afterEach(() => {
    document.body.innerHTML = ''
  })

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

  it('formats effective rate without floating point noise', () => {
    const wrapper = mountBadge({
      name: 'precision-group',
      platform: 'zhipu',
      rateMultiplier: 0.035,
      effectiveRateMultiplier: 0.35000000000000003,
    })

    const text = wrapper.text()
    expect(text).toContain('0.035x')
    expect(text).toContain('0.35x')
    expect(text).not.toContain('0.35000000000000003')
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

  it('shows a compact single peak window with a full tooltip by default', async () => {
    const wrapper = mountBadge({
      name: 'peak-group',
      platform: 'openai',
      rateMultiplier: 1,
      peakRateEnabled: true,
      peakRateWindows: [{ start: '09:00', end: '12:00', multiplier: 1.5 }],
    })

    expect(wrapper.text()).toContain('Peak x1.5')
    expect(wrapper.text()).not.toContain('09:00-12:00')

    const peakBadge = wrapper.get('[data-test="group-badge-peak-rate"]')
    expect(peakBadge.attributes('title')).toBeUndefined()
    await peakBadge.trigger('mouseenter')
    const tooltip = wrapper.get('[data-test="peak-rate-tooltip"]')
    const windowLines = tooltip.findAll('[data-test="peak-rate-window"]')

    expect(peakBadge.exists()).toBe(true)
    expect(tooltip.classes()).toContain('fixed')
    expect(tooltip.classes()).toContain('z-[100000050]')
    expect(tooltip.text()).toContain('Peak rate:')
    expect(windowLines).toHaveLength(1)
    expect(windowLines[0].text()).toBe('09:00-12:00 base x peak = 1 x 1.5 = 1.5 final')
  })

  it('summarizes multiple peak windows in compact mode', async () => {
    const wrapper = mountBadge({
      name: 'multi-peak-group',
      platform: 'openai',
      rateMultiplier: 1.2,
      effectiveRateMultiplier: 1.8,
      peakRateEnabled: true,
      peakRateWindows: [
        { start: '09:00', end: '12:00', multiplier: 1.5 },
        { start: '18:00', end: '22:00', multiplier: 2 },
      ],
    })

    expect(wrapper.text()).toContain('Peak 2 windows')
    expect(wrapper.text()).not.toContain('09:00-12:00')
    expect(wrapper.text()).not.toContain('18:00-22:00')

    const peakBadge = wrapper.get('[data-test="group-badge-peak-rate"]')
    await peakBadge.trigger('mouseenter')
    const windowLines = wrapper.findAll('[data-test="peak-rate-window"]')
    expect(windowLines).toHaveLength(2)
    expect(windowLines[0].text()).toBe('09:00-12:00 base x peak = 1.8 x 1.5 = 2.7 final')
    expect(windowLines[1].text()).toBe('18:00-22:00 base x peak = 1.8 x 2 = 3.6 final')
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
