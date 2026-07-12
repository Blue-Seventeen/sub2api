import fs from 'node:fs'
import path from 'node:path'
import { describe, expect, it } from 'vitest'

import en from '../locales/en/index'
import zh from '../locales/zh/index'

type Messages = Record<string, any>

const SOURCE_ROOT = path.resolve(process.cwd(), 'src')
const SOURCE_EXTENSIONS = new Set(['.vue', '.ts', '.tsx', '.js', '.jsx'])
const IGNORED_SEGMENTS = new Set(['__tests__', 'locales'])

const TRANSLATION_CALL_PATTERNS = [
  /(?:^|[^\w$])(?:t|\$t)\(\s*(['"`])([^'"`$]+)\1/g,
  /i18n\.global\.t\(\s*(['"`])([^'"`$]+)\1/g,
]

interface KeyRef {
  key: string
  refs: string[]
}

function isRuntimeSource(filePath: string): boolean {
  const relative = path.relative(SOURCE_ROOT, filePath).replaceAll('\\', '/')
  const segments = relative.split('/')

  return (
    SOURCE_EXTENSIONS.has(path.extname(filePath)) &&
    !segments.some((segment) => IGNORED_SEGMENTS.has(segment)) &&
    !/\.(spec|test)\.[cm]?[jt]sx?$/.test(path.basename(filePath))
  )
}

function walkFiles(dir: string, files: string[] = []): string[] {
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    if (['node_modules', 'dist', 'coverage'].includes(entry.name)) continue

    const fullPath = path.join(dir, entry.name)
    const relative = path.relative(SOURCE_ROOT, fullPath).replaceAll('\\', '/')
    if (relative.split('/').some((segment) => IGNORED_SEGMENTS.has(segment))) continue

    if (entry.isDirectory()) {
      walkFiles(fullPath, files)
    } else if (isRuntimeSource(fullPath)) {
      files.push(fullPath)
    }
  }

  return files
}

function nextNonWhitespace(text: string, index: number): string {
  for (let i = index; i < text.length; i += 1) {
    if (!/\s/.test(text[i])) return text[i]
  }
  return ''
}

function hasKey(messages: Messages, key: string): boolean {
  let current: unknown = messages
  for (const part of key.split('.')) {
    if (!current || typeof current !== 'object' || !(part in current)) {
      return false
    }
    current = (current as Messages)[part]
  }
  return current !== undefined
}

function collectUsedLocaleKeys(): {
  staticKeys: KeyRef[]
  dynamicPrefixes: KeyRef[]
} {
  const staticKeys = new Map<string, Set<string>>()
  const dynamicPrefixes = new Map<string, Set<string>>()

  for (const file of walkFiles(SOURCE_ROOT)) {
    const text = fs.readFileSync(file, 'utf8')
    const relative = path.relative(SOURCE_ROOT, file).replaceAll('\\', '/')

    for (const pattern of TRANSLATION_CALL_PATTERNS) {
      pattern.lastIndex = 0
      let match: RegExpExecArray | null

      while ((match = pattern.exec(text))) {
        const key = match[2]
        const next = nextNonWhitespace(text, pattern.lastIndex)
        if (!key.includes('.') || key.includes('${')) continue

        if (key.endsWith('.') || next === '+') {
          const parentKey = key.replace(/\.$/, '')
          if (!parentKey) continue
          if (!dynamicPrefixes.has(parentKey)) dynamicPrefixes.set(parentKey, new Set())
          dynamicPrefixes.get(parentKey)?.add(relative)
          continue
        }

        if (next !== ')' && next !== ',') continue

        if (!staticKeys.has(key)) staticKeys.set(key, new Set())
        staticKeys.get(key)?.add(relative)
      }
    }
  }

  const toKeyRefs = (entries: Map<string, Set<string>>): KeyRef[] =>
    [...entries.entries()]
      .map(([key, refs]) => ({ key, refs: [...refs].sort() }))
      .sort((a, b) => a.key.localeCompare(b.key))

  return {
    staticKeys: toKeyRefs(staticKeys),
    dynamicPrefixes: toKeyRefs(dynamicPrefixes),
  }
}

function missingKeys(messages: Messages, keys: KeyRef[]): string[] {
  return keys
    .filter(({ key }) => !hasKey(messages, key))
    .map(({ key, refs }) => `${key} :: ${refs.slice(0, 3).join(', ')}`)
}

describe('runtime i18n key coverage', () => {
  const { staticKeys, dynamicPrefixes } = collectUsedLocaleKeys()

  it.each([
    ['zh', zh],
    ['en', en],
  ] as const)('%s locale has every static key used by runtime source', (_locale, messages) => {
    expect(missingKeys(messages, staticKeys)).toEqual([])
  })

  it.each([
    ['zh', zh],
    ['en', en],
  ] as const)('%s locale has parent namespaces for dynamic keys', (_locale, messages) => {
    expect(missingKeys(messages, dynamicPrefixes)).toEqual([])
  })
})
