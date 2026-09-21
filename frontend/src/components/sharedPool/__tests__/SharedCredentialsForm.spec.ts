import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import SharedCredentialsForm from '../SharedCredentialsForm.vue'
import OAuthAuthorizationFlow from '@/components/account/OAuthAuthorizationFlow.vue'
import type { SharedPlatform } from '@/api/sharedPool'

const { oauthStart, oauthFinish, copyToClipboard, getCapabilities } = vi.hoisted(() => ({
  oauthStart: vi.fn(), oauthFinish: vi.fn(), copyToClipboard: vi.fn(), getCapabilities: vi.fn()
}))
vi.mock('@/api/sharedPool', () => ({ sharedPoolAPI: { oauthStart, oauthFinish } }))
vi.mock('@/api/admin', () => ({ adminAPI: { grok: { getCapabilities } } }))
vi.mock('@/composables/useClipboard', async () => {
  const { ref } = await import('vue')
  return { useClipboard: () => ({ copied: ref(false), copyToClipboard }) }
})
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

const authorization = { auth_url: 'https://auth.example.com/authorize?state=generated-state', session_id: 'fixture-session' }
function render(platform: SharedPlatform = 'openai') {
  return mount(SharedCredentialsForm, {
    props: { platform, type: 'oauth', editing: false, proxyUrl: '' },
    global: { stubs: { Icon: true }, mocks: { $t: (key: string) => key } }
  })
}
async function start(wrapper: ReturnType<typeof render>) {
  const button = wrapper.findAll('button').find(button => button.text().endsWith('generateAuthUrl'))!
  await button.trigger('click')
  await flushPromises()
}
function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>(done => { resolve = done })
  return { promise, resolve }
}

describe('shared account browser authorization', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    oauthStart.mockResolvedValue(authorization)
    oauthFinish.mockResolvedValue({ credentials: { access_token: 'fixture-token' } })
  })

  it('reuses account management authorization URL display, copy, and regenerate controls', async () => {
    const wrapper = render()
    const flow = wrapper.getComponent(OAuthAuthorizationFlow)
    expect(flow.props('showCookieOption')).toBe(false)
    expect(flow.props('showCodexSessionImportOption')).toBe(false)
    await start(wrapper)
    expect((wrapper.get('input[readonly]').element as HTMLInputElement).value).toBe(authorization.auth_url)
    await wrapper.get('button[title="Copy URL"]').trigger('click')
    expect(copyToClipboard).toHaveBeenCalledWith(authorization.auth_url, expect.any(String))
    await wrapper.findAll('button').find(button => button.text().includes('oauth.regenerate'))!.trigger('click')
    await flushPromises()
    expect(oauthStart).toHaveBeenCalledTimes(2)
    expect(getCapabilities).not.toHaveBeenCalled()
  })

  it.each(['openai', 'gemini', 'antigravity'] as SharedPlatform[])('submits the callback code and state for %s', async platform => {
    const wrapper = render(platform)
    await start(wrapper)
    await wrapper.get('textarea').setValue('http://localhost:1455/callback?code=fixture-code&state=callback-state')
    await wrapper.get('[data-action="finish-authorization"]').trigger('click')
    await flushPromises()
    expect(oauthFinish).toHaveBeenCalledWith(platform, { session_id: 'fixture-session', code: 'fixture-code', state: 'callback-state' })
    expect(wrapper.emitted('change')?.at(-1)).toEqual([{ access_token: 'fixture-token' }])
    expect(wrapper.get('[role="status"]').text()).toBe('sharedPool.authorized')
  })

  it('supports a plain authorization code with the state from the generated URL', async () => {
    const wrapper = render()
    await start(wrapper)
    await wrapper.get('textarea').setValue('plain-fixture-code')
    await wrapper.get('[data-action="finish-authorization"]').trigger('click')
    await flushPromises()
    expect(oauthFinish).toHaveBeenCalledWith('openai', expect.objectContaining({ code: 'plain-fixture-code', state: 'generated-state' }))
  })

  it('discards a stale start response after switching platform', async () => {
    const pending = deferred<typeof authorization>()
    oauthStart.mockReturnValueOnce(pending.promise)
    const wrapper = render()
    await start(wrapper)
    expect(wrapper.emitted('busy')?.at(-1)).toEqual([true])
    await wrapper.setProps({ platform: 'anthropic' })
    pending.resolve(authorization)
    await flushPromises()
    expect(wrapper.find('input[readonly]').exists()).toBe(false)
    expect(wrapper.emitted('busy')?.at(-1)).toEqual([false])
    expect(wrapper.emitted('change')?.at(-1)).toEqual([undefined])
  })

  it('discards credentials returned for a proxy that was changed during authorization', async () => {
    const pending = deferred<{ credentials: Record<string, unknown> }>()
    oauthFinish.mockReturnValueOnce(pending.promise)
    const wrapper = render()
    await start(wrapper)
    await wrapper.get('textarea').setValue('plain-fixture-code')
    await wrapper.get('[data-action="finish-authorization"]').trigger('click')
    await wrapper.setProps({ proxyUrl: 'http://new-proxy.example:8080' })
    pending.resolve({ credentials: { access_token: 'stale-token' } })
    await flushPromises()
    expect(wrapper.emitted('change')?.at(-1)).toEqual([undefined])
    expect(wrapper.find('[role="status"]').exists()).toBe(false)
    expect(wrapper.emitted('busy')?.at(-1)).toEqual([false])
  })

  it('requires a new link after exchange failure and clears old callback state on regeneration', async () => {
    const wrapper = render()
    await start(wrapper)
    await wrapper.get('textarea').setValue('http://localhost/callback?code=old-code&state=old-state')
    oauthFinish.mockRejectedValueOnce(new Error('fixture authorization failure'))
    await wrapper.get('[data-action="finish-authorization"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-action="finish-authorization"]').attributes('disabled')).toBeDefined()
    expect(wrapper.text()).toContain('fixture authorization failure')
    await wrapper.findAll('button').find(button => button.text().includes('oauth.regenerate'))!.trigger('click')
    await flushPromises()
    await wrapper.get('textarea').setValue('fresh-code')
    await wrapper.get('[data-action="finish-authorization"]').trigger('click')
    await flushPromises()
    expect(oauthFinish).toHaveBeenLastCalledWith('openai', expect.objectContaining({ code: 'fresh-code', state: 'generated-state' }))
  })

  it('keeps JSON credentials and clears them when switching authorization method', async () => {
    const wrapper = render()
    await wrapper.get('input[value="json"]').setValue()
    await wrapper.get('#shared-credentials').setValue('{"access_token":"json-fixture"}')
    expect(wrapper.emitted('change')?.at(-1)).toEqual([{ access_token: 'json-fixture' }])
    await wrapper.get('#shared-credentials').setValue('{broken')
    expect(wrapper.emitted('valid')?.at(-1)).toEqual([false])
    await wrapper.get('input[value="browser"]').setValue()
    expect(wrapper.emitted('change')?.at(-1)).toEqual([undefined])
    expect(wrapper.emitted('valid')?.at(-1)).toEqual([true])
  })
})
