import { describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import MonitorAdvancedRequestConfig from '../MonitorAdvancedRequestConfig.vue'
import {
  API_MODE_CHAT_COMPLETIONS,
  API_MODE_RESPONSES,
  PROVIDER_OPENAI,
} from '@/constants/channelMonitor'
import type { APIMode } from '@/api/admin/channelMonitor'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key,
    }),
  }
})

function mountConfig(apiMode: APIMode) {
  return mount(MonitorAdvancedRequestConfig, {
    props: {
      provider: PROVIDER_OPENAI,
      apiMode,
      extraHeaders: {},
      bodyOverrideMode: 'replace',
      bodyOverride: null,
    },
  })
}

function findModeButton(wrapper: ReturnType<typeof mountConfig>, labelPart: string) {
  const button = wrapper.findAll('button').find((item) => item.text().includes(labelPart))
  if (!button) throw new Error(`button not found: ${labelPart}`)
  return button
}

describe('MonitorAdvancedRequestConfig', () => {
  it('seeds a valid OpenAI Chat Completions replace body when body is empty', async () => {
    const wrapper = mountConfig(API_MODE_CHAT_COMPLETIONS)
    await flushPromises()

    const body = (wrapper.find('textarea').element as HTMLTextAreaElement).value
    expect(body).toContain('"messages"')

    const emitted = wrapper.emitted('update:bodyOverride') ?? []
    expect(emitted[0][0]).toMatchObject({
      messages: [{ role: 'user', content: 'Reply with exactly: ok' }],
    })
  })

  it('seeds a valid OpenAI Responses replace body when body is empty', async () => {
    const wrapper = mountConfig(API_MODE_RESPONSES)
    await flushPromises()

    const body = (wrapper.find('textarea').element as HTMLTextAreaElement).value
    expect(body).toContain('"instructions"')
    expect(body).toContain('"input"')

    const emitted = wrapper.emitted('update:bodyOverride') ?? []
    expect(emitted[0][0]).toMatchObject({
      instructions: 'You are a health check endpoint. Reply briefly.',
      input: 'Reply with exactly: ok',
    })
  })

  it('emits a valid OpenAI Responses body when switching from off to replace', async () => {
    const wrapper = mount(MonitorAdvancedRequestConfig, {
      props: {
        provider: PROVIDER_OPENAI,
        apiMode: API_MODE_RESPONSES,
        extraHeaders: {},
        bodyOverrideMode: 'off',
        bodyOverride: null,
      },
    })
    await flushPromises()

    await findModeButton(wrapper, 'bodyModeReplace').trigger('click')

    expect(wrapper.emitted('update:bodyOverrideMode')?.[0][0]).toBe('replace')
    const emitted = wrapper.emitted('update:bodyOverride') ?? []
    expect(emitted[0][0]).toMatchObject({
      instructions: 'You are a health check endpoint. Reply briefly.',
      input: 'Reply with exactly: ok',
    })
  })
})
