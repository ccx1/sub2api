import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { defineComponent, h, ref } from 'vue'
import { createMemoryHistory, createRouter } from 'vue-router'
import { useSettingsNavigation } from '../useSettingsNavigation'

const wrappers: VueWrapper[] = []
afterEach(() => {
  wrappers.splice(0).forEach(wrapper => wrapper.unmount())
  document.body.innerHTML = ''
  vi.restoreAllMocks()
})

async function mountNavigation(url: string, initiallyLoading = false) {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/admin/settings', component: { template: '<div />' } }],
  })
  await router.push(url)
  const loading = ref(initiallyLoading)
  const loadFailed = ref(false)
  const scrolled = vi.fn()
  const wrapper = mount(defineComponent({
    setup() {
      const navigation = useSettingsNavigation(loading, loadFailed)
      return () => h('div', [
        h('output', { 'data-test': 'tab' }, navigation.activeTab.value),
        h('button', { id: 'settings-tab-gateway' }, 'Gateway'),
        h('button', { onClick: () => navigation.selectSettingsTab('email') }, 'Email'),
        !loading.value && navigation.activeTab.value === 'features'
          ? h('h2', {
            id: 'settings-section-features-risk-control', tabindex: -1,
            ref: (element: unknown) => {
              if (element instanceof HTMLElement) element.scrollIntoView = scrolled
            },
          }, 'Risk control') : null,
      ])
    },
  }), { attachTo: document.body, global: { plugins: [router] } })
  wrappers.push(wrapper)
  await flushPromises()
  return { wrapper, router, loading, loadFailed, scrolled }
}

describe('settings navigation', () => {
  it('reveals the anchor tab then focuses after asynchronous settings loading', async () => {
    const { wrapper, loading, scrolled } = await mountNavigation(
      '/admin/settings?tab=security#settings-section-features-risk-control', true,
    )
    expect(wrapper.get('[data-test="tab"]').text()).toBe('features')
    expect(scrolled).not.toHaveBeenCalled()
    loading.value = false
    await flushPromises()
    expect(document.activeElement?.id).toBe('settings-section-features-risk-control')
    expect(scrolled).toHaveBeenCalledOnce()
    expect(scrolled).toHaveBeenCalledWith({ block: 'start' })
  })

  it('does not focus failed settings and retries when loading recovers', async () => {
    const { loading, loadFailed, scrolled } = await mountNavigation(
      '/admin/settings?tab=features#settings-section-features-risk-control', true,
    )
    loadFailed.value = true
    loading.value = false
    await flushPromises()
    expect(scrolled).not.toHaveBeenCalled()
    loadFailed.value = false
    await flushPromises()
    expect(scrolled).toHaveBeenCalledOnce()
  })

  it('clears the anchor while selecting a tab and allows selecting the result again', async () => {
    const { wrapper, router, scrolled } = await mountNavigation(
      '/admin/settings?keep=1#settings-section-features-risk-control',
    )
    await wrapper.findAll('button')[1].trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.query).toEqual({ keep: '1', tab: 'email' })
    expect(router.currentRoute.value.hash).toBe('')
    expect(wrapper.get('[data-test="tab"]').text()).toBe('email')
    await router.push('/admin/settings?tab=features#settings-section-features-risk-control')
    await flushPromises()
    expect(scrolled).toHaveBeenCalledTimes(2)
  })

  it('preserves legacy gateway links and ignores unknown locations', async () => {
    const { wrapper, router } = await mountNavigation('/admin/settings#unknown')
    expect(wrapper.get('[data-test="tab"]').text()).toBe('general')
    wrapper.get('#settings-tab-gateway').element.scrollIntoView = vi.fn()
    await router.push('/admin/settings#gateway')
    await flushPromises()
    expect(wrapper.get('[data-test="tab"]').text()).toBe('gateway')
    expect(document.activeElement?.id).toBe('settings-tab-gateway')
  })
})
