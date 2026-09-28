import { describe, expect, it } from 'vitest'

import { platformBadgeClass, platformBadgeLightClass } from '../platformColors'

describe('platformColors', () => {
  it.each([
    ['anthropic', 'orange'],
    ['openai', 'emerald'],
    ['gemini', 'sky'],
    ['grok', 'zinc'],
    ['antigravity', 'purple'],
    ['kimi', 'pink'],
    ['zhipu', 'indigo'],
    ['deepseek', 'teal'],
    ['minimax', 'rose'],
    ['opencode_go', 'amber'],
  ] as const)('uses the official %s badge palette for %s', (platform, palette) => {
    const badge = platformBadgeClass(platform)
    const lightBadge = platformBadgeLightClass(platform)

    expect(badge).toContain(`bg-${palette}-`)
    expect(badge).toContain(`text-${palette}-`)
    expect(lightBadge).toContain(`bg-${palette}-`)
    expect(lightBadge).toContain(`text-${palette}-`)

    const legacyPalette = {
      openai: 'green',
      gemini: 'blue',
      zhipu: 'emerald',
      deepseek: 'cyan',
    }[platform]
    if (legacyPalette) {
      expect(badge).not.toContain(`text-${legacyPalette}-`)
      expect(lightBadge).not.toContain(`text-${legacyPalette}-`)
    }
  })
})
