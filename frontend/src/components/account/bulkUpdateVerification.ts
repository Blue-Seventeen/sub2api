import type { Account } from '@/types'

type Fields = Record<string, unknown>

const object = (value: unknown): Fields =>
  value !== null && typeof value === 'object' && !Array.isArray(value) ? value as Fields : {}

const empty = (value: unknown) => value === null || value === undefined

function normalized(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(normalized).sort((a, b) => JSON.stringify(a).localeCompare(JSON.stringify(b)))
  if (value !== null && typeof value === 'object') {
    return Object.fromEntries(Object.entries(object(value)).sort(([a], [b]) => a.localeCompare(b)).map(([key, child]) => [key, normalized(child)]))
  }
  return value
}

function matches(key: string, expected: unknown, actual: unknown): boolean {
  if (key === 'proxy_id' || key === 'load_factor') {
    if (expected === 0 && empty(actual)) return true
  }
  if (expected === false && empty(actual)) return true
  if (['base_rpm', 'rpm_sticky_buffer'].includes(key) && expected === 0 && empty(actual)) return true
  if (['rpm_strategy', 'user_msg_queue_mode'].includes(key) && expected === '' && empty(actual)) return true
  if (['model_mapping', 'compact_model_mapping', 'header_overrides'].includes(key) && Object.keys(object(expected)).length === 0 && empty(actual)) return true
  if (key === 'openai_responses_mode' && empty(expected) && (empty(actual) || actual === 'auto')) return true
  if (key === 'openai_capabilities' && empty(expected)) {
    return empty(actual) || JSON.stringify(normalized(actual)) === JSON.stringify(normalized(['chat_completions', 'embeddings']))
  }
  return JSON.stringify(normalized(expected)) === JSON.stringify(normalized(actual))
}

/** Compare only fields submitted by this bulk edit; never return credential values. */
export function bulkAccountUpdateMatches(account: Account, updates: Fields): boolean {
  const row = account as unknown as Fields
  for (const [key, expected] of Object.entries(updates)) {
    if (key === 'confirm_mixed_channel_risk') continue
    if (key === 'credentials' || key === 'extra') {
      const actual = object(row[key])
      for (const [child, value] of Object.entries(object(expected))) {
        let saved = actual[child]
        if (key === 'extra' && empty(saved)) {
          if (child === 'auto_select_proxy') saved = account.proxy_auto_select_best
          else if (['base_rpm', 'rpm_strategy', 'rpm_sticky_buffer', 'user_msg_queue_mode'].includes(child)) saved = row[child]
        }
        if (!matches(child, value, saved)) return false
      }
      continue
    }
    const actual = key === 'upstream_billing_probe_enabled'
      ? account.extra?.upstream_billing_probe_enabled
      : key === 'group_ids'
        ? account.group_ids ?? account.groups?.map(group => group.id) ?? []
        : row[key]
    if (!matches(key, expected, actual)) return false
  }
  return true
}

/** Bound detail requests so a filtered bulk edit cannot fan out unbounded reads. */
export async function verifyBulkUpdatedAccounts(
  ids: number[],
  updates: Fields,
  getAccount: (id: number) => Promise<Account>
): Promise<{ verifiedIds: number[]; failedIds: number[] }> {
  const uniqueIds = [...new Set(ids)]
  const verified = new Set<number>()
  let cursor = 0
  await Promise.all(Array.from({ length: Math.min(4, uniqueIds.length) }, async () => {
    while (cursor < uniqueIds.length) {
      const id = uniqueIds[cursor++]
      try {
        const account = await getAccount(id)
        if (account.id === id && bulkAccountUpdateMatches(account, updates)) verified.add(id)
      } catch {
        // The caller reports a generic verification error; credentials stay in memory.
      }
    }
  }))
  return { verifiedIds: uniqueIds.filter(id => verified.has(id)), failedIds: uniqueIds.filter(id => !verified.has(id)) }
}
