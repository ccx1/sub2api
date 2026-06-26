import { describe, expect, it, vi, beforeEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import AvailableChannelsView from '../AvailableChannelsView.vue'

const { getAvailable, showError } = vi.hoisted(() => ({
  getAvailable: vi.fn(),
  showError: vi.fn(),
}))

const messages: Record<string, string> = {
  'availableChannels.searchPlaceholder': 'Search channels or models...',
  'availableChannels.empty': 'No channels',
  'availableChannels.title': 'Available Channels',
  'availableChannels.noModels': 'No models',
  'availableChannels.pricing.billingMode': 'Billing Mode',
  'availableChannels.pricing.inputPrice': 'Input',
  'availableChannels.pricing.outputPrice': 'Output',
  'availableChannels.pricing.cacheWritePrice': 'Cache Write',
  'availableChannels.pricing.cacheReadPrice': 'Cache Read',
  'availableChannels.pricing.perRequestPrice': 'Price per Request',
  'availableChannels.pricing.unitPerMillion': '/ 1M tokens',
  'availableChannels.pricing.unitPerRequest': '/ request',
  'availableChannels.pricing.imageOutputPrice': 'Image Output',
  'admin.usage.billingModeToken': 'Token',
  'admin.usage.billingModePerRequest': 'Per Request',
  'admin.usage.billingModeImage': 'Image',
  'common.default': 'Default',
  'common.refresh': 'Refresh',
  'common.error': 'Error',
}

vi.mock('@/api/channels', () => ({
  default: {
    getAvailable,
  },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError }),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, fallback?: string) => messages[key] ?? fallback ?? key,
    }),
  }
})

const AppLayoutStub = { template: '<div><slot /></div>' }
const TablePageLayoutStub = {
  template: '<div><slot name="filters" /><slot name="table" /></div>',
}
const IconStub = {
  props: ['name'],
  template: '<span />',
}

function makePricing(overrides: Record<string, unknown>) {
  return {
    billing_mode: 'token',
    input_price: null,
    output_price: null,
    cache_write_price: null,
    cache_read_price: null,
    image_output_price: null,
    per_request_price: null,
    intervals: [],
    ...overrides,
  }
}

describe('AvailableChannelsView model cards', () => {
  beforeEach(() => {
    getAvailable.mockReset()
    showError.mockReset()
  })

  it('shows per-request and image request prices instead of dashes', async () => {
    getAvailable.mockResolvedValue([
      {
        name: 'Codex',
        description: '',
        platforms: [
          {
            platform: 'codex',
            groups: [],
            supported_models: [
              {
                name: 'gpt-5',
                platform: 'codex',
                pricing: makePricing({
                  billing_mode: 'token',
                  input_price: 0.000001,
                  output_price: 0.000002,
                }),
              },
              {
                name: 'gpt-image-2',
                platform: 'codex',
                pricing: makePricing({
                  billing_mode: 'per_request',
                  per_request_price: 0.04,
                }),
              },
              {
                name: 'gpt-image-tiered',
                platform: 'codex',
                pricing: makePricing({
                  billing_mode: 'image',
                  per_request_price: 0.08,
                  intervals: [
                    { min_tokens: 0, max_tokens: null, tier_label: 'HD', input_price: null, output_price: null, cache_write_price: null, cache_read_price: null, per_request_price: 0.12 },
                  ],
                }),
              },
            ],
          },
        ],
      },
    ])

    const wrapper = mount(AvailableChannelsView, {
      global: {
        stubs: {
          AppLayout: AppLayoutStub,
          TablePageLayout: TablePageLayoutStub,
          Icon: IconStub,
        },
      },
    })

    await flushPromises()

    const text = wrapper.text()
    expect(text).toContain('Price per Request')
    expect(text).toContain('Input$1/ 1M tokens')
    expect(text).toContain('Output$2/ 1M tokens')
    expect(text).toContain('Price per Request$0.04/ request')
    expect(text).toContain('Default$0.08/ request')
    expect(text).toContain('HD$0.12/ request')
  })

  it('falls back to legacy image output price for image request pricing', async () => {
    getAvailable.mockResolvedValue([
      {
        name: 'Legacy',
        description: '',
        platforms: [
          {
            platform: 'codex',
            groups: [],
            supported_models: [
              {
                name: 'legacy-image',
                platform: 'codex',
                pricing: makePricing({
                  billing_mode: 'image',
                  image_output_price: 0.05,
                }),
              },
            ],
          },
        ],
      },
    ])

    const wrapper = mount(AvailableChannelsView, {
      global: {
        stubs: {
          AppLayout: AppLayoutStub,
          TablePageLayout: TablePageLayoutStub,
          Icon: IconStub,
        },
      },
    })

    await flushPromises()

    expect(wrapper.text()).toContain('$0.05')
  })
})
