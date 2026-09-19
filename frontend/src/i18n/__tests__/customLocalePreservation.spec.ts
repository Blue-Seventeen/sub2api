import { describe, expect, it } from 'vitest'

import en, { legacyLocale as legacyEn } from '../locales/en'
import zh, { legacyLocale as legacyZh } from '../locales/zh'
import runtimeEn from '../locales/en/index'
import runtimeZh from '../locales/zh/index'

describe.each([
  ['en', en, runtimeEn, legacyEn],
  ['zh', zh, runtimeZh, legacyZh]
] as const)('%s custom locale preservation', (locale, messages, runtime, legacy) => {
  it('loads the same translations through legacy and lazy runtime entry points', () => {
    expect(runtime, locale).toEqual(messages)
    expect(runtime.usage.serviceTierUltrafast).toBe('Ultrafast')
    expect(runtime.admin.accounts.autoResetCredit.thresholdHint).toContain('100')
  })

  it('retains the configured custom subscription window placeholder', () => {
    expect(runtime.subscriptionProgress.custom).toBe('{hours}H')
  })

  it('preserves custom peak billing, selective quota resets, and usage cost labels', () => {
    expect(runtime.admin.groups.peakRate.multiplierHint).toBe(legacy.admin.groups.peakRate.multiplierHint)
    expect(runtime.admin.groups.rateMultiplierHint).toBe(legacy.admin.groups.rateMultiplierHint)
    expect(runtime.admin.subscriptions.adjustHint).toBe(legacy.admin.subscriptions.adjustHint)
    expect(runtime.admin.subscriptions.resetQuotaConfirm).toBe(legacy.admin.subscriptions.resetQuotaConfirm)
    expect(runtime.admin.subscriptions.guide).toEqual(legacy.admin.subscriptions.guide)
    for (const key of ['billingModeImage', 'billingModeDuration', 'billingModeCharacter'] as const) {
      expect(runtime.admin.usage[key]).toBe(legacy.admin.usage[key])
    }
    expect(runtime.nav.promotion).toBe(legacy.nav.promotion)
    expect(runtime.nav.promotionAdmin).toBe(legacy.nav.promotionAdmin)
  })

  it('keeps the custom forced-standard Flex policy copy', () => {
    expect(runtime.admin.settings.openaiFastPolicy.empty).toBe(legacy.admin.settings.openaiFastPolicy.empty)
    expect(runtime.admin.settings.openaiFastPolicy.tierFlex).toBe(legacy.admin.settings.openaiFastPolicy.tierFlex)
  })
})

it('does not replace Chinese account messages with generated English fallback labels', () => {
  expect(runtimeZh.admin.accounts.form).toMatchObject(legacyZh.admin.accounts.form)
  expect(runtimeZh.admin.accounts.filters).toMatchObject(legacyZh.admin.accounts.filters)
  expect(runtimeZh.admin.accounts.accountCreatedSuccess).toBe(legacyZh.admin.accounts.accountCreatedSuccess)
  expect(runtimeZh.admin.groups.accountsUnit).toBe(legacyZh.admin.groups.accountsUnit)
})

it('retains English account confirmation placeholders and translated form labels', () => {
  expect(runtimeEn.admin.accounts.deleteConfirmMessage).toContain('{name}')
  expect(runtimeEn.admin.accounts.form.credentialsLabel).toBe('Credentials')
  expect(runtimeEn.admin.accounts.filters.allPlatforms).toBe('All Platforms')
  expect(runtimeEn.admin.accounts.types.api_key).toBe('API Key')
  expect(runtimeEn.admin.groups.modelRouting.claudeMaxSimulation).toEqual(
    runtimeEn.admin.groups.claudeMaxSimulation
  )
})
