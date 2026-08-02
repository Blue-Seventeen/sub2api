import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

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
        if (key === 'common.peakRateFormula') return `${params?.window} base x peak = ${params?.base} x ${params?.peak} = ${params?.final} final`
        if (key === 'common.peakRateImageNote') return '; all billing modes include the peak multiplier'
        return key
      },
    }),
  }
})

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ cachedPublicSettings: null }),
}))

const mountOption = (props: Record<string, unknown> = {}) => mount(GroupOptionItem, {
  props: {
    name: 'peak-group',
    platform: 'openai',
    rateMultiplier: 1,
    ...props,
  },
  global: {
    stubs: {
      PlatformIcon: true,
      Teleport: true,
    },
  },
})

describe('GroupOptionItem', () => {
  afterEach(() => {
    document.body.innerHTML = ''
  })

  it('formats effective rate without floating point noise', () => {
    const wrapper = mountOption({
      rateMultiplier: 0.035,
      effectiveRateMultiplier: 0.35000000000000003,
    })

    const text = wrapper.text()
    expect(text).toContain('0.035x')
    expect(text).toContain('0.35x')
    expect(text).not.toContain('0.35000000000000003')
  })

  it('summarizes multiple peak windows in compact mode and keeps full details in the tooltip', async () => {
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

    const peakPill = wrapper.get('[data-test="group-option-peak-rate"]')
    const ratePillGroup = peakPill.element.parentElement?.parentElement
    expect(ratePillGroup?.className).toContain('flex-row')
    expect(ratePillGroup?.className).not.toContain('flex-col')
    expect(peakPill.attributes('title')).toBeUndefined()
    await peakPill.trigger('mouseenter')
    const tooltip = wrapper.get('[data-test="peak-rate-tooltip"]')
    const windowLines = tooltip.findAll('[data-test="peak-rate-window"]')

    expect(tooltip.classes()).toContain('fixed')
    expect(tooltip.classes()).toContain('z-[100000050]')
    expect(windowLines).toHaveLength(2)
    expect(windowLines[0].text()).toBe('09:00-12:00 base x peak = 1 x 1.5 = 1.5 final')
    expect(windowLines[1].text()).toBe('18:00-22:00 base x peak = 1 x 2 = 2 final')
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

    const peakPill = wrapper.get('[data-test="group-option-peak-rate"]')
    expect(peakPill.element.parentElement?.parentElement?.className).toContain('flex-col')
    expect(peakPill.classes()).toContain('whitespace-normal')
    expect(peakPill.classes()).not.toContain('whitespace-nowrap')
    expect(wrapper.classes()).toContain('flex-col')
    expect(wrapper.classes()).toContain('sm:flex-row')
  })

  it('applies multiline and overflow-safe text styles to descriptions', () => {
    const description = 'First section\nvery-long-unbroken-description-value-that-must-not-overflow'
    const wrapper = mountOption({
      name: 'Example group',
      description,
      rateMultiplier: undefined,
    })

    const descriptionElement = wrapper
      .findAll('span')
      .find((element) => element.text() === description)

    expect(descriptionElement).toBeDefined()
    expect(descriptionElement?.classes()).toContain('whitespace-pre-line')
    expect(descriptionElement?.classes()).toContain('[overflow-wrap:anywhere]')
    expect(descriptionElement?.classes()).toContain('line-clamp-3')
    expect(wrapper.find('[title]').attributes('title')).toBe(description)
  })
})
