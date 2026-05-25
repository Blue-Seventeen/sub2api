import { describe, expect, it } from 'vitest'
import { parseProxyBatchInput, parseProxyUrl, proxyDedupeKey } from '../proxyBatchInput'

describe('proxyBatchInput', () => {
  it('parses IPv6 host literals and encoded auth', () => {
    expect(parseProxyUrl('socks5h://user%40name:p%3Ass@[2001:db8::1]:1080')).toEqual({
      protocol: 'socks5h',
      host: '[2001:db8::1]',
      port: 1080,
      username: 'user@name',
      password: 'p:ss'
    })
  })

  it('includes protocol in dedupe key', () => {
    const http = parseProxyUrl('http://example.com:8080')!
    const socks = parseProxyUrl('socks5://example.com:8080')!

    expect(proxyDedupeKey(http)).not.toBe(proxyDedupeKey(socks))
  })

  it('dedupes exact five-tuples only', () => {
    const result = parseProxyBatchInput([
      'http://user:pass@example.com:8080',
      'socks5://user:pass@example.com:8080',
      'HTTP://user:pass@example.com:8080',
      'https://example.com:70000',
      'not-a-url'
    ].join('\n'))

    expect(result.total).toBe(5)
    expect(result.valid).toBe(2)
    expect(result.duplicate).toBe(1)
    expect(result.invalid).toBe(2)
    expect(result.proxies.map((proxy) => proxy.protocol)).toEqual(['http', 'socks5'])
  })
})
