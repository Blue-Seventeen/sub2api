import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createPinia } from 'pinia'

import GroupOptionItem from '../GroupOptionItem.vue'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, unknown>) => {
        if (key === 'admin.groups.rateLabel') return 'rate'
        if (key === 'common.peakRateCompactSingle') return `Peak x${params?.multiplier}`
        if (key === 'common.peakRateCompactMultiple') return `Peak ${params?.count} windows`
        if (key === 'common.peakRateTooltip') return `Peak rate: ${params?.window}`
        return key
      },
    }),
  }
})

const mountOption = (props: Record<string, unknown> = {}) => mount(GroupOptionItem, {
  props: {
    name: 'peak-group',
    platform: 'openai',
    rateMultiplier: 1,
    ...props,
  },
  global: {
    plugins: [createPinia()],
    stubs: {
      PlatformIcon: true,
    },
  },
})

describe('GroupOptionItem', () => {
  it('summarizes multiple peak windows in compact mode and keeps full details in the tooltip', () => {
    const wrapper = mountOption({
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
    const wrapper = mountOption({
      peakDisplayMode: 'full',
      peakRateEnabled: true,
      peakRateWindows: [
        { start: '09:00', end: '12:00', multiplier: 1.5 },
        { start: '18:00', end: '22:00', multiplier: 2 },
      ],
    })

    expect(wrapper.text()).toContain('09:00-12:00')
    expect(wrapper.text()).toContain('18:00-22:00')

    const peakPill = wrapper.find('[title*="09:00-12:00"]')
    expect(peakPill.classes()).toContain('whitespace-normal')
    expect(peakPill.classes()).not.toContain('whitespace-nowrap')
    expect(wrapper.classes()).toContain('flex-col')
    expect(wrapper.classes()).toContain('sm:flex-row')
  })
})
