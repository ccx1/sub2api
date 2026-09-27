import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useAppStore } from '../app'
import { checkUpdates, type VersionInfo } from '@/api/admin/system'

vi.mock('@/api/admin/system', () => ({ checkUpdates: vi.fn() }))
vi.mock('@/api/auth', () => ({ getPublicSettings: vi.fn() }))
vi.mock('@/i18n', () => ({ i18n: { global: { t: (key: string) => key } } }))

function payload(): VersionInfo {
  return { current_version: '0.2.8.17', latest_version: '0.2.8', has_update: false,
    cached: false, build_type: 'release', ranxi: { current_version: '2.8.14',
      latest_version: '2.8.15', has_update: true, cached: false } }
}

beforeEach(() => { setActivePinia(createPinia()); vi.clearAllMocks() })

describe('version source state', () => {
  it('preserves both sources on a frontend cache hit', async () => {
    vi.mocked(checkUpdates).mockResolvedValue(payload())
    const app = useAppStore()
    await app.fetchVersion()
    const cached = await app.fetchVersion()
    expect(checkUpdates).toHaveBeenCalledTimes(1)
    expect(cached?.current_version).toBe('0.2.8.17')
    expect(cached?.ranxi?.has_update).toBe(true)
    expect(app.hasUpdate).toBe(false)
  })

  it('clears independent state for legacy responses and cache invalidation', async () => {
    vi.mocked(checkUpdates).mockResolvedValue(payload())
    const app = useAppStore()
    await app.fetchVersion()
    const legacy = payload()
    delete legacy.ranxi
    vi.mocked(checkUpdates).mockResolvedValue(legacy)
    await app.fetchVersion(true)
    expect(app.ranxiVersionInfo).toBeNull()
    app.clearVersionCache()
    expect(app.versionLoaded).toBe(false)
    expect(app.hasUpdate).toBe(false)
    expect(app.versionWarning).toBe('')
  })

  it.each(['local', 'ranxi'])('retries partial %s failures without force', async (source) => {
    const data = payload()
    if (source === 'local') data.warning = 'offline'
    else data.ranxi!.warning = 'offline'
    vi.mocked(checkUpdates).mockResolvedValueOnce(data).mockResolvedValueOnce(payload())
    const app = useAppStore()
    await app.fetchVersion()
    expect(app.versionLoaded).toBe(false)
    await app.fetchVersion()
    expect(checkUpdates).toHaveBeenCalledTimes(2)
    expect(app.versionLoaded).toBe(true)
  })

  it('keeps the displayed local version and exposes network failure', async () => {
    const log = vi.spyOn(console, 'error').mockImplementation(() => {})
    vi.mocked(checkUpdates).mockResolvedValueOnce(payload()).mockRejectedValueOnce(new Error('offline'))
    const app = useAppStore()
    await app.fetchVersion()
    await app.fetchVersion(true)
    expect(app.currentVersion).toBe('0.2.8.17')
    expect(app.versionLoaded).toBe(false)
    expect(app.versionWarning).not.toBe('')
    expect(app.versionCached).toBe(true)
    expect(app.ranxiVersionInfo?.warning).not.toBe('')
    log.mockRestore()
  })
})
