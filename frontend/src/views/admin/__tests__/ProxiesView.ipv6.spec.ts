import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { parseProxyUrl } from '@/utils/proxyBatchInput'

// Keep the page wired to the shared parser and exercise its URL contract.
const source = readFileSync(
  resolve(process.cwd(), 'src/views/admin/ProxiesView.vue'),
  'utf8'
)

describe('proxy batch URL parsing (IPv6 support)', () => {
  it('uses the shared batch parser on the proxy page', () => {
    expect(source).toContain('parseProxyBatchInput(batchInput.value)')
  })

  it.each([
    ['socks5://[2001:db8::1]:1080', true],
    ['socks5h://[2001:db8::1]:1080', true],
    ['http://[::1]:8080', true],
    ['socks5://user:pass@[2001:db8::1]:1080', true],
    ['socks5://proxy.example.com:1080', true],
    ['http://192.168.1.1:8080', true],
    ['socks5://user:pass@proxy.example.com:1080', true],
    // bare IPv6 without brackets is ambiguous with host:port — rejected
    ['socks5://2001:db8::1:1080', false],
    // unsupported schemes / malformed ports stay invalid
    ['ftp://example.com:21', false],
    ['socks5://example.com:port', false]
  ])('%s => %s', (line, expected) => {
    expect(parseProxyUrl(line) !== null).toBe(expected)
  })

  it('extracts bare IPv6 host without brackets', () => {
    expect(parseProxyUrl('socks5://user:pass@[2001:db8::1]:1080')).toEqual({
      protocol: 'socks5', username: 'user', password: 'pass', host: '2001:db8::1', port: 1080
    })
  })
})
