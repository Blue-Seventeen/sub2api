import { describe, expect, it } from 'vitest'

import zh from '../locales/zh'

const suspiciousFragments = [
  '鍦?GitHub',
  '鏌ョ湅鏂囨。',
  '鐧诲綍',
  '绔嬪嵆寮',
  '璁㈤槄',
  '骞冲彴',
  '鍒锋柊',
  '杩炴帴鍒',
  '鏁版嵁搴',
  '璇锋眰',
  '閰嶇疆',
  '淇濆瓨',
  '鍒犻櫎',
  '缁撴灉',
  '鏃堕棿',
  '绠＄悊'
]

function collectStrings(value: unknown): string[] {
  if (typeof value === 'string') return [value]
  if (Array.isArray(value)) return value.flatMap((item) => collectStrings(item))
  if (value && typeof value === 'object') {
    return Object.values(value as Record<string, unknown>).flatMap((item) => collectStrings(item))
  }
  return []
}

describe('zh locale mojibake guard', () => {
  it('does not contain common mojibake fragments across the full locale tree', () => {
    const allStrings = collectStrings(zh)
    const offenders = allStrings.filter(
      (text) => text.includes('�') || suspiciousFragments.some((fragment) => text.includes(fragment))
    )

    expect(offenders).toEqual([])
  })

  it('keeps a few representative zh strings readable after cleanup', () => {
    expect(zh.home.viewOnGithub).toBe('在 GitHub 上查看')
    expect(zh.keyUsage.title).toBe('API Key 用量查询')
    expect(zh.keyUsage.tokenStats).toBe('令牌统计')
    expect(zh.keyUsage.privacyNote).toBe('您的密钥仅在浏览器本地处理，不会被存储')
    expect(zh.setup.title).toBe('Sub2API 安装向导')
    expect(zh.setup.description).toBe('配置您的 Sub2API 实例')
  })
})
