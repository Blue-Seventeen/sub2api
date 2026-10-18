import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, h, ref } from 'vue'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import PricingEntryCard from '../PricingEntryCard.vue'
import ModelTagInput from '../ModelTagInput.vue'
import channelsAPI, { type ModelDefaultPricing } from '@/api/admin/channels'
import { createDefaultTimePricingForm, type PricingFormEntry } from '../types'

vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key }),
}))

function pendingPricing() {
  let resolve!: (value: ModelDefaultPricing) => void
  const promise = new Promise<ModelDefaultPricing>(done => { resolve = done })
  return { promise, resolve }
}

const defaultPricing: ModelDefaultPricing = {
  found: true, input_price: 3e-6, output_price: 15e-6,
  cache_write_price: 4e-6, cache_read_price: 0.3e-6,
}

const wrappers: VueWrapper[] = []

function mountCard() {
  const entry = ref<PricingFormEntry>({
    models: [], billing_mode: 'token', input_price: null, output_price: null,
    cache_write_price: null, cache_read_price: null,
    image_input_price: null, image_output_price: null, per_request_price: null,
    intervals: [], time_pricing: createDefaultTimePricingForm(),
  })
  const harness = defineComponent({
    setup: () => () => h(PricingEntryCard, {
      entry: entry.value,
      onUpdate: (updated: PricingFormEntry) => { entry.value = updated },
    }),
  })
  const wrapper = mount(harness)
  wrappers.push(wrapper)
  const models = async (names: string[]) => {
    wrapper.getComponent(ModelTagInput).vm.$emit('update:models', names)
    await flushPromises()
  }
  return { wrapper, entry, models }
}

describe('PricingEntryCard pending default pricing', () => {
  let request: ReturnType<typeof pendingPricing>

  beforeEach(() => {
    request = pendingPricing()
    vi.spyOn(channelsAPI, 'getModelDefaultPricing').mockReturnValue(request.promise)
  })
  afterEach(() => {
    for (const wrapper of wrappers.splice(0)) wrapper.unmount()
    vi.restoreAllMocks()
  })

  it('fills default prices when the requested model and untouched prices are still current', async () => {
    const { entry, models } = mountCard()
    await models(['model-a'])
    expect(channelsAPI.getModelDefaultPricing).toHaveBeenCalledWith('model-a')
    expect(entry.value.input_price).toBeNull()
    request.resolve(defaultPricing)
    await flushPromises()
    expect(entry.value).toMatchObject({
      models: ['model-a'], input_price: 3, output_price: 15,
      cache_write_price: 4, cache_read_price: 0.3,
    })
  })

  it('does not undo a model added while its default pricing request is pending', async () => {
    const { entry, models } = mountCard()
    await models(['model-a'])
    await models(['model-a', 'model-b'])
    expect(channelsAPI.getModelDefaultPricing).toHaveBeenCalledTimes(1)
    request.resolve(defaultPricing)
    await flushPromises()
    expect(entry.value.models).toEqual(['model-a', 'model-b'])
    expect(entry.value.input_price).toBeNull()
  })

  it('does not restore a model removed while its default pricing request is pending', async () => {
    const { entry, models } = mountCard()
    await models(['model-a'])
    await models([])
    request.resolve(defaultPricing)
    await flushPromises()
    expect(entry.value.models).toEqual([])
    expect(entry.value.input_price).toBeNull()
  })

  it('does not overwrite a price typed while its default pricing request is pending', async () => {
    const { wrapper, entry, models } = mountCard()
    await models(['model-a'])
    const inputPrice = wrapper.getComponent(PricingEntryCard).findAll('input[type="number"]')[0]
    await inputPrice.setValue('1.25')
    expect(entry.value.input_price).toBe('1.25')
    request.resolve(defaultPricing)
    await flushPromises()
    expect(entry.value.input_price).toBe('1.25')
    expect(entry.value.output_price).toBeNull()
    expect(entry.value.models).toEqual(['model-a'])
  })
})
