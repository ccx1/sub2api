import type { AdminGroup } from '@/types'
import type { SharedPlatform, SharedSettlementPolicy } from '@/api/sharedPool'

export const sharedPlatforms: SharedPlatform[] = ['openai', 'anthropic', 'gemini', 'antigravity']
export const sharedPlatformNames: Record<SharedPlatform, string> = { openai: 'OpenAI', anthropic: 'Claude', gemini: 'Gemini', antigravity: 'Antigravity' }
export const subscriptionTierOptions = {
  openai: [
    { value: 'free', label: 'Free' }, { value: 'plus', label: 'Plus' },
    { value: 'pro', label: 'Pro 20x' }, { value: 'prolite', label: 'Pro 5x' },
    { value: 'team', label: 'Business Standard' }, { value: 'self_serve_business_prolite', label: 'Business Premium' },
    { value: 'business', label: 'Business' }, { value: 'enterprise', label: 'Enterprise' }
  ],
  anthropic: [],
  gemini: [
    { value: 'google_one_free', label: 'Google One Free' }, { value: 'google_ai_pro', label: 'Google AI Pro' },
    { value: 'google_ai_ultra', label: 'Google AI Ultra' }, { value: 'gcp_standard', label: 'GCP Standard' },
    { value: 'gcp_enterprise', label: 'GCP Enterprise' }, { value: 'aistudio_free', label: 'AI Studio Free' },
    { value: 'aistudio_paid', label: 'AI Studio Paid' }
  ],
  antigravity: [{ value: 'free', label: 'Free' }, { value: 'pro', label: 'Pro' }, { value: 'ultra', label: 'Ultra' }]
} satisfies Record<SharedPlatform, { value: string; label: string }[]>

export const validSettlementMultiplier = (value: unknown): value is number => typeof value === 'number' && Number.isFinite(value) && value >= 0 && value <= 100
export const validSharedPriority = (value: unknown): value is number => typeof value === 'number' && Number.isInteger(value) && value >= 0 && value <= 100
export function hasSettlementPolicy(config: SharedSettlementPolicy) {
  return validSettlementMultiplier(config.settlement_multiplier)
}
export const isDispatchGroup = (group: AdminGroup) => group.status === 'active' && group.subscription_type === 'standard' && !group.is_exclusive && group.rate_multiplier > 0
export const isSharedManualAssignmentGroup = (group: AdminGroup) => group.status === 'active'
  && (group.subscription_type === 'standard' || group.subscription_type === 'subscription')
