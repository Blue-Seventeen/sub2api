import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import ModelPlazaContent from '../ModelPlazaContent.vue'
import type { ModelPlazaResponse } from '@/api/modelPlaza'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key
    })
  }
})

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({
    isAuthenticated: true
  })
}))

function tokenModel(name: string, platform = 'openai') {
  return {
    name,
    platform,
    pricing: {
      billing_mode: 'token',
      input_price: 3e-6,
      output_price: 1.5e-5,
      cache_write_price: null,
      cache_read_price: null,
      image_input_price: null,
      image_output_price: null,
      per_request_price: null,
      intervals: []
    },
    official_pricing: null
  }
}

function group(
  id: number,
  name: string,
  platform: string,
  rateMultiplier: number,
  models: Array<ReturnType<typeof tokenModel>>,
  overrides: Partial<ModelPlazaResponse['groups'][number]> = {}
): ModelPlazaResponse['groups'][number] {
  return {
    id,
    name,
    description: '',
    platform,
    subscription_type: 'standard',
    rate_multiplier: rateMultiplier,
    user_rate_multiplier: undefined,
    peak_rate_enabled: false,
    peak_start: '',
    peak_end: '',
    peak_rate_multiplier: 1,
    is_exclusive: false,
    image_rate_independent: false,
    image_rate_multiplier: 1,
    models,
    ...overrides
  }
}

describe('ModelPlazaContent', () => {
  it('all platform view aggregates by platform and keeps the lowest rate model', () => {
    const response: ModelPlazaResponse = {
      description: '',
      groups: [
        group(1, 'openai-high', 'openai', 0.3, [tokenModel('gpt-5.5'), tokenModel('gpt-4')]),
        group(2, 'openai-low', 'openai', 0.1, [tokenModel('gpt-5.5')], {
          subscription_type: 'subscription',
          peak_rate_enabled: true,
          peak_start: '13:00',
          peak_end: '14:00',
          peak_rate_multiplier: 2,
          is_exclusive: true
        }),
        group(3, 'anthropic-main', 'anthropic', 0.2, [tokenModel('claude-sonnet')])
      ]
    }

    const wrapper = mount(ModelPlazaContent, {
      props: {
        response,
        loading: false,
        error: false,
        embedded: true
      },
      global: {
        stubs: {
          Icon: true,
          PlazaFilterBar: true,
          PlazaGroupSection: {
            props: ['group'],
            template: `
              <section class="stub-group">
                <h2 class="group-name">{{ group.name }}</h2>
                <div class="group-rate">{{ group.rate_multiplier }}</div>
                <div class="group-models">
                  <span v-for="m in group.models" :key="m.platform + ':' + m.name" class="model-item">
                    {{ m.platform }}:{{ m.name }}@{{ m.rate_multiplier ?? group.rate_multiplier }}:{{ m.source_group_name }}:{{ m.source_group_subscription_type }}:{{ m.source_group_is_exclusive }}:{{ m.source_group_peak_rate_enabled }}
                  </span>
                </div>
              </section>
            `
          }
        }
      }
    })

    const sections = wrapper.findAll('.stub-group')
    expect(sections).toHaveLength(2)
    expect(wrapper.text()).toContain('openai')
    expect(wrapper.text()).toContain('anthropic')
    expect(wrapper.text()).toContain('openai:gpt-5.5@0.1:openai-low:subscription:true:true')
    expect(wrapper.text()).toContain('openai:gpt-4@0.3:openai-high:standard:false:false')
    expect(wrapper.text()).toContain('openai:claude-sonnet@0.2:anthropic-main:standard:false:false')
  })

  it('all platform view preserves same model names from different concrete platforms', () => {
    const response: ModelPlazaResponse = {
      description: '',
      groups: [
        group(1, 'openai-low', 'openai', 0.1, [tokenModel('shared-model', 'openai')]),
        group(2, 'anthropic-low', 'anthropic', 0.2, [tokenModel('shared-model', 'anthropic')])
      ]
    }

    const wrapper = mount(ModelPlazaContent, {
      props: {
        response,
        loading: false,
        error: false,
        embedded: true
      },
      global: {
        stubs: {
          Icon: true,
          PlazaFilterBar: true,
          PlazaGroupSection: {
            props: ['group'],
            template: `
              <section class="stub-group">
                <h2 class="group-name">{{ group.name }}</h2>
                <span v-for="m in group.models" :key="m.platform + ':' + m.name" class="model-item">
                  {{ m.platform }}:{{ m.name }}
                </span>
              </section>
            `
          }
        }
      }
    })

    expect(wrapper.text()).toContain('openai:shared-model')
    expect(wrapper.text()).toContain('anthropic:shared-model')
  })
})
