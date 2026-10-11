import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'
import { listPlatformIds } from '@/constants/platformCatalog'
import { CONCRETE_PLATFORM_OPTIONS } from '@/constants/platforms'

describe('Composite channel platform options', () => {
  it('includes the CN concrete providers for pricing and model mapping', () => {
    const source = readFileSync(resolve('src/views/admin/ChannelsView.vue'), 'utf8')
    // Platform catalog order is extended with the fork's compatible providers.
    expect(source).toContain('CONCRETE_PLATFORM_OPTIONS.map(option => option.value)')
    expect(source).toContain('const compositePlatforms = platformOrder')

    expect(CONCRETE_PLATFORM_OPTIONS.map(option => option.value)).toEqual(
      [...new Set([
        ...listPlatformIds(),
        'volcengine', 'ali', 'moonshot', 'perplexity', 'mistral',
        'siliconflow', 'openrouter', 'suno', 'kling', 'midjourney',
      ])]
    )
  })
})
