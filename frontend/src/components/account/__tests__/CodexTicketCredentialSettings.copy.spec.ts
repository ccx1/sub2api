import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { baseCompile } from '@intlify/message-compiler'
import CodexTicketCredentialSettings from '../CodexTicketCredentialSettings.vue'
import zhAccounts from '@/i18n/locales/zh/admin/accounts'
import enAccounts from '@/i18n/locales/en/admin/accounts'

const messages = Object.fromEntries(Object.entries({ zh: zhAccounts, en: enAccounts }).map(([locale, source]) => [locale, {
  admin: { accounts: { codexTicketCredential: Object.fromEntries(Object.entries(source.accounts.codexTicketCredential)
    .map(([key, value]) => [key, new Function(`return ${baseCompile(value, { mode: 'arrow' }).code}`)()])) } }
}]))

describe('CodexTicketCredentialSettings scheduling guidance', () => {
  it.each(['cookie', 'cookie_state'] as const)('explains the global pause control for %s in both languages', mode => {
    for (const [locale, guidance] of [
      ['zh', '是否暂停业务请求由全局“没有合格票据时暂停模型调度”开关决定'],
      ['en', 'the global “Pause model scheduling without a valid ticket” setting determines whether business requests are blocked']
    ]) {
      const wrapper = mount(CodexTicketCredentialSettings, {
        props: { modelValue: { mode } },
        global: { plugins: [createI18n({ legacy: false, locale, messages })] }
      })
      expect(wrapper.text()).toContain(guidance)
      wrapper.unmount()
    }
  })
})
