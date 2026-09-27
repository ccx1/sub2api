import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createI18n } from 'vue-i18n'
import VersionBadge from '../VersionBadge.vue'
import VersionSourceCard from '../VersionSourceCard.vue'
import { checkUpdates, getRollbackVersions, performUpdate, type VersionInfo } from '@/api/admin/system'
import zh from '@/i18n/locales/zh/misc'
import en from '@/i18n/locales/en/misc'

const auth = vi.hoisted(() => ({ isAdmin: true }))
vi.mock('@/stores', async () => ({
  useAppStore: (await import('@/stores/app')).useAppStore,
  useAuthStore: () => auth
}))
vi.mock('@/api/admin/system', () => ({
  checkUpdates: vi.fn(), performUpdate: vi.fn(), restartService: vi.fn(),
  getRollbackVersions: vi.fn(), rollback: vi.fn()
}))
vi.mock('@/api/auth', () => ({ getPublicSettings: vi.fn() }))

function versionInfo(local = false, ranxi = false): VersionInfo {
  return {
    current_version: '0.2.8.17', latest_version: local ? '0.2.8.18' : '0.2.8.17',
    has_update: local, cached: false, build_type: 'release',
    release_info: { name: 'Local', body: 'Local notes', published_at: '', html_url: 'https://github.com/ccx1/sub2api/releases' },
    ranxi: {
      current_version: '2.8.14', latest_version: ranxi ? '2.8.15' : '2.8.14',
      has_update: ranxi, cached: false,
      release_info: { name: 'Ranxi', body: 'Ranxi notes', published_at: '', html_url: 'https://github.com/ranxi2001/sub2api/releases' }
    }
  }
}

function options(locale = 'zh') {
  const pinia = createPinia()
  setActivePinia(pinia)
  const compile = (messages: typeof zh.version) => ({
    version: Object.fromEntries(Object.entries(messages).map(([key, value]) => [key, () => value]))
  })
  const messages = { zh: compile(zh.version), en: compile(en.version) }
  return { global: { plugins: [pinia, createI18n({ legacy: false, locale, messages })] } }
}

beforeEach(() => { vi.clearAllMocks(); auth.isAdmin = true })

