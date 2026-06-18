(function (root, factory) {
  const api = factory(root)
  root.CodexSub2apiConverter = api
  if (typeof window !== 'undefined') window.CodexSub2apiConverter = api
  if (typeof globalThis !== 'undefined') globalThis.CodexSub2apiConverter = api
  if (typeof self !== 'undefined') self.CodexSub2apiConverter = api
  if (typeof module !== 'undefined' && module.exports) {
    module.exports = api
  }
})(typeof globalThis !== 'undefined' ? globalThis : window, function (root) {
  const OPENAI_CLIENT_ID = 'app_EMoamEEZ73f0CkXaXp7hrann'
  const REQUIRED_TOKEN_KEYS = ['access_token', 'refresh_token', 'id_token']

  const nowRfc3339 = () => new Date().toISOString().replace(/\.\d{3}Z$/, 'Z')
  const stringValue = value => typeof value === 'string' ? value.trim() : ''

  function firstString(data, paths) {
    for (const path of paths) {
      let current = data
      for (const key of path) {
        current = current && typeof current === 'object' ? current[key] : undefined
      }
      if (stringValue(current)) return current.trim()
    }
    return ''
  }

  function decodeBase64Url(value) {
    const base64 = value.replace(/-/g, '+').replace(/_/g, '/')
    const padded = base64.padEnd(base64.length + ((4 - base64.length % 4) % 4), '=')
    if (typeof atob === 'function') {
      const binary = atob(padded)
      return Uint8Array.from(binary, char => char.charCodeAt(0))
    }
    if (typeof Buffer !== 'undefined') {
      return Uint8Array.from(Buffer.from(padded, 'base64'))
    }
    throw new Error('base64 decoder unavailable')
  }

  function decodeUtf8(bytes) {
    if (typeof TextDecoder !== 'undefined') {
      return new TextDecoder().decode(bytes)
    }
    if (typeof Buffer !== 'undefined') {
      return Buffer.from(bytes).toString('utf8')
    }
    throw new Error('utf8 decoder unavailable')
  }

  function decodeJwtPayload(token) {
    const parts = token.split('.')
    if (parts.length !== 3) return {}
    try {
      const decoded = JSON.parse(decodeUtf8(decodeBase64Url(parts[1])))
      return decoded && typeof decoded === 'object' ? decoded : {}
    } catch {
      return {}
    }
  }

  function setIfMissing(target, key, value) {
    if (stringValue(value) && !stringValue(target[key])) target[key] = value.trim()
  }

  function firstOrgId(organizations, defaultOnly) {
    if (!Array.isArray(organizations)) return ''
    for (const item of organizations) {
      if (!item || typeof item !== 'object') continue
      if (defaultOnly && item.is_default !== true) continue
      if (stringValue(item.id)) return item.id.trim()
    }
    return ''
  }

  function enrichFromJwt(credentials, token) {
    const claims = decodeJwtPayload(token)
    if (!Object.keys(claims).length) return
    setIfMissing(credentials, 'email', stringValue(claims.email))
    if (!credentials.expires_at && Number(claims.exp) > 0) {
      credentials.expires_at = new Date(Number(claims.exp) * 1000).toISOString()
    }
    const auth = claims['https://api.openai.com/auth']
    if (!auth || typeof auth !== 'object') {
      setIfMissing(credentials, 'chatgpt_user_id', stringValue(claims.sub))
      return
    }
    setIfMissing(credentials, 'chatgpt_account_id', stringValue(auth.chatgpt_account_id))
    setIfMissing(credentials, 'chatgpt_user_id', stringValue(auth.chatgpt_user_id || auth.user_id || claims.sub))
    setIfMissing(credentials, 'plan_type', stringValue(auth.chatgpt_plan_type))
    setIfMissing(credentials, 'organization_id', stringValue(auth.poid))
    setIfMissing(credentials, 'organization_id', firstOrgId(auth.organizations, true))
    setIfMissing(credentials, 'organization_id', firstOrgId(auth.organizations, false))
  }

  async function sha256(text) {
    if (root.crypto?.subtle && typeof TextEncoder !== 'undefined') {
      const bytes = new TextEncoder().encode(text)
      const hash = await root.crypto.subtle.digest('SHA-256', bytes)
      return [...new Uint8Array(hash)].map(byte => byte.toString(16).padStart(2, '0')).join('')
    }
    if (typeof require === 'function') {
      return require('node:crypto').createHash('sha256').update(text, 'utf8').digest('hex')
    }
    return ''
  }

  function buildBaseName(fileName, data, credentials) {
    return firstString(data, [['name'], ['user', 'name']]) ||
      stringValue(credentials.email) ||
      stringValue(credentials.chatgpt_account_id) ||
      fileName.replace(/\.json$/i, '')
  }

  async function buildAccount(fileName, data, settings) {
    const credentials = {
      access_token: firstString(data, [['tokens', 'access_token'], ['tokens', 'accessToken'], ['access_token'], ['accessToken'], ['token']]),
      refresh_token: firstString(data, [['tokens', 'refresh_token'], ['tokens', 'refreshToken'], ['refresh_token'], ['refreshToken']]),
      id_token: firstString(data, [['tokens', 'id_token'], ['tokens', 'idToken'], ['id_token'], ['idToken']]),
      client_id: OPENAI_CLIENT_ID,
    }
    const missing = REQUIRED_TOKEN_KEYS.filter(key => !credentials[key])
    if (missing.length) throw new Error(`缺少字段: ${missing.join(', ')}`)
    setIfMissing(credentials, 'email', firstString(data, [['email'], ['user', 'email']]))
    enrichFromJwt(credentials, credentials.id_token)
    enrichFromJwt(credentials, credentials.access_token)

    const account = {
      name: buildBaseName(fileName, data, credentials),
      platform: 'openai',
      type: 'oauth',
      credentials,
      extra: await buildExtra(fileName, data, credentials.access_token),
      concurrency: settings.concurrency,
      priority: settings.priority,
    }
    if (settings.notes?.trim()) account.notes = settings.notes.trim()
    return account
  }

  async function buildExtra(fileName, data, accessToken) {
    const extra = {
      import_source: 'codex_json_ui',
      import_file: fileName,
      imported_at: nowRfc3339(),
    }
    const fingerprint = await sha256(accessToken)
    if (fingerprint) extra.access_token_sha256 = fingerprint
    for (const key of ['token_source', 'saved_at']) {
      if (stringValue(data[key])) extra[key] = data[key].trim()
    }
    if (stringValue(data.type)) extra.source_type = data.type.trim()
    return extra
  }

  function applySettings(account, settings) {
    const output = typeof structuredClone === 'function'
      ? structuredClone(account)
      : JSON.parse(JSON.stringify(account))
    output.name = settings.namePrefix ? `${settings.namePrefix}${output.name}` : output.name
    output.concurrency = settings.concurrency
    output.priority = settings.priority
    if (settings.notes?.trim()) output.notes = settings.notes.trim()
    else delete output.notes
    return output
  }

  function dedupeAccounts(accounts, mode) {
    if (mode === 'none') return accounts
    const seen = new Set()
    return accounts.filter(account => {
      const key = mode === 'file'
        ? stringValue(account.extra?.import_file).toLowerCase()
        : stringValue(account.credentials?.email).toLowerCase()
      if (!key || seen.has(key)) return !key
      seen.add(key)
      return true
    })
  }

  function buildBundle(accounts) {
    return {
      type: 'sub2api-data',
      version: 1,
      exported_at: nowRfc3339(),
      proxies: [],
      accounts,
    }
  }

  return {
    applySettings,
    buildAccount,
    buildBundle,
    dedupeAccounts,
  }
})
