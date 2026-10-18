import type { ChannelModelPricing } from '@/api/admin/channels'
import {
  apiIntervalsToForm, createDefaultTimePricingForm, formIntervalsToAPI,
  formReasoningEffortMultipliersToAPI, mTokToPerToken, perTokenToMTok, toNullableNumber,
  type PricingFormEntry,
} from '@/components/admin/channel/types'

export interface GroupPricingFormEntry extends PricingFormEntry {
  formKey?: number
  // Retain API fields that this editor does not expose, including legacy multipliers.
  original?: ChannelModelPricing
}

let nextFormKey = 0

export function createGroupPricing(): GroupPricingFormEntry {
  return {
    formKey: ++nextFormKey,
    models: [], billing_mode: 'token', input_price: null, output_price: null,
    cache_write_price: null, cache_write_1h_price: null, cache_read_price: null,
    image_input_price: null, image_output_price: null, per_request_price: null,
    intervals: [], time_pricing: createDefaultTimePricingForm(),
  }
}

export function groupPricingFromAPI(pricing: ChannelModelPricing[] | undefined): GroupPricingFormEntry[] {
  return (pricing || []).map(entry => ({
    formKey: ++nextFormKey,
    original: { ...entry },
    models: [...entry.models], billing_mode: entry.billing_mode || 'token',
    input_price: perTokenToMTok(entry.input_price),
    output_price: perTokenToMTok(entry.output_price),
    cache_write_price: perTokenToMTok(entry.cache_write_price),
    cache_write_1h_price: perTokenToMTok(entry.cache_write_1h_price),
    cache_read_price: perTokenToMTok(entry.cache_read_price),
    fast_multiplier: entry.fast_multiplier, flex_multiplier: entry.flex_multiplier,
    reasoning_effort_multipliers: entry.reasoning_effort_multipliers
      ? { ...entry.reasoning_effort_multipliers } : null,
    image_input_price: perTokenToMTok(entry.image_input_price),
    image_output_price: perTokenToMTok(entry.image_output_price),
    per_request_price: entry.per_request_price,
    intervals: apiIntervalsToForm(entry.intervals || []),
    time_pricing: createDefaultTimePricingForm(),
  }))
}

export function groupPricingToAPI(pricing: GroupPricingFormEntry[], platform: string): ChannelModelPricing[] {
  return pricing.filter(entry => entry.models.length > 0).map(entry => ({
    ...entry.original,
    platform: entry.original?.platform || platform,
    models: [...entry.models], billing_mode: entry.billing_mode,
    input_price: mTokToPerToken(entry.input_price),
    output_price: mTokToPerToken(entry.output_price),
    cache_write_price: mTokToPerToken(entry.cache_write_price),
    cache_write_1h_price: mTokToPerToken(entry.cache_write_1h_price),
    cache_read_price: mTokToPerToken(entry.cache_read_price),
    fast_multiplier: toNullableNumber(entry.fast_multiplier),
    flex_multiplier: toNullableNumber(entry.flex_multiplier),
    reasoning_effort_multipliers: formReasoningEffortMultipliersToAPI(entry.reasoning_effort_multipliers),
    image_input_price: mTokToPerToken(entry.image_input_price),
    image_output_price: mTokToPerToken(entry.image_output_price),
    per_request_price: toNullableNumber(entry.per_request_price),
    // Token ladders use official presets. Retain dormant legacy data on unrelated edits.
    intervals: entry.billing_mode === 'token'
      ? entry.original && (!entry.original.billing_mode || entry.original.billing_mode === 'token')
        ? entry.original.intervals || [] : []
      : formIntervalsToAPI(entry.intervals || []),
    time_pricing: null,
  }))
}
