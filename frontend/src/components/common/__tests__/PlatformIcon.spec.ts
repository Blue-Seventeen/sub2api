import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'

import PlatformIcon from '../PlatformIcon.vue'

describe('PlatformIcon', () => {
  it('renders the official marks for upstream-added platforms', () => {
    const officialMarks = [
      ['kimi', 'M21.765'],
      ['zhipu', 'M11.991'],
      ['deepseek', 'M23.748'],
      ['minimax', 'M16.278'],
      ['opencode_go', 'M16 6'],
    ] as const

    for (const [platform, pathPrefix] of officialMarks) {
      const wrapper = mount(PlatformIcon, {
        props: { platform },
      })

      expect(wrapper.find('svg').exists()).toBe(true)
      expect(wrapper.find('path').attributes('d')).toMatch(pathPrefix)
    }
  })

  it.each([
    ['anthropic', 'text-orange-500'],
    ['openai', 'text-emerald-500'],
    ['gemini', 'text-sky-500'],
    ['grok', 'text-zinc-800'],
    ['antigravity', 'text-purple-500'],
    ['kimi', 'text-pink-500'],
    ['zhipu', 'text-indigo-500'],
    ['deepseek', 'text-teal-500'],
    ['minimax', 'text-rose-500'],
    ['opencode_go', 'text-amber-500'],
  ] as const)('uses the official platform color for %s', (platform, colorClass) => {
    const wrapper = mount(PlatformIcon, {
      props: { platform },
    })

    expect(wrapper.find('svg').classes()).toContain(colorClass)
  })
})
