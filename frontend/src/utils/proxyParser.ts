import type { ProxyProtocol } from '@/types'

export interface ParsedProxyInput {
  protocol: ProxyProtocol
  host: string
  port: number
  username: string
  password: string
}

export type ProxyInputFormat = 'auto' | 'host-first' | 'credentials-first'
export interface ProxyParseResult {
  proxy: ParsedProxyInput | null
  error: 'invalid' | 'ambiguous' | null
}

const SUPPORTED_PROTOCOLS = new Set<ProxyProtocol>(['http', 'https', 'socks5', 'socks5h'])

const decodeProxyPart = (value: string): string => {
  try {
    return decodeURIComponent(value)
  } catch {
    return value
  }
}

const parsePort = (value: string): number | null => {
  if (!/^\d+$/.test(value)) return null
  const port = Number(value)
  return port >= 1 && port <= 65535 ? port : null
}

const normalizeHost = (value: string): string | null => {
  const host = value.trim()
  if (!host || /[\s/@?#]/.test(host)) return null

  if (host.startsWith('[') || host.endsWith(']')) {
    if (!/^\[[0-9a-f:.]+\]$/i.test(host)) return null
    return host.slice(1, -1)
  }

  if (host.includes(':')) return null
  return host
}

const parseHostPort = (value: string): { host: string; port: number } | null => {
  const trimmed = value.trim()
  let rawHost = ''
  let rawPort = ''

  if (trimmed.startsWith('[')) {
    const closingBracket = trimmed.indexOf(']')
    if (closingBracket < 0 || trimmed[closingBracket + 1] !== ':') return null
    rawHost = trimmed.slice(0, closingBracket + 1)
    rawPort = trimmed.slice(closingBracket + 2)
  } else {
    const lastColon = trimmed.lastIndexOf(':')
    if (lastColon <= 0) return null
    rawHost = trimmed.slice(0, lastColon)
    rawPort = trimmed.slice(lastColon + 1)
  }

  const host = normalizeHost(rawHost)
  const port = parsePort(rawPort)
  return host && port !== null ? { host, port } : null
}

const parseCredentials = (value: string): { username: string; password: string } | null => {
  const separator = value.indexOf(':')
  const rawUsername = (separator < 0 ? value : value.slice(0, separator)).trim()
  const rawPassword = separator < 0 ? '' : value.slice(separator + 1).trim()
  if (!rawUsername && !rawPassword) return null
  if (rawUsername.includes('@') || rawPassword.includes('@')) return null
  return {
    username: decodeProxyPart(rawUsername),
    password: decodeProxyPart(rawPassword)
  }
}

const buildParsedProxy = (
  protocol: ProxyProtocol,
  hostPort: { host: string; port: number },
  credentials: { username: string; password: string } = { username: '', password: '' }
): ParsedProxyInput => ({
  protocol,
  host: hostPort.host,
  port: hostPort.port,
  username: credentials.username,
  password: credentials.password
})

const parseExplicitScheme = (line: string): ParsedProxyInput | null => {
  const schemeMatch = line.match(/^([a-z][a-z0-9+.-]*):\/\/(.*)$/i)
  if (!schemeMatch) return null

  const protocolName = schemeMatch[1].toLowerCase() as ProxyProtocol
  if (!SUPPORTED_PROTOCOLS.has(protocolName)) return null

  const authority = schemeMatch[2]
  const at = authority.lastIndexOf('@')
  const hostPortText = at >= 0 ? authority.slice(at + 1) : authority
  const hostPort = parseHostPort(hostPortText)
  if (!hostPort) return null

  if (at < 0) return buildParsedProxy(protocolName, hostPort)
  const credentials = parseCredentials(authority.slice(0, at))
  return credentials ? buildParsedProxy(protocolName, hostPort, credentials) : null
}

const parseAtFormat = (line: string, protocol: ProxyProtocol): ParsedProxyInput | null => {
  const at = line.lastIndexOf('@')
  if (at < 0) return null
  const credentials = parseCredentials(line.slice(0, at))
  const hostPort = parseHostPort(line.slice(at + 1))
  return credentials && hostPort ? buildParsedProxy(protocol, hostPort, credentials) : null
}

const parseHostPortCredentialsFormat = (
  line: string,
  protocol: ProxyProtocol
): ParsedProxyInput | null => {
  const match = line.match(/^(\[[0-9a-f:.]+\]|[^:@\s]+):(\d+):([^:@]*):(.*)$/i)
  if (!match) return null
  const port = parsePort(match[2])
  const host = normalizeHost(match[1])
  if (
    !host ||
    port === null ||
    (!match[3].trim() && !match[4].trim()) ||
    match[3].includes('@') ||
    match[4].includes('@')
  ) {
    return null
  }
  return buildParsedProxy(protocol, { host, port }, {
    username: decodeProxyPart(match[3].trim()),
    password: decodeProxyPart(match[4].trim())
  })
}

const parseCredentialsHostPortFormat = (
  line: string,
  protocol: ProxyProtocol
): ParsedProxyInput | null => {
  const match = line.match(/^([^:@]*):([^:@]*):(\[[0-9a-f:.]+\]|[^:@\s]+):(\d+)$/i)
  if (!match || (!match[1].trim() && !match[2].trim())) return null
  const port = parsePort(match[4])
  const host = normalizeHost(match[3])
  if (!host || port === null) return null
  return buildParsedProxy(protocol, { host, port }, {
    username: decodeProxyPart(match[1].trim()),
    password: decodeProxyPart(match[2].trim())
  })
}

export const parseProxyInputResult = (line: string, defaultProtocol: ProxyProtocol, format: ProxyInputFormat = 'auto'): ProxyParseResult => {
  const invalid: ProxyParseResult = { proxy: null, error: 'invalid' }
  const trimmed = line.trim()
  if (!trimmed || !SUPPORTED_PROTOCOLS.has(defaultProtocol)) return invalid

  const explicit = parseExplicitScheme(trimmed)
  if (explicit) return { proxy: explicit, error: null }
  if (/^[a-z][a-z0-9+.-]*:\/\//i.test(trimmed)) return invalid
  const atFormat = parseAtFormat(trimmed, defaultProtocol)
  if (atFormat) return { proxy: atFormat, error: null }
  const hostFirst = parseHostPortCredentialsFormat(trimmed, defaultProtocol)
  const credentialsFirst = parseCredentialsHostPortFormat(trimmed, defaultProtocol)
  if (format === 'auto' && hostFirst && credentialsFirst && JSON.stringify(hostFirst) !== JSON.stringify(credentialsFirst)) {
    return { proxy: null, error: 'ambiguous' }
  }
  const colonFormat = format === 'host-first' ? hostFirst : format === 'credentials-first' ? credentialsFirst : hostFirst || credentialsFirst
  const hostPort = parseHostPort(trimmed)
  const proxy = colonFormat || (hostPort ? buildParsedProxy(defaultProtocol, hostPort) : null)
  return proxy ? { proxy, error: null } : invalid
}

export const parseProxyInput = (line: string, defaultProtocol: ProxyProtocol, format: ProxyInputFormat = 'auto'): ParsedProxyInput | null =>
  parseProxyInputResult(line, defaultProtocol, format).proxy
