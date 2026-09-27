import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import CodexModelQualityView from '../CodexModelQualityView.vue'

const mocks = vi.hoisted(() => ({ load: vi.fn() }))
vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key })
}))
vi.mock('@/api/admin/codexTicketSettings', () => ({ getCodexTicketSettings: mocks.load }))
vi.mock('@/components/layout/AppLayout.vue', () => ({ default: { template: '<div><slot /></div>' } }))

const QualityStub = { name: 'CodexModelQuality', props: ['models'], template: '<section data-testid="quality" />' }
let wrapper: ReturnType<typeof mount<typeof CodexModelQualityView>> | undefined
const render = () => {
  wrapper = mount(CodexModelQualityView, {
    global: { stubs: { RouterLink: { template: '<a><slot /></a>' }, CodexModelQuality: QualityStub } }
  })
  return wrapper
}
beforeEach(() => vi.resetAllMocks())
afterEach(() => wrapper?.unmount())

describe('CodexModelQualityView', () => {
  it('passes the saved ticket models to the quality policy', async () => {
    mocks.load.mockResolvedValue({ models: ['gpt-6-astra', 'gpt-5.6-sol'] })
    const page = render()
    await flushPromises()
    expect(page.getComponent(QualityStub).props('models')).toEqual(['gpt-6-astra', 'gpt-5.6-sol'])
  })

  it('shows a retry when ticket models fail to load', async () => {
    mocks.load.mockRejectedValueOnce(new Error('boom')).mockResolvedValueOnce({ models: ['gpt-test'] })
    const page = render()
    await flushPromises()
    expect(page.find('[data-testid="quality"]').exists()).toBe(false)
    await page.get('[data-testid="load-retry"]').trigger('click')
    await flushPromises()
    expect(page.getComponent(QualityStub).props('models')).toEqual(['gpt-test'])
  })
})
