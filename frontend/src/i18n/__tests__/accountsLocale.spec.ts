import { describe, expect, it } from 'vitest'

import zh from '../locales/zh'

const suspiciousMojibakePattern = /[�鍦鍒鍙鍚鍛鍜鍝鍞鍟鍠鎴鏂鏌鐪鐧璐骞彿绫闂淇娴鍔瀛鏁鎿缁璇杩鎺]/

function collectStrings(value: unknown): string[] {
  if (typeof value === 'string') return [value]
  if (Array.isArray(value)) return value.flatMap((item) => collectStrings(item))
  if (value && typeof value === 'object') {
    return Object.values(value as Record<string, unknown>).flatMap((item) => collectStrings(item))
  }
  return []
}

describe('account locale strings', () => {
  it('keeps admin.accounts zh copy readable and free of common mojibake markers', () => {
    const accountStrings = collectStrings(zh.admin.accounts)
    const suspicious = accountStrings.filter((text) => suspiciousMojibakePattern.test(text))

    expect(suspicious).toEqual([])
  })

  it('contains cleaned zh copy for key account-management labels', () => {
    expect(zh.admin.accounts.title).toBe('账号管理')
    expect(zh.admin.accounts.description).toBe('管理 AI 平台账号和凭证')
    expect(zh.admin.accounts.dataExportIncludeProxies).toBe('包含关联代理（导出账号所绑定的代理）')
    expect(zh.admin.accounts.schedulableHint).toBe('开启后账号会参与 API 请求调度')
    expect(zh.admin.accounts.allPrivacyModes).toBe('全部隐私状态')
    expect(zh.admin.accounts.tokenRefreshed).toBe('令牌刷新成功')
    expect(zh.admin.accounts.rateLimitCleared).toBe('限流状态已清除')
    expect(zh.admin.accounts.addModel).toBe('添加')
    expect(zh.admin.accounts.autoOpsDialog.title).toBe('自动运维')
  })
})
