import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createPinia } from 'pinia'

import GroupSelector from '../GroupSelector.vue'
import type { AdminGroup } from '@/types'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, unknown>) => {
        if (key === 'admin.groups.rateAndAccounts') return `${params?.rate}x / ${params?.count} accounts`
        if (key === 'common.selectedCount') return `${params?.count} selected`
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

const makeGroup = (overrides: Partial<AdminGroup> = {}): AdminGroup => ({
  id: 1,
  name: 'peak-group',
  description: null,
  platform: 'openai',
  rate_multiplier: 1,
  is_exclusive: false,
  status: 'active',
  subscription_type: 'standard',
  daily_limit_usd: null,
  weekly_limit_usd: null,
  monthly_limit_usd: null,
  custom_limit_hours: 0,
  custom_limit_usd: null,
  allow_image_generation: true,
  image_rate_independent: false,
  image_rate_multiplier: 1,
  image_price_1k: null,
  image_price_2k: null,
  image_price_4k: null,
  peak_rate_enabled: true,
  peak_start: '09:00',
  peak_end: '12:00',
  peak_rate_multiplier: 1.5,
  peak_rate_windows: [{ start: '09:00', end: '12:00', multiplier: 1.5 }],
  claude_code_only: false,
  fallback_group_id: null,
  fallback_group_id_on_invalid_request: null,
  require_oauth_only: false,
  require_privacy_set: false,
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-01T00:00:00Z',
  model_routing: null,
  model_routing_enabled: false,
  mcp_xml_inject: false,
  sort_order: 0,
  account_count: 2,
  active_account_count: 2,
  rate_limited_account_count: 0,
  ...overrides,
})

describe('GroupSelector', () => {
  afterEach(() => {
    document.body.innerHTML = ''
  })

  it('shows peak-rate windows compactly and keeps full details in the badge tooltip', async () => {
    const wrapper = mount(GroupSelector, {
      props: {
        modelValue: [],
        groups: [makeGroup({
          peak_rate_windows: [
            { start: '09:00', end: '12:00', multiplier: 1.5 },
            { start: '18:00', end: '22:00', multiplier: 2 },
          ],
        })],
      },
      global: {
        plugins: [createPinia()],
        stubs: {
          Icon: true,
          PlatformIcon: true,
          Teleport: true,
        },
      },
    })

    const text = wrapper.text()
    expect(text).toContain('peak-group')
    expect(text).toContain('Peak 2 windows')
    expect(text).not.toContain('09:00-12:00')
    expect(text).not.toContain('18:00-22:00')

    const peakPill = wrapper.get('[data-test="group-badge-peak-rate"]')
    expect(peakPill.attributes('title')).toBeUndefined()
    await peakPill.trigger('mouseenter')
    const windowLines = wrapper.findAll('[data-test="peak-rate-window"]')

    expect(windowLines).toHaveLength(2)
    expect(windowLines[0].text()).toBe('09:00-12:00 base x peak = 1 x 1.5 = 1.5 final')
    expect(windowLines[1].text()).toBe('18:00-22:00 base x peak = 1 x 2 = 2 final')
  })
})
