import type { CodexTicketTierRule } from '@/api/admin/codexTicketSettings'
import { subscriptionTierOptions } from '@/components/sharedPool/settlementPolicy'
import { getModelsByPlatform } from '@/composables/useModelWhitelist'
import { normalizePlanType } from '@/utils/planType'

export interface TicketTierRow {
  id: number
  tiers: string[]
  target_length: number
}

interface TicketOption {
  value: string
  label: string
  disabled?: boolean
}

const knownTierAliases: Record<string, string[]> = {
  free: ['free'],
  plus: ['plus'],
  pro: ['pro', 'chatgptpro'],
  prolite: ['prolite'],
  team: ['team', 'chatgptteam', 'business_standard'],
  self_serve_business_prolite: ['self_serve_business_prolite'],
  business: ['business', 'chatgptbusiness'],
  enterprise: ['enterprise']
}

const tierByAlias = new Map(Object.entries(knownTierAliases).flatMap(([tier, names]) =>
  names.map(name => [normalizePlanType(name), tier] as const)
))

function uniqueTierNames(names: string[]): string[] {
  const seen = new Set<string>()
  return names.filter(name => {
    const key = normalizePlanType(name)
    if (seen.has(key)) return false
    seen.add(key)
    return true
  })
}

export function readTicketTierSelections(rules: CodexTicketTierRule[]): {
  rows: TicketTierRow[]
  bindings: Record<string, string[]>
  options: TicketOption[]
} {
  const entries = rules.flatMap(rule => [rule.tier, ...(rule.aliases ?? [])].map(name => ({
    name, length: rule.target_length, canonical: tierByAlias.get(normalizePlanType(name))
  })))
  const lengthsByTier = new Map<string, Set<number>>()
  const namesByTier = new Map<string, Set<string>>()
  for (const entry of entries) {
    if (!entry.canonical) continue
    const lengths = lengthsByTier.get(entry.canonical) ?? new Set<number>()
    lengths.add(entry.length)
    lengthsByTier.set(entry.canonical, lengths)
    const names = namesByTier.get(entry.canonical) ?? new Set<string>()
    names.add(normalizePlanType(entry.name))
    namesByTier.set(entry.canonical, names)
  }
  const completeTiers = new Set([...lengthsByTier].filter(([tier, lengths]) => lengths.size === 1
    && knownTierAliases[tier]!.every(name => namesByTier.get(tier)?.has(normalizePlanType(name))))
    .map(([tier]) => tier))
  const options: TicketOption[] = subscriptionTierOptions.openai.map(option => ({ ...option }))
  const bindings: Record<string, string[]> = Object.fromEntries(
    Object.entries(knownTierAliases).map(([tier, names]) => [tier, [...names]])
  )
  const savedBindings = new Map<string, string[]>()
  const byLength = new Map<number, TicketTierRow>()
  for (const entry of entries) {
    const key = entry.canonical && completeTiers.has(entry.canonical)
      ? entry.canonical : `legacy:${normalizePlanType(entry.name)}`
    const row = byLength.get(entry.length) ?? { id: byLength.size, tiers: [], target_length: entry.length }
    if (!row.tiers.includes(key)) row.tiers.push(key)
    byLength.set(entry.length, row)
    savedBindings.set(key, [...(savedBindings.get(key) ?? []), entry.name])
    if (!options.some(option => option.value === key)) options.push({ value: key, label: entry.name })
  }
  // 不完整的别名集合保持为旧标签；只有用户明确选择完整档次才扩大匹配范围。
  for (const [tier, names] of savedBindings) bindings[tier] = uniqueTierNames(names)
  return { rows: [...byLength.values()], bindings, options }
}

export function writeTicketTierSelections(
  rows: TicketTierRow[], bindings: Record<string, string[]>
): CodexTicketTierRule[] {
  return rows.flatMap(row => {
    const names = uniqueTierNames(row.tiers.flatMap(tier => bindings[tier] ?? []))
    if (names.length === 0) return [{ tier: '', aliases: [], target_length: row.target_length }]
    const rules: CodexTicketTierRule[] = []
    // 后端每条最多 16 个别名；仅在需要时拆分，避免多选产生额外规则膨胀。
    for (let offset = 0; offset < names.length; offset += 17) {
      const batch = names.slice(offset, offset + 17)
      rules.push({ tier: batch[0]!, aliases: batch.slice(1), target_length: row.target_length })
    }
    return rules
  })
}

export function ticketTierOptions(
  row: TicketTierRow,
  rows: TicketTierRow[],
  bindings: Record<string, string[]>,
  options: TicketOption[]
): TicketOption[] {
  const occupied = new Set(rows.filter(other => other.id !== row.id)
    .flatMap(other => other.tiers.flatMap(tier => bindings[tier] ?? []))
    .map(normalizePlanType))
  return options.map(option => ({
    ...option,
    disabled: !row.tiers.includes(option.value)
      && (bindings[option.value] ?? []).some(name => occupied.has(normalizePlanType(name)))
  }))
}

export function ticketModelOptions(savedModels: string[]): TicketOption[] {
  const selectable = getModelsByPlatform('openai').filter(model =>
    !/(image|audio|realtime)/i.test(model) && !['gpt-6', 'gpt-5.6'].includes(model)
  )
  // 目录更新和存量自定义模型均不可在打开页面或保存其它设置时被静默删除。
  return [...new Set([...selectable, ...savedModels])].map(model => ({ value: model, label: model }))
}
