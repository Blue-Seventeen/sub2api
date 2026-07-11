import { describe, expect, it } from 'vitest'

import en from '../locales/en'
import zh from '../locales/zh'
import type { AccountPlatform, GroupPlatform } from '@/types'

const groupPlatforms: GroupPlatform[] = [
  'anthropic',
  'openai',
  'gemini',
  'antigravity',
  'grok',
  'zhipu',
  'deepseek',
  'volcengine',
  'ali',
  'moonshot',
  'perplexity',
  'mistral',
  'siliconflow',
  'xai',
  'openrouter',
  'suno',
  'kling',
  'midjourney',
]

const accountPlatforms: Array<AccountPlatform | 'claude'> = ['claude', ...groupPlatforms]

function readPath(messages: Record<string, any>, path: string): unknown {
  return path.split('.').reduce<unknown>((current, segment) => {
    if (!current || typeof current !== 'object') return undefined
    return (current as Record<string, any>)[segment]
  }, messages)
}

describe('platform locale keys', () => {
  it.each([
    ['zh', zh],
    ['en', en],
  ] as const)('%s locale resolves every group platform label', (_locale, messages) => {
    for (const platform of groupPlatforms) {
      const key = `admin.groups.platforms.${platform}`
      const value = readPath(messages, key)
      expect(value, key).toEqual(expect.any(String))
      expect(value, key).not.toBe(key)
    }
  })

  it.each([
    ['zh', zh],
    ['en', en],
  ] as const)('%s locale resolves every account platform label', (_locale, messages) => {
    for (const platform of accountPlatforms) {
      const key = `admin.accounts.platforms.${platform}`
      const value = readPath(messages, key)
      expect(value, key).toEqual(expect.any(String))
      expect(value, key).not.toBe(key)
    }
  })
})
