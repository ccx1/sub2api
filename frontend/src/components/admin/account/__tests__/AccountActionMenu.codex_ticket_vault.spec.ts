import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import AccountActionMenu from '../AccountActionMenu.vue'
import type { Account } from '@/types'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

const account = {
  id: 9,
  name: 'vault-account',
  platform: 'openai',
  type: 'oauth',
  parent_account_id: null,
  status: 'active',
  schedulable: true
} as unknown as Account
const anchorRect = new DOMRect(100, 100, 24, 24)
const label = 'admin.accounts.codexTicketVault.menu'
const findButton = () => Array.from(document.body.querySelectorAll('button')).find(b => b.textContent?.includes(label))

describe('AccountActionMenu — 票库入口', () => {
  it('仅在 showCodexTicketVault 时显示，点击触发 codex-ticket-vault 并关闭菜单', async () => {
    const hidden = mount(AccountActionMenu, { props: { show: true, account, anchorRect }, attachTo: document.body })
    expect(findButton()).toBeUndefined()
    hidden.unmount()

    const wrapper = mount(AccountActionMenu, { props: { show: true, account, anchorRect, showCodexTicketVault: true }, attachTo: document.body })
    const button = findButton()
    expect(button).toBeDefined()
    button!.click()
    await wrapper.vm.$nextTick()
    expect(wrapper.emitted('codex-ticket-vault')![0][0]).toMatchObject({ id: 9 })
    expect(wrapper.emitted('close')).toBeTruthy()
    wrapper.unmount()
  })
})
