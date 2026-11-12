import { describe, expect, it, vi } from 'vitest'
import type { Account } from '@/types'
import { bulkAccountUpdateMatches, verifyBulkUpdatedAccounts } from '../bulkUpdateVerification'

const account = (fields: Record<string, unknown> = {}) => ({ id: 1, ...fields }) as Account

describe('bulkAccountUpdateMatches', () => {
  it('compares only submitted keys, including individual merged credential and extra keys', () => {
    expect(bulkAccountUpdateMatches(account({
      priority: 4,
      credentials: { api_key: 'unrelated-fixture', model_mapping: { model: 'target' } },
      extra: { unrelated: true, auto_select_proxy: false }
    }), {
      priority: 4,
      credentials: { model_mapping: { model: 'target' } },
      extra: { auto_select_proxy: false },
      confirm_mixed_channel_risk: true
    })).toBe(true)
  })

  it.each([
    [{ priority: 0 }, { priority: 1 }],
    [{ rate_multiplier: 0 }, { rate_multiplier: 1 }],
    [{ concurrency: 8 }, {}],
    [{ extra: { auto_select_proxy: true } }, { extra: { auto_select_proxy: false } }],
    [{ credentials: { custom_error_codes: [] } }, { credentials: { custom_error_codes: [401] } }],
    [{ credentials: { model_mapping: {} } }, { credentials: { model_mapping: { old: 'mapping' } } }],
    [{ group_ids: [] }, { group_ids: [2] }],
    [{ extra: { openai_responses_mode: 'force_responses' } }, {}]
  ])('rejects a mismatched submitted value %#', (updates, saved) => {
    expect(bulkAccountUpdateMatches(account(saved), updates)).toBe(false)
  })

  it.each([
    [{ proxy_id: 0 }, { proxy_id: null }],
    [{ load_factor: 0 }, { load_factor: null }],
    [{ extra: { auto_select_proxy: false } }, { proxy_auto_select_best: false }],
    [{ extra: { openai_passthrough: false } }, {}],
    [{ extra: { base_rpm: 0, rpm_strategy: '', rpm_sticky_buffer: 0 } }, { base_rpm: 0 }],
    [{ extra: { user_msg_queue_mode: '', user_msg_queue_enabled: false } }, {}],
    [{ credentials: { model_mapping: {} } }, {}],
    [{ credentials: { compact_model_mapping: {} } }, { credentials: { compact_model_mapping: null } }],
    [{ credentials: { header_overrides: {}, header_override_enabled: false } }, {}],
    [{ extra: { openai_responses_mode: null } }, { extra: { openai_responses_mode: 'auto' } }],
    [{ credentials: { openai_capabilities: null } }, { credentials: { openai_capabilities: ['embeddings', 'chat_completions'] } }],
    [{ credentials: { openai_capabilities: null } }, {}],
    [{ group_ids: [3, 2] }, { groups: [{ id: 2 }, { id: 3 }] }],
    [{ upstream_billing_probe_enabled: true }, { extra: { upstream_billing_probe_enabled: true } }],
    [{ credentials: { header_overrides: { 'x-test': 'fixture', accept: 'application/json' } } }, { credentials: { header_overrides: { accept: 'application/json', 'x-test': 'fixture' } } }],
    [{ extra: { base_rpm: 30, rpm_strategy: 'tiered', rpm_sticky_buffer: 5, user_msg_queue_mode: 'serialize' } }, { base_rpm: 30, rpm_strategy: 'tiered', rpm_sticky_buffer: 5, user_msg_queue_mode: 'serialize' }]
  ])('accepts the persisted normalization contract %#', (updates, saved) => {
    expect(bulkAccountUpdateMatches(account(saved), updates)).toBe(true)
  })

  it('does not accept a nondefault capability reset or collapse distinct string values', () => {
    expect(bulkAccountUpdateMatches(account({ credentials: { openai_capabilities: ['embeddings'] } }), { credentials: { openai_capabilities: null } })).toBe(false)
    expect(bulkAccountUpdateMatches(account({ extra: { rpm_strategy: 'tiered' } }), { extra: { rpm_strategy: '' } })).toBe(false)
  })

  it('checks a shadow account persisted long-context value without comparing its parent billing effect', () => {
    const shadow = account({
      parent_account_id: 10,
      extra: { openai_long_context_billing_enabled: true },
      parent_account: { id: 10, extra: { openai_long_context_billing_enabled: false } }
    })
    expect(bulkAccountUpdateMatches(shadow, { extra: { openai_long_context_billing_enabled: true } })).toBe(true)
    expect(bulkAccountUpdateMatches(shadow, { extra: { openai_long_context_billing_enabled: false } })).toBe(false)
  })
})

describe('verifyBulkUpdatedAccounts', () => {
  it('rejects a failed request, wrong account identity, or mismatched persisted value', async () => {
    const getAccount = vi.fn(async (id: number) => {
      if (id === 2) throw new Error('fixture unavailable')
      if (id === 3) return account({ id: 30, priority: 4 })
      return account({ id, priority: id === 4 ? 99 : 4 })
    })
    expect(await verifyBulkUpdatedAccounts([1, 2, 3, 4, 1], { priority: 4 }, getAccount)).toEqual({ verifiedIds: [1], failedIds: [2, 3, 4] })
    expect(getAccount).toHaveBeenCalledTimes(4)
  })

  it('limits detail reads to four concurrent requests and preserves original ID order', async () => {
    let active = 0
    let maximum = 0
    const release = new Map<number, () => void>()
    const getAccount = vi.fn(async (id: number) => {
      active++
      maximum = Math.max(maximum, active)
      await new Promise<void>(resolve => { release.set(id, resolve) })
      active--
      return account({ id, priority: 4 })
    })
    const result = verifyBulkUpdatedAccounts([6, 5, 4, 3, 2, 1, 6], { priority: 4 }, getAccount)
    expect(getAccount).toHaveBeenCalledTimes(4)
    release.get(3)!()
    await vi.waitFor(() => expect(getAccount).toHaveBeenCalledTimes(5))
    release.get(4)!()
    await vi.waitFor(() => expect(getAccount).toHaveBeenCalledTimes(6))
    for (const resume of release.values()) resume()
    expect(await result).toEqual({ verifiedIds: [6, 5, 4, 3, 2, 1], failedIds: [] })
    expect(maximum).toBe(4)
  })

  it('does not issue detail reads for an empty success set', async () => {
    const getAccount = vi.fn()
    expect(await verifyBulkUpdatedAccounts([], {}, getAccount)).toEqual({ verifiedIds: [], failedIds: [] })
    expect(getAccount).not.toHaveBeenCalled()
  })
})
