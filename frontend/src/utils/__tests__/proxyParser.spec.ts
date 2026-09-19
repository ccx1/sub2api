import { describe, expect, it } from 'vitest'
import { parseProxyInput, parseProxyInputResult } from '../proxyParser'

describe('parseProxyInput', () => {
  it.each([
    ['host:port:user:pass', 'proxy.example.com:8001:alice:secret'],
    ['user:pass@host:port', 'alice:secret@proxy.example.com:8001'],
    ['user:pass:host:port', 'alice:secret:proxy.example.com:8001'],
  ])('accepts the documented %s form', (_format, value) => {
    expect(parseProxyInput(value, 'http')).toMatchObject({
      protocol: 'http',
      host: 'proxy.example.com',
      port: 8001,
      username: 'alice',
      password: 'secret'
    })
  })

  it.each([['http', 'http'], ['https', 'https'], ['socks5', 'socks5']] as const)(
    'uses %s for a scheme-less host',
    (protocol, expected) => {
      expect(parseProxyInput('proxy.example.com:8080', protocol)?.protocol).toBe(expected)
    }
  )

  it('accepts explicit http(s), socks5h, IPv6, and encoded credentials', () => {
    expect(parseProxyInput('https://alice:p%40ss@proxy.example.com:443', 'http')).toMatchObject({
      protocol: 'https',
      username: 'alice',
      password: 'p@ss',
      port: 443
    })
    expect(parseProxyInput('socks5h://[2001:db8::1]:1080', 'http')).toMatchObject({
      protocol: 'socks5h',
      host: '2001:db8::1',
      port: 1080
    })
  })

  it('rejects malformed or unsupported lines', () => {
    expect(parseProxyInput('ftp://proxy.example.com:21', 'http')).toBeNull()
    expect(parseProxyInput('proxy.example.com:not-a-port', 'http')).toBeNull()
    expect(parseProxyInput('proxy.example.com:8080', 'ftp' as never)).toBeNull()
    expect(parseProxyInput('http://proxy.example.com/path:8080', 'http')).toBeNull()
  })

  it('does not silently treat a numeric password as a port', () => {
    expect(parseProxyInputResult('alice:12345:proxy.example.com:8001', 'http')).toEqual({ proxy: null, error: 'ambiguous' })
    expect(parseProxyInput('alice:12345:proxy.example.com:8001', 'http', 'credentials-first')).toMatchObject({
      host: 'proxy.example.com', port: 8001, username: 'alice', password: '12345'
    })
  })

  it('supports an explicit host-first choice when both forms are plausible', () => {
    expect(parseProxyInput('proxy.example.com:8001:alice:12345', 'http')).toBeNull()
    expect(parseProxyInput('proxy.example.com:8001:alice:12345', 'http', 'host-first')).toMatchObject({
      host: 'proxy.example.com', port: 8001, username: 'alice', password: '12345'
    })
  })

  it('keeps URL and at-sign input independent of the selected colon format', () => {
    expect(parseProxyInput('alice:12345@proxy.example.com:8001', 'socks5', 'host-first')).toMatchObject({
      host: 'proxy.example.com', protocol: 'socks5', username: 'alice', password: '12345'
    })
    expect(parseProxyInput('https://alice:12345@proxy.example.com:443', 'http', 'credentials-first')).toMatchObject({
      protocol: 'https', port: 443, password: '12345'
    })
  })

  it('preserves IPv6 and colon-containing passwords in explicit formats', () => {
    expect(parseProxyInput('[2001:db8::1]:8080:alice:secret:more', 'http', 'host-first')).toMatchObject({
      host: '2001:db8::1', password: 'secret:more'
    })
    expect(parseProxyInput('alice:12345:[2001:db8::1]:8080', 'http', 'credentials-first')).toMatchObject({
      host: '2001:db8::1', port: 8080, password: '12345'
    })
  })
})
