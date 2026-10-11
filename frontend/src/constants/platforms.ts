import { reactive, watchSyncEffect } from 'vue'
import type { AccountPlatform, GroupPlatform } from '@/types'
import { listPlatforms } from './platformCatalog'

// type 别名（而非 interface）以便赋给 Select 组件的 Record<string, unknown>[] 选项类型。
export type PlatformOption<T extends string = string> = {
  value: T
  label: string
}

/**
 * Concrete upstream platforms supported by accounts and request routing.
 * Derived from the platform catalog (backend platform list, see platformCatalog.ts),
 * so newly registered providers show up in every selector without frontend
 * changes. Reactive so that tests replacing the catalog see the update.
 */
export const CUSTOM_PLATFORM_OPTIONS: readonly PlatformOption<AccountPlatform>[] = [
  { value: 'volcengine', label: 'VolcEngine/Doubao' },
  { value: 'ali', label: 'Qwen/Ali' },
  { value: 'moonshot', label: 'Kimi/Moonshot' },
  { value: 'perplexity', label: 'Perplexity' },
  { value: 'mistral', label: 'Mistral' },
  { value: 'siliconflow', label: 'SiliconFlow' },
  { value: 'openrouter', label: 'OpenRouter' },
  { value: 'suno', label: 'Suno' },
  { value: 'kling', label: 'Kling' },
  { value: 'midjourney', label: 'Midjourney' }
]

export const CUSTOM_PLATFORM_IDS = CUSTOM_PLATFORM_OPTIONS.map(({ value }) => value)

export const CONCRETE_PLATFORM_OPTIONS: PlatformOption<AccountPlatform>[] = reactive([])

/** Platforms that can own a group. */
export const GROUP_PLATFORM_OPTIONS: PlatformOption<GroupPlatform>[] = reactive([])

watchSyncEffect(() => {
  const options = new Map<string, PlatformOption<AccountPlatform>>()
  for (const spec of listPlatforms()) {
    options.set(spec.id, { value: spec.id, label: spec.display_name })
  }
  for (const option of CUSTOM_PLATFORM_OPTIONS) {
    if (!options.has(option.value)) options.set(option.value, option)
  }
  const concrete = [...options.values()]
  CONCRETE_PLATFORM_OPTIONS.splice(0, CONCRETE_PLATFORM_OPTIONS.length, ...concrete)
  GROUP_PLATFORM_OPTIONS.splice(0, GROUP_PLATFORM_OPTIONS.length, ...concrete, {
    value: 'composite',
    label: 'Composite'
  })
})
