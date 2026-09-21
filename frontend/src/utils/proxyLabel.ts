import type { Proxy } from '@/types'
import { randomProxyAddress } from '@/utils/randomProxy'

export function proxyOptionLabel(proxy: Pick<Proxy, 'account_count' | 'name' | 'protocol' | 'host' | 'port' | 'group_name'>): string {
  const count = Number.isInteger(proxy.account_count) && proxy.account_count! >= 0 ? proxy.account_count : '—'
  const group = proxy.group_name ? ` [${proxy.group_name}]` : ''
  return `(${count}) ${proxy.name}${group} ${proxy.protocol}://${randomProxyAddress(proxy)}`
}
