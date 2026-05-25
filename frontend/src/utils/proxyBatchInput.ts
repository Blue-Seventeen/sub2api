import type { ProxyProtocol } from '@/types'

export interface ParsedProxyInput {
  protocol: ProxyProtocol
  host: string
  port: number
  username: string
  password: string
}

export interface ProxyBatchParseResult {
  total: number
  valid: number
  invalid: number
  duplicate: number
  proxies: ParsedProxyInput[]
}

const SUPPORTED_PROTOCOLS = new Set(['http', 'https', 'socks5', 'socks5h'])

export function parseProxyUrl(line: string): ParsedProxyInput | null {
  const trimmed = line.trim()
  if (!trimmed) return null

  let parsed: URL
  try {
    parsed = new URL(trimmed)
  } catch {
    return null
  }

  const protocol = parsed.protocol.replace(/:$/, '').toLowerCase()
  if (!SUPPORTED_PROTOCOLS.has(protocol)) return null
  if (!parsed.hostname || !parsed.port) return null

  const port = Number.parseInt(parsed.port, 10)
  if (!Number.isInteger(port) || port < 1 || port > 65535) return null

  return {
    protocol: protocol as ProxyProtocol,
    host: parsed.hostname.trim(),
    port,
    username: decodeURLPart(parsed.username),
    password: decodeURLPart(parsed.password)
  }
}

export function proxyDedupeKey(proxy: ParsedProxyInput): string {
  return [
    proxy.protocol.toLowerCase(),
    proxy.host.trim().toLowerCase(),
    String(proxy.port),
    proxy.username.trim(),
    proxy.password.trim()
  ].join('|')
}

export function parseProxyBatchInput(input: string): ProxyBatchParseResult {
  const lines = input.split('\n').filter((line) => line.trim())
  const seen = new Set<string>()
  const proxies: ParsedProxyInput[] = []
  let invalid = 0
  let duplicate = 0

  for (const line of lines) {
    const parsed = parseProxyUrl(line)
    if (!parsed) {
      invalid++
      continue
    }

    const key = proxyDedupeKey(parsed)
    if (seen.has(key)) {
      duplicate++
      continue
    }
    seen.add(key)
    proxies.push(parsed)
  }

  return {
    total: lines.length,
    valid: proxies.length,
    invalid,
    duplicate,
    proxies
  }
}

function decodeURLPart(value: string): string {
  if (!value) return ''
  try {
    return decodeURIComponent(value).trim()
  } catch {
    return value.trim()
  }
}
