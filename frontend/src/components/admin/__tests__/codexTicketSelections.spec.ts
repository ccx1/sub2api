import { describe, expect, it } from 'vitest'
import type { CodexTicketTierRule } from '@/api/admin/codexTicketSettings'
import { normalizePlanType } from '@/utils/planType'
import {
  readTicketTierSelections,
  ticketModelOptions,
  ticketTierOptions,
  writeTicketTierSelections
} from '../codexTicketSelections'

function matchingLengths(rules: CodexTicketTierRule[]) {
  return Object.fromEntries(rules.flatMap(rule => [rule.tier, ...rule.aliases]
    .map(name => [normalizePlanType(name), rule.target_length])).sort(([a], [b]) =>
    String(a).localeCompare(String(b))))
}

const defaults: CodexTicketTierRule[] = [
  { tier: 'team', aliases: ['business', 'chatgptteam', 'chatgptbusiness', 'business_standard'], target_length: 332 },
  { tier: 'pro', aliases: ['chatgptpro'], target_length: 292 }
]

describe('Codex ticket subscription selections', () => {
  it('shows Team and Business tags for the default 332 rule and a Pro tag for 292', () => {
    const result = readTicketTierSelections(defaults)
    expect(result.rows).toEqual([
      { id: 0, tiers: ['team', 'business'], target_length: 332 },
      { id: 1, tiers: ['pro'], target_length: 292 }
    ])
    expect(matchingLengths(writeTicketTierSelections(result.rows, result.bindings)))
      .toEqual(matchingLengths(defaults))
    expect(result.options.find(option => option.value === 'pro')?.label).toBe('Pro 20x')
    expect(result.options.find(option => option.value === 'team')?.label).toBe('Business Standard')
  })

  it('preserves unknown tiers and exact partial alias coverage without adding known aliases', () => {
    const rules = [
      { tier: 'CHATGPT_PRO', aliases: ['future-tier'], target_length: 352 },
      { tier: 'business_standard', aliases: [], target_length: 332 }
    ]
    const result = readTicketTierSelections(rules)
    expect(result.rows[0]?.tiers).toEqual(['legacy:chatgptpro', 'legacy:futuretier'])
    expect(result.rows[1]?.tiers).toEqual(['legacy:businessstandard'])
    expect(result.bindings['legacy:chatgptpro']).toEqual(['CHATGPT_PRO'])
    expect(result.bindings['legacy:businessstandard']).toEqual(['business_standard'])
    expect(result.bindings.pro).toEqual(['pro', 'chatgptpro'])
    expect(result.options.find(option => option.value === 'legacy:futuretier')?.label).toBe('future-tier')
    expect(matchingLengths(writeTicketTierSelections(result.rows, result.bindings)))
      .toEqual(matchingLengths(rules))
  })

  it('keeps partial Team and Business labels explicit and permits selecting their complete families', () => {
    const rules = [{ tier: 'team', aliases: ['business'], target_length: 332 }]
    const result = readTicketTierSelections(rules)
    expect(result.rows[0]?.tiers).toEqual(['legacy:team', 'legacy:business'])
    expect(matchingLengths(writeTicketTierSelections(result.rows, result.bindings)))
      .toEqual(matchingLengths(rules))
    const candidates = ticketTierOptions(result.rows[0]!, result.rows, result.bindings, result.options)
    expect(candidates.find(option => option.value === 'team')?.disabled).toBe(false)
    expect(candidates.find(option => option.value === 'business')?.disabled).toBe(false)
    result.rows[0]!.tiers = ['team', 'business']
    expect(matchingLengths(writeTicketTierSelections(result.rows, result.bindings))).toEqual({
      team: 332, chatgptteam: 332, businessstandard: 332, business: 332, chatgptbusiness: 332
    })
  })

  it('can explicitly replace a partial Pro tag with the complete subscription tier', () => {
    const result = readTicketTierSelections([{ tier: 'chatgptpro', aliases: [], target_length: 352 }])
    expect(result.rows[0]?.tiers).toEqual(['legacy:chatgptpro'])
    const candidates = ticketTierOptions(result.rows[0]!, result.rows, result.bindings, result.options)
    expect(candidates.find(option => option.value === 'pro')?.disabled).toBe(false)
    result.rows[0]!.tiers = ['pro']
    expect(writeTicketTierSelections(result.rows, result.bindings)).toEqual([{
      tier: 'pro', aliases: ['chatgptpro'], target_length: 352
    }])
  })

  it('accepts null aliases from YAML defaults and keeps singleton tiers recognizable', () => {
    const result = readTicketTierSelections([
      { tier: 'free', aliases: null as unknown as string[], target_length: 292 },
      { tier: 'team', aliases: undefined as unknown as string[], target_length: 332 }
    ])
    expect(result.rows.map(row => row.tiers)).toEqual([['free'], ['legacy:team']])
    expect(writeTicketTierSelections(result.rows, result.bindings)).toEqual([
      { tier: 'free', aliases: [], target_length: 292 },
      { tier: 'team', aliases: [], target_length: 332 }
    ])
  })

  it('keeps aliases with different existing lengths as independent tags', () => {
    const rules = [
      { tier: 'pro', aliases: [], target_length: 292 },
      { tier: 'chatgpt_pro', aliases: [], target_length: 332 }
    ]
    const result = readTicketTierSelections(rules)
    expect(result.rows.map(row => row.tiers)).toEqual([['legacy:pro'], ['legacy:chatgptpro']])
    expect(matchingLengths(writeTicketTierSelections(result.rows, result.bindings)))
      .toEqual(matchingLengths(rules))
    const candidates = ticketTierOptions(result.rows[0]!, result.rows, result.bindings, result.options)
    expect(candidates.find(option => option.value === 'pro')?.disabled).toBe(true)
    expect(candidates.find(option => option.value === 'legacy:pro')?.disabled).toBe(false)
    expect(candidates.find(option => option.value === 'legacy:chatgptpro')?.disabled).toBe(true)
  })

  it('prevents selecting tiers occupied by another row and preserves existing selections', () => {
    const result = readTicketTierSelections(defaults)
    const candidates = ticketTierOptions(result.rows[0]!, result.rows, result.bindings, result.options)
    expect(candidates.find(option => option.value === 'pro')?.disabled).toBe(true)
    expect(candidates.find(option => option.value === 'team')?.disabled).toBe(false)
    expect(candidates.find(option => option.value === 'business')?.disabled).toBe(false)
    expect(candidates.find(option => option.value === 'prolite')?.disabled).toBe(false)
  })

  it('supports multiple selected subscription tiers in one rule with exact default aliases', () => {
    const result = readTicketTierSelections([])
    const rows = [{ id: 0, tiers: ['team', 'prolite', 'self_serve_business_prolite'], target_length: 332 }]
    expect(writeTicketTierSelections(rows, result.bindings)).toEqual([{
      tier: 'team', aliases: ['chatgptteam', 'business_standard', 'prolite', 'self_serve_business_prolite'], target_length: 332
    }])
    expect(result.options).toHaveLength(8)
    expect(result.options.find(option => option.value === 'self_serve_business_prolite')?.label).toBe('Business Premium')
  })

  it('merges rules sharing a length and deduplicates normalized names within a row only', () => {
    const result = readTicketTierSelections([
      { tier: 'pro', aliases: ['chatgpt_pro'], target_length: 292 },
      { tier: 'plus', aliases: [], target_length: 292 }
    ])
    expect(result.rows).toHaveLength(1)
    result.bindings.pro!.push('CHATGPT-PRO')
    expect(writeTicketTierSelections(result.rows, result.bindings)).toEqual([{
      tier: 'pro', aliases: ['chatgpt_pro', 'plus'], target_length: 292
    }])
    expect(writeTicketTierSelections([
      ...result.rows, { id: 1, tiers: ['pro'], target_length: 332 }
    ], result.bindings)).toHaveLength(2)
  })

  it('chunks large rows at the backend alias limit and keeps empty rows invalid', () => {
    const result = readTicketTierSelections(Array.from({ length: 20 }, (_, index) => ({
      tier: `future_${index}`, aliases: [], target_length: 332
    })))
    const rules = writeTicketTierSelections(result.rows, result.bindings)
    expect(rules).toHaveLength(2)
    expect(rules[0]?.aliases).toHaveLength(16)
    expect(rules[1]?.aliases).toHaveLength(2)
    expect(Object.keys(matchingLengths(rules))).toHaveLength(20)
    expect(writeTicketTierSelections([{ id: 0, tiers: [], target_length: 332 }], result.bindings))
      .toEqual([{ tier: '', aliases: [], target_length: 332 }])
  })

  it('does not mutate saved rules while normalizing selections', () => {
    const source = structuredClone(defaults)
    const result = readTicketTierSelections(source)
    result.bindings.team!.push('another')
    result.rows[0]!.tiers.pop()
    expect(source).toEqual(defaults)
  })
})

