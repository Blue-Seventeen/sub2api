import { describe, expect, it } from 'vitest'
import { createGroupPricing, groupPricingFromAPI, groupPricingToAPI } from '../groupsModelPricing'
import type { ChannelModelPricing } from '@/api/admin/channels'

const card: ChannelModelPricing & { max_reasoning_effort_multiplier: number } = {
  platform: 'anthropic', models: ['claude-*'], billing_mode: 'token',
  input_price: 0.00000005, output_price: 0.000002, cache_write_price: null,
  cache_write_1h_price: 0, cache_read_price: 0,
  image_input_price: null, image_output_price: null, per_request_price: null,
  fast_multiplier: 2, flex_multiplier: 0.5, max_reasoning_effort_multiplier: 3,
  reasoning_effort_multipliers: { high: 1.5 }, intervals: [], time_pricing: null,
}

describe('group pricing conversion', () => {
  it('roundtrips price units, free prices, existing platforms and hidden multiplier fields', () => {
    const form = groupPricingFromAPI([card])
    expect(form[0].input_price).toBe(0.05)
    expect(form[0].cache_read_price).toBe(0)
    expect(groupPricingToAPI(form, 'composite')).toEqual([card])
    form[0].models.push('claude-extra')
    form[0].reasoning_effort_multipliers!.high = 2
    expect(card.models).toEqual(['claude-*'])
    expect(card.reasoning_effort_multipliers).toEqual({ high: 1.5 })
  })

  it('serializes edited prices without scaling request prices and strips group token intervals', () => {
    const form = groupPricingFromAPI([card])
    form[0].input_price = '0.15'
    form[0].output_price = ''
    form[0].intervals.push({ min_tokens: 0, max_tokens: null, tier_label: '',
      input_price: 5, output_price: 10, cache_read_price: null, cache_write_price: null,
      per_request_price: null, sort_order: 0,
    })
    expect(groupPricingToAPI(form, 'composite')[0]).toMatchObject({
      input_price: 0.00000015, output_price: null, intervals: [], time_pricing: null,
      max_reasoning_effort_multiplier: 3,
    })
    form[0].billing_mode = 'per_request'
    form[0].per_request_price = '0.03'
    const result = groupPricingToAPI(form, 'composite')[0]
    expect(result.per_request_price).toBe(0.03)
    expect(result.intervals[0].input_price).toBe(0.000005)
  })

  it('uses the selected platform for new entries and explicitly serializes clearing all cards', () => {
    const empty = createGroupPricing()
    expect(groupPricingToAPI([empty], 'volcengine')).toEqual([])
    empty.models = ['doubao-*']
    empty.input_price = 0
    expect(groupPricingToAPI([empty], 'volcengine')[0]).toMatchObject({
      platform: 'volcengine', models: ['doubao-*'], input_price: 0, intervals: [], time_pricing: null,
    })
    expect(groupPricingToAPI([], 'volcengine')).toEqual([])
  })

  it.each(['token', ''])('preserves stored legacy token intervals for mode %s', (billingMode) => {
    const legacy = { ...card, billing_mode: billingMode as ChannelModelPricing['billing_mode'], intervals: [{ min_tokens: 1000, max_tokens: null,
      tier_label: 'legacy', input_price: 0.000001, output_price: null,
      cache_write_price: null, cache_read_price: null, per_request_price: null, sort_order: 0,
    }] }
    expect(groupPricingToAPI(groupPricingFromAPI([legacy]), 'anthropic')[0].intervals).toEqual(legacy.intervals)
  })
})