describe('VersionBadge sources', () => {
  it.each([[false, false], [true, false], [false, true], [true, true]])(
    'keeps the local badge and correct actions for local=%s ranxi=%s', async (local, ranxi) => {
      vi.mocked(checkUpdates).mockResolvedValue(versionInfo(local, ranxi))
      const wrapper = mount(VersionBadge, options())
      await flushPromises()
      expect(wrapper.get('[data-testid="version-badge"]').text()).toBe('v0.2.8.17')
      expect(wrapper.find('[data-testid="version-update-indicator"]').exists()).toBe(local || ranxi)
      const title = local && ranxi ? zh.version.bothUpdatesTitle
        : local ? zh.version.localUpdateTitle : ranxi ? zh.version.ranxiUpdateTitle : zh.version.versionDetails
      expect(wrapper.get('[data-testid="version-badge"]').attributes('title')).toBe(title)
      expect(wrapper.findAll('section')).toHaveLength(0)
      await wrapper.get('[data-testid="version-badge"]').trigger('click')
      const cards = wrapper.findAllComponents(VersionSourceCard)
      expect(cards).toHaveLength(2)
      expect(cards[0]!.text()).toContain('本地版本')
      expect(cards[0]!.text()).toContain('v0.2.8.17')
      expect(cards[1]!.text()).toContain('Ranxi 版本')
      expect(cards[1]!.text()).toContain('v2.8.14')
      expect(cards[0]!.text()).toContain('Local notes')
      expect(cards[1]!.text()).toContain('Ranxi notes')
      expect(cards[0]!.classes('border-cyan-400')).toBe(local)
      expect(cards[0]!.classes('bg-cyan-50/80')).toBe(local)
      expect(cards[0]!.get('h3').classes('text-cyan-800')).toBe(local)
      expect(cards[0]!.findAll('dd')[1]!.classes('font-semibold')).toBe(local)
      expect(cards[1]!.classes('border-orange-400')).toBe(ranxi)
      expect(cards[1]!.classes('bg-orange-50/80')).toBe(ranxi)
      expect(cards[1]!.get('h3').classes('text-orange-800')).toBe(ranxi)
      expect(cards[1]!.findAll('dd')[1]!.classes('font-semibold')).toBe(ranxi)
      expect(cards[0]!.text()).toContain(local ? '发行版可升级' : '已是最新版本')
      expect(cards[1]!.text()).toContain(ranxi ? '源码待合并' : '已是最新版本')
      expect(cards[0]!.text()).toContain('无需重新拉取源码')
      expect(cards[1]!.text()).toContain('需合并后重新打包')
      expect(wrapper.findAll('button').some(b => b.text() === '立即更新')).toBe(local)
      expect(performUpdate).not.toHaveBeenCalled()
      wrapper.unmount()
    }
  )

  it('refreshes both sources using the existing refresh button', async () => {
    vi.mocked(checkUpdates).mockResolvedValueOnce(versionInfo())
      .mockResolvedValueOnce(versionInfo(true, true)).mockResolvedValueOnce(versionInfo())
    const wrapper = mount(VersionBadge, options())
    await flushPromises()
    await wrapper.get('[data-testid="version-badge"]').trigger('click')
    await wrapper.get('button[title="刷新"]').trigger('click')
    await flushPromises()
    expect(checkUpdates).toHaveBeenLastCalledWith(true)
    expect(wrapper.text()).toContain('v0.2.8.18')
    expect(wrapper.text()).toContain('v2.8.15')
    expect(wrapper.get('[data-testid="version-badge"]').text()).toBe('v0.2.8.17')
    expect(wrapper.findAllComponents(VersionSourceCard)[0]!.classes()).toContain('border-cyan-400')
    await wrapper.get('button[title="刷新"]').trigger('click')
    await flushPromises()
    for (const card of wrapper.findAllComponents(VersionSourceCard)) {
      expect(card.classes()).toContain('border-gray-200')
      expect(card.findAll('dd')[1]!.classes()).not.toContain('font-semibold')
      expect(card.text()).toContain('已是最新版本')
    }
    expect(wrapper.find('[data-testid="version-update-indicator"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="version-badge"]').attributes('title')).toBe('版本信息')
    wrapper.unmount()
  })

  it('accepts old API responses and retains the unknown Ranxi section', async () => {
    const data = versionInfo()
    delete data.ranxi
    vi.mocked(checkUpdates).mockResolvedValue(data)
    const wrapper = mount(VersionBadge, options())
    await flushPromises()
    await wrapper.get('[data-testid="version-badge"]').trigger('click')
    const ranxi = wrapper.findAllComponents(VersionSourceCard)[1]!
    expect(ranxi.text()).toContain('暂无版本信息')
    expect(ranxi.classes()).toContain('border-gray-200')
    expect(ranxi.text()).not.toContain('源码待合并')
    wrapper.unmount()
  })

  it('shows warnings independently without claiming the failed source is current', async () => {
    const data = versionInfo(false, true)
    data.warning = 'offline'
    vi.mocked(checkUpdates).mockResolvedValue(data)
    const wrapper = mount(VersionBadge, options())
    await flushPromises()
    await wrapper.get('[data-testid="version-badge"]').trigger('click')
    const cards = wrapper.findAllComponents(VersionSourceCard)
    expect(cards[0]!.text()).toContain('版本检查失败')
    expect(cards[0]!.text()).not.toContain('已是最新版本')
    expect(cards[0]!.classes()).toContain('border-gray-200')
    expect(cards[1]!.text()).toContain('源码待合并')
    expect(wrapper.get('[data-testid="version-badge"]').attributes('title')).toBe(zh.version.ranxiUpdateTitle)
    wrapper.unmount()
  })

  it('suppresses stale update flags when both source checks fail without cached data', async () => {
    const data = versionInfo(true, true)
    data.warning = 'offline'
    data.ranxi!.warning = 'offline'
    vi.mocked(checkUpdates).mockResolvedValue(data)
    const wrapper = mount(VersionBadge, options())
    await flushPromises()
    await wrapper.get('[data-testid="version-badge"]').trigger('click')
    for (const card of wrapper.findAllComponents(VersionSourceCard)) {
      expect(card.classes()).toContain('border-gray-200')
      expect(card.findAll('dd')[1]!.text()).toBe('—')
      expect(card.text()).toContain('版本检查失败')
      expect(card.text()).not.toContain('已是最新版本')
    }
    expect(wrapper.find('[data-testid="version-update-indicator"]').exists()).toBe(false)
    expect(wrapper.findAll('button').some(button => button.text() === '立即更新')).toBe(false)
    expect(performUpdate).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it.each([false, true])('keeps cached source updates identifiable with warning=%s', async warning => {
    const data = versionInfo(true, true)
    data.cached = true
    data.ranxi!.cached = true
    if (warning) { data.warning = 'offline'; data.ranxi!.warning = 'offline' }
    vi.mocked(checkUpdates).mockResolvedValue(data)
    const wrapper = mount(VersionBadge, options())
    await flushPromises()
    await wrapper.get('[data-testid="version-badge"]').trigger('click')
    const cards = wrapper.findAllComponents(VersionSourceCard)
    expect(cards[0]!.classes()).toContain('border-cyan-400')
    expect(cards[1]!.classes()).toContain('border-orange-400')
    for (const card of cards) {
      expect(card.get('[role="status"]').text()).toBe(warning ? zh.version.cachedWarning : zh.version.cachedInfo)
      expect(card.text()).not.toContain('已是最新版本')
    }
    expect(wrapper.get('[data-testid="version-badge"]').attributes('title')).toBe(zh.version.bothUpdatesTitle)
    wrapper.unmount()
  })

  it('uses the local release for the explicit update action', async () => {
    vi.mocked(checkUpdates).mockResolvedValue(versionInfo(true, true))
    vi.mocked(performUpdate).mockResolvedValue({ message: 'updated', need_restart: true })
    const wrapper = mount(VersionBadge, options())
    await flushPromises()
    await wrapper.get('[data-testid="version-badge"]').trigger('click')
    await wrapper.findAll('button').find(button => button.text() === '立即更新')!.trigger('click')
    await flushPromises()
    expect(performUpdate).toHaveBeenCalledTimes(1)
    expect(wrapper.text()).toContain('更新完成')
    expect(wrapper.get('[data-testid="version-badge"]').text()).toBe('v0.2.8.17')
    wrapper.unmount()
  })

  it('offers only the owned release script when choosing a rollback version', async () => {
    vi.mocked(checkUpdates).mockResolvedValue(versionInfo())
    vi.mocked(getRollbackVersions).mockResolvedValue({ versions: [{
      version: '0.2.8.16', published_at: '', html_url: 'https://github.com/ccx1/sub2api/releases/tag/v0.2.8.16'
    }] })
    const wrapper = mount(VersionBadge, options())
    await flushPromises()
    await wrapper.get('[data-testid="version-badge"]').trigger('click')
    await wrapper.findAll('button').find(button => button.text() === '版本回退')!.trigger('click')
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text().includes('v0.2.8.16'))!.trigger('click')
    expect(wrapper.get('code').text()).toBe('curl -sSL https://raw.githubusercontent.com/ccx1/sub2api/v0.2.8.16/deploy/install.sh | sudo bash -s -- rollback v0.2.8.16')
    expect(wrapper.text()).toContain('当前仅发布 Linux amd64 二进制')
    expect(wrapper.findAll('button').some(button => button.text() === 'Docker')).toBe(false)
    expect(wrapper.text()).not.toContain('weishaw/sub2api')
    expect(performUpdate).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('preserves the static local version for non-admin users', () => {
    auth.isAdmin = false
    const wrapper = mount(VersionBadge, { ...options(), props: { version: '0.2.8.17' } })
    expect(wrapper.text()).toBe('v0.2.8.17')
    expect(checkUpdates).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('renders release notes as text and rejects unsafe release URLs', () => {
    const source = versionInfo()
    source.release_info = { name: '', body: '<script>alert(1)</script>', published_at: '', html_url: 'javascript:alert(1)' }
    const wrapper = mount(VersionSourceCard, { ...options('en'), props: { title: 'Local Version', source } })
    expect(wrapper.find('a').exists()).toBe(false)
    expect(wrapper.find('script').exists()).toBe(false)
    expect(wrapper.text()).toContain('<script>alert(1)</script>')
    wrapper.unmount()
  })
})