describe('Codex ticket model selections', () => {
  it('uses the existing OpenAI catalog excluding image, audio, realtime, and bare aliases', () => {
    const models = ticketModelOptions([]).map(option => option.value)
    expect(models).toEqual(expect.arrayContaining(['gpt-6-astra', 'gpt-5.6-sol', 'gpt-5.6-terra', 'gpt-5.6-luna', 'gpt-5.5']))
    expect(models.some(model => /(image|audio|realtime)/i.test(model))).toBe(false)
    expect(models).not.toContain('gpt-6')
    expect(models).not.toContain('gpt-5.6')
    expect(models).not.toContain('claude-opus-4-6')
    expect(models).toContain('gpt-5.2-pro')
    expect(models).toContain('gpt-5.2-2025-12-11')
  })

  it('preserves existing unknown and previously permitted models without duplicating catalog entries', () => {
    const options = ticketModelOptions(['future-codex', 'gpt-image-1', 'gpt-6', 'gpt-6-astra', 'future-codex'])
    expect(options).toContainEqual({ value: 'future-codex', label: 'future-codex' })
    expect(options).toContainEqual({ value: 'gpt-image-1', label: 'gpt-image-1' })
    expect(options).toContainEqual({ value: 'gpt-6', label: 'gpt-6' })
    expect(options.filter(option => option.value === 'gpt-6-astra')).toHaveLength(1)
    expect(options.filter(option => option.value === 'future-codex')).toHaveLength(1)
  })
})
