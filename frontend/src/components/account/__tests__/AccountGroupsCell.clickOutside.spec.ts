import { afterEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import AccountGroupsCell from '../AccountGroupsCell.vue'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key })
  }
})

enableAutoUnmount(afterEach)

describe('AccountGroupsCell click outside', () => {
  it('lets the target receive the same click that closes the popover', async () => {
    const wrapper = mount(AccountGroupsCell, {
      props: {
        maxDisplay: 2,
        groups: [
          { id: 1, name: 'one', platform: 'openai' },
          { id: 2, name: 'two', platform: 'openai' },
          { id: 3, name: 'three', platform: 'openai' }
        ]
      },
      attachTo: document.body,
      global: {
        stubs: {
          GroupBadge: { props: ['name'], template: '<span>{{ name }}</span>' }
        }
      }
    })

    await wrapper.get('button').trigger('click')
    expect(document.body.querySelector('[data-testid="account-groups-popover"]')).not.toBeNull()
    expect(document.body.querySelector('.fixed.inset-0.z-40')).toBeNull()

    const outsideButton = document.createElement('button')
    const outsideClick = vi.fn()
    outsideButton.addEventListener('click', outsideClick)
    document.body.appendChild(outsideButton)
    outsideButton.dispatchEvent(new Event('pointerdown', { bubbles: true }))
    outsideButton.click()
    await nextTick()
    await new Promise((resolve) => setTimeout(resolve, 150))

    expect(outsideClick).toHaveBeenCalledOnce()
    expect(document.body.querySelector('[data-testid="account-groups-popover"]')).toBeNull()
    outsideButton.remove()
  })
})
