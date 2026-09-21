import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import OpenAIQuotaResetCell from '../OpenAIQuotaResetCell.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import type { Account } from '@/types'
import {
  refreshOpenAIQuota, resetOpenAIQuota,
  type OpenAIQuotaRefreshResult, type OpenAIQuotaResetResult
} from '@/api/admin/accounts'

vi.mock('@/api/admin/accounts', () => ({ refreshOpenAIQuota: vi.fn(), resetOpenAIQuota: vi.fn() }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

type QuotaAccount = Pick<Account, 'id' | 'platform' | 'type' | 'parent_account_id' | 'extra'>
const expiry = '2099-07-03T04:05:06Z'
const snapshot = (count: number) => ({
  available_count: count,
  credits: Array.from({ length: count }, () => ({ expires_at: expiry }))
})
const account = (id = 1, count?: number): QuotaAccount => ({
  id, platform: 'openai', type: 'oauth', parent_account_id: null,
  extra: count === undefined ? {} : { codex_reset_credit_snapshot: snapshot(count) }
})
const quota = (count: number): OpenAIQuotaRefreshResult => ({
  fetched_at: 1770000000, cache_persisted: true, rate_limit_reset_credits: snapshot(count)
})
const resetResult = (partial = false): OpenAIQuotaResetResult => ({
  code: 'success', windows_reset: 1, cache_refreshed: !partial, account_state_recovered: true,
  quota: partial ? null : quota(0), warning_code: partial ? 'reset_credit_cache_refresh_failed' : undefined
})
function deferred<T>() {
  let resolve!: (result: T) => void
  let reject!: (reason: Error) => void
  const promise = new Promise<T>((resolveValue, rejectValue) => { resolve = resolveValue; reject = rejectValue })
  return { promise, resolve, reject }
}
const render = (props: InstanceType<typeof OpenAIQuotaResetCell>['$props']) =>
  mount(OpenAIQuotaResetCell, { props, global: { stubs: { ConfirmDialog: true } } })
const buttons = (wrapper: ReturnType<typeof render>) => wrapper.findAll('button')
const confirm = (wrapper: ReturnType<typeof render>) => wrapper.findComponent(ConfirmDialog).vm.$emit('confirm')
const countText = (wrapper: ReturnType<typeof render>) => buttons(wrapper)[0].text()

beforeEach(() => vi.clearAllMocks())

describe('OpenAIQuotaResetCell 共享账号接入', () => {
  it('使用注入的查询接口并在请求期间上报忙碌状态', async () => {
    const pending = deferred<OpenAIQuotaRefreshResult>()
    const queryQuota = vi.fn(() => pending.promise)
    const wrapper = render({ account: account(), queryQuota })
    await buttons(wrapper)[0].trigger('click')
    expect(queryQuota).toHaveBeenCalledWith(1)
    expect(refreshOpenAIQuota).not.toHaveBeenCalled()
    expect(wrapper.emitted('busy-change')).toEqual([[true]])
    expect(buttons(wrapper)[0].attributes('disabled')).toBeDefined()
    pending.resolve(quota(2))
    await flushPromises()
    expect(countText(wrapper)).toContain('count2')
    expect(wrapper.emitted('busy-change')).toEqual([[true], [false]])
    wrapper.unmount()
  })

  it('首次操作前接收异步到达的快照', async () => {
    const wrapper = render({ account: account() })
    expect(buttons(wrapper)[1].attributes('disabled')).toBeDefined()
    await wrapper.setProps({ account: account(1, 2) })
    expect(countText(wrapper)).toContain('count2')
    expect(buttons(wrapper)[1].attributes('disabled')).toBeUndefined()
    expect(refreshOpenAIQuota).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('主动查询开始后旧快照不能覆盖实时查询结果', async () => {
    const pending = deferred<OpenAIQuotaRefreshResult>()
    const wrapper = render({ account: account(1, 1), queryQuota: () => pending.promise })
    await buttons(wrapper)[0].trigger('click')
    await wrapper.setProps({ account: account(1, 3) })
    expect(countText(wrapper)).toContain('count1')
    pending.resolve(quota(0))
    await flushPromises()
    await wrapper.setProps({ account: account(1, 3) })
    expect(countText(wrapper)).toContain('count0')
    expect(buttons(wrapper)[1].attributes('disabled')).toBeDefined()
    wrapper.unmount()
  })

  it('disabled 同时阻止查询、打开确认和已打开确认的提交', async () => {
    const queryQuota = vi.fn()
    const resetQuota = vi.fn()
    const wrapper = render({ account: account(1, 1), disabled: true, queryQuota, resetQuota })
    await buttons(wrapper)[0].trigger('click')
    await buttons(wrapper)[1].trigger('click')
    confirm(wrapper)
    await flushPromises()
    expect(queryQuota).not.toHaveBeenCalled()
    expect(resetQuota).not.toHaveBeenCalled()
    await wrapper.setProps({ disabled: false })
    await buttons(wrapper)[1].trigger('click')
    expect(wrapper.findComponent(ConfirmDialog).props('show')).toBe(true)
    await wrapper.setProps({ disabled: true })
    expect(wrapper.findComponent(ConfirmDialog).props('show')).toBe(false)
    confirm(wrapper)
    expect(resetQuota).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('查询进行中禁止通过确认事件启动重置', async () => {
    const pending = deferred<OpenAIQuotaRefreshResult>()
    const resetQuota = vi.fn()
    const wrapper = render({ account: account(1, 1), queryQuota: () => pending.promise, resetQuota })
    await buttons(wrapper)[1].trigger('click')
    await buttons(wrapper)[0].trigger('click')
    confirm(wrapper)
    expect(resetQuota).not.toHaveBeenCalled()
    pending.resolve(quota(1))
    await flushPromises()
    wrapper.unmount()
  })

  it.each([false, true])('重置成功（部分成功=%s）在解除忙碌后通知父层刷新', async (partial) => {
    const pending = deferred<OpenAIQuotaResetResult>()
    const resetQuota = vi.fn(() => pending.promise)
    const queryQuota = vi.fn()
    const events: string[] = []
    const wrapper = render({
      account: account(1, 1), resetQuota, queryQuota,
      'onBusy-change': (busy) => events.push(`busy:${busy}`),
      'onQuota-reset': () => events.push('reset')
    })
    await buttons(wrapper)[1].trigger('click')
    confirm(wrapper)
    confirm(wrapper)
    await buttons(wrapper)[0].trigger('click')
    expect(resetQuota).toHaveBeenCalledTimes(1)
    expect(resetQuota).toHaveBeenCalledWith(1)
    expect(resetOpenAIQuota).not.toHaveBeenCalled()
    expect(queryQuota).not.toHaveBeenCalled()
    const result = resetResult(partial)
    pending.resolve(result)
    await flushPromises()
    expect(events).toEqual(['busy:true', 'busy:false', 'reset'])
    expect(wrapper.emitted('quota-reset')).toEqual([[result]])
    await wrapper.setProps({ account: account(1, 1) })
    expect(countText(wrapper)).not.toContain('count1')
    expect(buttons(wrapper)[1].attributes('disabled')).toBeDefined()
    wrapper.unmount()
  })

  it('旧账号查询晚返回不会覆盖新账号或释放新请求的忙碌状态', async () => {
    const oldRequest = deferred<OpenAIQuotaRefreshResult>()
    const newRequest = deferred<OpenAIQuotaRefreshResult>()
    const queryQuota = vi.fn((id: number) => id === 1 ? oldRequest.promise : newRequest.promise)
    const wrapper = render({ account: account(), queryQuota })
    await buttons(wrapper)[0].trigger('click')
    await wrapper.setProps({ account: account(2, 3) })
    await buttons(wrapper)[0].trigger('click')
    oldRequest.resolve(quota(9))
    await flushPromises()
    expect(countText(wrapper)).toContain('count3')
    expect(buttons(wrapper)[0].attributes('disabled')).toBeDefined()
    newRequest.resolve(quota(2))
    await flushPromises()
    expect(countText(wrapper)).toContain('count2')
    wrapper.unmount()
  })

  it('旧账号请求失败不会在新账号显示错误', async () => {
    const pending = deferred<OpenAIQuotaRefreshResult>()
    const wrapper = render({ account: account(), queryQuota: () => pending.promise })
    await buttons(wrapper)[0].trigger('click')
    await wrapper.setProps({ account: account(2, 2) })
    pending.reject(new Error('old account error'))
    await flushPromises()
    expect(wrapper.text()).not.toContain('old account error')
    expect(countText(wrapper)).toContain('count2')
    wrapper.unmount()
  })

  it('旧账号重置晚返回不会通知父层或清除新账号快照', async () => {
    const pending = deferred<OpenAIQuotaResetResult>()
    const wrapper = render({ account: account(1, 1), resetQuota: () => pending.promise })
    await buttons(wrapper)[1].trigger('click')
    confirm(wrapper)
    await wrapper.setProps({ account: account(2, 2) })
    pending.resolve(resetResult())
    await flushPromises()
    expect(countText(wrapper)).toContain('count2')
    expect(wrapper.emitted('quota-reset')).toBeUndefined()
    expect(wrapper.emitted('account-updated')).toBeUndefined()
    wrapper.unmount()
  })

  it('卸载时解除忙碌，晚返回不再发出成功事件', async () => {
    const pending = deferred<OpenAIQuotaResetResult>()
    const onBusyChange = vi.fn()
    const onQuotaReset = vi.fn()
    const wrapper = render({
      account: account(1, 1), resetQuota: () => pending.promise,
      'onBusy-change': onBusyChange, 'onQuota-reset': onQuotaReset
    })
    await buttons(wrapper)[1].trigger('click')
    confirm(wrapper)
    await flushPromises()
    wrapper.unmount()
    expect(onBusyChange.mock.calls).toEqual([[true], [false]])
    pending.resolve(resetResult())
    await flushPromises()
    expect(onQuotaReset).not.toHaveBeenCalled()
    expect(onBusyChange.mock.calls).toEqual([[true], [false]])
  })

  it('重置失败解除忙碌但不发成功事件，允许用户重新查询', async () => {
    const wrapper = render({ account: account(1, 1), resetQuota: vi.fn().mockRejectedValue(new Error('reset failed')) })
    await buttons(wrapper)[1].trigger('click')
    confirm(wrapper)
    await flushPromises()
    expect(wrapper.text()).toContain('reset failed')
    expect(wrapper.emitted('quota-reset')).toBeUndefined()
    expect(wrapper.emitted('busy-change')).toEqual([[true], [false]])
    expect(buttons(wrapper)[0].attributes('disabled')).toBeUndefined()
    wrapper.unmount()
  })
})
