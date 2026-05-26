<template>
  <AppLayout>
    <div class="mx-auto max-w-7xl space-y-6">
      <section class="rounded-lg border border-gray-200 bg-white/90 p-5 shadow-sm dark:border-dark-700 dark:bg-dark-800/90 md:p-6">
        <div class="flex flex-col gap-5 lg:flex-row lg:items-start lg:justify-between">
          <div class="max-w-3xl">
            <div class="mb-3 inline-flex items-center gap-2 rounded-full border border-primary-200 bg-primary-50 px-3 py-1 text-xs font-semibold text-primary-700 dark:border-primary-800 dark:bg-primary-900/20 dark:text-primary-300">
              <Icon name="book" size="sm" />
              Sub2API 用户手册
            </div>
            <h2 class="text-2xl font-semibold text-gray-900 dark:text-white md:text-3xl">
              从创建密钥到客户端接入
            </h2>
            <p class="mt-3 text-sm leading-6 text-gray-600 dark:text-gray-300">
              按当前系统已有能力整理，覆盖 API Key、可用渠道、Claude Code、Codex CLI、Gemini CLI、curl、用量和充值。
            </p>
          </div>

          <div class="rounded-lg border border-gray-200 bg-gray-50 p-4 dark:border-dark-700 dark:bg-dark-900/50 lg:w-80">
            <div class="text-xs font-medium uppercase text-gray-500 dark:text-gray-400">当前站点 Base URL</div>
            <div class="mt-2 flex items-center gap-2">
              <code class="min-w-0 flex-1 truncate rounded-md bg-white px-3 py-2 text-sm text-gray-800 dark:bg-dark-800 dark:text-gray-100">
                {{ baseUrl }}
              </code>
              <button class="btn btn-secondary btn-sm flex-shrink-0" type="button" @click="copyText(baseUrl)">
                <Icon name="copy" size="sm" />
              </button>
            </div>
          </div>
        </div>
      </section>

      <div class="grid gap-6 lg:grid-cols-[240px_minmax(0,1fr)]">
        <aside class="hidden lg:block">
          <nav class="sticky top-24 rounded-lg border border-gray-200 bg-white/90 p-3 shadow-sm dark:border-dark-700 dark:bg-dark-800/90" aria-label="使用说明目录">
            <a
              v-for="item in sections"
              :key="item.id"
              :href="`#${item.id}`"
              class="flex items-center gap-2 rounded-md px-3 py-2 text-sm text-gray-600 transition hover:bg-gray-100 hover:text-gray-900 dark:text-gray-300 dark:hover:bg-dark-700 dark:hover:text-white"
            >
              <Icon :name="item.icon" size="sm" />
              <span class="truncate">{{ item.title }}</span>
            </a>
          </nav>
        </aside>

        <main class="space-y-6">
          <section id="prepare" class="guide-section">
            <GuideSectionHeader icon="checkCircle" title="使用前准备" />
            <div class="grid gap-3 md:grid-cols-3">
              <div v-for="item in prepareItems" :key="item.title" class="guide-panel">
                <div class="text-sm font-semibold text-gray-900 dark:text-white">{{ item.title }}</div>
                <p class="mt-2 text-sm leading-6 text-gray-600 dark:text-gray-300">{{ item.description }}</p>
              </div>
            </div>
          </section>

          <section id="key" class="guide-section">
            <GuideSectionHeader icon="key" title="1. 创建 API Key" />
            <ol class="space-y-3 text-sm leading-6 text-gray-700 dark:text-gray-300">
              <li v-for="step in keySteps" :key="step" class="flex gap-3">
                <span class="mt-0.5 flex h-6 w-6 flex-shrink-0 items-center justify-center rounded-full bg-primary-100 text-xs font-semibold text-primary-700 dark:bg-primary-900/30 dark:text-primary-300">
                  {{ keySteps.indexOf(step) + 1 }}
                </span>
                <span>{{ step }}</span>
              </li>
            </ol>
            <div class="mt-4 rounded-lg border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-800 dark:border-amber-800 dark:bg-amber-900/20 dark:text-amber-200">
              API Key 只在客户端调用时使用，请不要提交到代码仓库，也不要公开到截图或聊天记录中。
            </div>
          </section>

          <section id="channels" class="guide-section">
            <GuideSectionHeader icon="cube" title="2. 查看可用渠道和模型" />
            <p class="guide-text">
              进入“可用渠道”页面，确认当前账户能使用的渠道、模型和定价。客户端里的模型名应以该页面展示为准。
            </p>
            <div class="mt-4 grid gap-3 md:grid-cols-2">
              <div v-for="item in channelHints" :key="item.title" class="guide-panel">
                <div class="flex items-center gap-2 text-sm font-semibold text-gray-900 dark:text-white">
                  <Icon :name="item.icon" size="sm" />
                  {{ item.title }}
                </div>
                <p class="mt-2 text-sm leading-6 text-gray-600 dark:text-gray-300">{{ item.description }}</p>
              </div>
            </div>
          </section>

          <section id="clients" class="guide-section">
            <GuideSectionHeader icon="terminal" title="3. 配置客户端" />
            <div class="mb-4 flex flex-wrap gap-2" role="tablist" aria-label="客户端配置示例">
              <button
                v-for="client in clientExamples"
                :key="client.id"
                type="button"
                class="inline-flex items-center gap-2 rounded-md border px-3 py-2 text-sm font-medium transition"
                :class="activeClient === client.id
                  ? 'border-primary-300 bg-primary-50 text-primary-700 dark:border-primary-700 dark:bg-primary-900/30 dark:text-primary-300'
                  : 'border-gray-200 bg-white text-gray-600 hover:bg-gray-50 dark:border-dark-700 dark:bg-dark-800 dark:text-gray-300 dark:hover:bg-dark-700'"
                role="tab"
                :aria-selected="activeClient === client.id"
                @click="activeClient = client.id"
              >
                <Icon :name="client.icon" size="sm" />
                {{ client.title }}
              </button>
            </div>

            <div class="rounded-lg border border-gray-200 bg-gray-50 dark:border-dark-700 dark:bg-dark-900/60">
              <div class="border-b border-gray-200 px-4 py-3 dark:border-dark-700">
                <div class="text-sm font-semibold text-gray-900 dark:text-white">{{ selectedClient.title }}</div>
                <p class="mt-1 text-sm text-gray-600 dark:text-gray-300">{{ selectedClient.description }}</p>
              </div>
              <div class="space-y-4 p-4">
                <CodeBlock
                  v-for="snippet in selectedClient.snippets"
                  :key="snippet.label"
                  :label="snippet.label"
                  :code="snippet.code"
                  @copy="copyText"
                />
              </div>
            </div>
          </section>

          <section id="usage" class="guide-section">
            <GuideSectionHeader icon="chart" title="4. 查看用量、充值和订单" />
            <div class="grid gap-3 md:grid-cols-2">
              <div v-for="item in usageItems" :key="item.title" class="guide-panel">
                <div class="text-sm font-semibold text-gray-900 dark:text-white">{{ item.title }}</div>
                <p class="mt-2 text-sm leading-6 text-gray-600 dark:text-gray-300">{{ item.description }}</p>
              </div>
            </div>
          </section>

          <section id="faq" class="guide-section">
            <GuideSectionHeader icon="questionCircle" title="5. 常见问题" />
            <div class="divide-y divide-gray-200 dark:divide-dark-700">
              <details v-for="item in faqItems" :key="item.question" class="group py-4 first:pt-0 last:pb-0">
                <summary class="flex cursor-pointer list-none items-center justify-between gap-4 text-sm font-semibold text-gray-900 dark:text-white">
                  {{ item.question }}
                  <Icon name="chevronDown" size="sm" class="flex-shrink-0 transition group-open:rotate-180" />
                </summary>
                <p class="mt-3 text-sm leading-6 text-gray-600 dark:text-gray-300">{{ item.answer }}</p>
              </details>
            </div>
          </section>
        </main>
      </div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, defineComponent, h, ref, type PropType } from 'vue'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import { useClipboard } from '@/composables/useClipboard'

type IconName = InstanceType<typeof Icon>['$props']['name']

interface Snippet {
  label: string
  code: string
}

interface ClientExample {
  id: string
  title: string
  icon: IconName
  description: string
  snippets: Snippet[]
}

const { copyToClipboard } = useClipboard()
const activeClient = ref('claude')

const baseUrl = computed(() => {
  if (typeof window === 'undefined') return 'https://api.example.com'
  return window.location.origin
})

const sections: Array<{ id: string; title: string; icon: IconName }> = [
  { id: 'prepare', title: '使用前准备', icon: 'checkCircle' },
  { id: 'key', title: '创建 API Key', icon: 'key' },
  { id: 'channels', title: '渠道与模型', icon: 'cube' },
  { id: 'clients', title: '客户端接入', icon: 'terminal' },
  { id: 'usage', title: '用量与充值', icon: 'chart' },
  { id: 'faq', title: '常见问题', icon: 'questionCircle' },
]

const prepareItems = [
  { title: '已经登录账户', description: '使用说明页面面向已登录用户，API Key、用量和订单都绑定当前账户。' },
  { title: '有可用权限', description: '账户需要有余额、订阅、兑换额度，或管理员分配的可用分组。' },
  { title: '渠道已配置', description: '管理员需要先配置上游账号、分组、渠道和模型，用户侧才能正常调用。' },
]

const keySteps = [
  '进入“API 密钥”页面，点击“创建密钥”。',
  '填写能区分用途的名称，例如 codex-local、claude-code 或 gemini-cli。',
  '创建后保存生成的 sk-... 密钥，并按需设置可访问分组、额度和有效期。',
  '点击“使用密钥”，按当前分组平台复制对应客户端配置。',
]

const channelHints: Array<{ title: string; icon: IconName; description: string }> = [
  { title: 'Claude Code', icon: 'terminal', description: '通常使用 Anthropic/Claude 兼容入口，按“使用密钥”弹窗生成的环境变量配置。' },
  { title: 'Codex CLI', icon: 'document', description: '通常使用 OpenAI Responses 兼容入口，配置 config.toml 和 auth.json。' },
  { title: 'Gemini CLI', icon: 'sparkles', description: '通常使用 Gemini v1beta 兼容入口，配置 GOOGLE_GEMINI_BASE_URL 和 GEMINI_API_KEY。' },
  { title: 'curl/第三方工具', icon: 'link', description: '按工具支持的协议选择 /v1/chat/completions、/v1/responses、/v1/messages 或 /v1beta。' },
]

const clientExamples = computed<ClientExample[]>(() => [
  {
    id: 'claude',
    title: 'Claude Code',
    icon: 'terminal',
    description: '适用于 Claude/Anthropic 兼容分组。',
    snippets: [
      {
        label: 'Linux / macOS',
        code: `export ANTHROPIC_BASE_URL="${baseUrl.value}"
export ANTHROPIC_AUTH_TOKEN="sk-xxxx"
export CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1`,
      },
      {
        label: 'PowerShell',
        code: `$env:ANTHROPIC_BASE_URL="${baseUrl.value}"
$env:ANTHROPIC_AUTH_TOKEN="sk-xxxx"
$env:CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC="1"`,
      },
    ],
  },
  {
    id: 'codex',
    title: 'Codex CLI',
    icon: 'document',
    description: '适用于 OpenAI/Responses 兼容分组。完整文件以“使用密钥”弹窗生成内容为准。',
    snippets: [
      {
        label: '~/.codex/config.toml',
        code: `model_provider = "sub2api"
model = "gpt-5.4"
review_model = "gpt-5.4"

[model_providers.sub2api]
name = "sub2api"
base_url = "${baseUrl.value}"
wire_api = "responses"
requires_openai_auth = true`,
      },
      {
        label: '~/.codex/auth.json',
        code: `{
  "OPENAI_API_KEY": "sk-xxxx"
}`,
      },
    ],
  },
  {
    id: 'gemini',
    title: 'Gemini CLI',
    icon: 'sparkles',
    description: '适用于 Gemini 兼容分组。',
    snippets: [
      {
        label: 'Linux / macOS',
        code: `export GOOGLE_GEMINI_BASE_URL="${baseUrl.value}"
export GEMINI_API_KEY="sk-xxxx"
export GEMINI_MODEL="gemini-2.0-flash"`,
      },
      {
        label: 'PowerShell',
        code: `$env:GOOGLE_GEMINI_BASE_URL="${baseUrl.value}"
$env:GEMINI_API_KEY="sk-xxxx"
$env:GEMINI_MODEL="gemini-2.0-flash"`,
      },
    ],
  },
  {
    id: 'curl',
    title: 'curl',
    icon: 'link',
    description: '用于快速验证 API Key、Base URL 和模型是否可用。',
    snippets: [
      {
        label: 'OpenAI Chat Completions',
        code: `curl "${baseUrl.value}/v1/chat/completions" \\
  -H "Authorization: Bearer sk-xxxx" \\
  -H "Content-Type: application/json" \\
  -d '{
    "model": "gpt-5.4",
    "messages": [
      {"role": "user", "content": "hello"}
    ]
  }'`,
      },
      {
        label: 'Anthropic Messages',
        code: `curl "${baseUrl.value}/v1/messages" \\
  -H "Authorization: Bearer sk-xxxx" \\
  -H "Content-Type: application/json" \\
  -d '{
    "model": "claude-sonnet-4-6",
    "max_tokens": 1024,
    "messages": [
      {"role": "user", "content": "hello"}
    ]
  }'`,
      },
      {
        label: 'Gemini v1beta',
        code: `curl "${baseUrl.value}/v1beta/models/gemini-2.0-flash:generateContent" \\
  -H "x-goog-api-key: sk-xxxx" \\
  -H "Content-Type: application/json" \\
  -d '{
    "contents": [
      {
        "parts": [
          {"text": "hello"}
        ]
      }
    ]
  }'`,
      },
    ],
  },
])

const selectedClient = computed(() => {
  return clientExamples.value.find((client) => client.id === activeClient.value) ?? clientExamples.value[0]
})

const usageItems = [
  { title: '仪表盘', description: '查看近期请求、费用、Token 和趋势概览。' },
  { title: '使用记录', description: '查看请求明细、模型、Token、费用、端点和响应状态。' },
  { title: '充值/订阅', description: '管理员启用支付后，可在这里购买套餐或充值。' },
  { title: '我的订单', description: '管理员启用支付后，可查看订单状态和支付结果。' },
]

const faqItems = [
  { question: 'API Key 无法使用怎么办？', answer: '先检查密钥是否过期、停用、额度耗尽，或是否没有分配可访问分组。' },
  { question: '看不到可用渠道怎么办？', answer: '通常是当前账户没有可访问分组，或管理员关闭了“可用渠道”入口。请联系管理员确认分组和渠道配置。' },
  { question: '客户端提示 401 怎么办？', answer: '通常是 API Key 不正确、复制时多了空格，或客户端没有按要求把 Key 放到请求头里。' },
  { question: '客户端提示模型不存在怎么办？', answer: '以“可用渠道”页面展示的模型名为准，确认客户端请求里的 model 字段是否一致。' },
  { question: 'Codex CLI 经过 Nginx 后异常怎么办？', answer: '如果请求包含带下划线的请求头，需要在 Nginx http 块启用 underscores_in_headers on。' },
]

async function copyText(text: string) {
  await copyToClipboard(text, '已复制')
}

const GuideSectionHeader = defineComponent({
  name: 'GuideSectionHeader',
  props: {
    icon: { type: String as PropType<IconName>, required: true },
    title: { type: String, required: true },
  },
  setup(props) {
    return () => h('div', { class: 'mb-4 flex items-center gap-3' }, [
      h('div', { class: 'flex h-9 w-9 items-center justify-center rounded-lg bg-primary-100 text-primary-700 dark:bg-primary-900/30 dark:text-primary-300' }, [
        h(Icon, { name: props.icon, size: 'md' }),
      ]),
      h('h3', { class: 'text-lg font-semibold text-gray-900 dark:text-white' }, props.title),
    ])
  },
})

const CodeBlock = defineComponent({
  name: 'CodeBlock',
  props: {
    label: { type: String, required: true },
    code: { type: String, required: true },
  },
  emits: {
    copy: (_code: string) => true,
  },
  setup(props, { emit }) {
    return () => h('div', { class: 'overflow-hidden rounded-lg border border-gray-800 bg-gray-950' }, [
      h('div', { class: 'flex items-center justify-between gap-3 border-b border-gray-800 px-4 py-2' }, [
        h('span', { class: 'min-w-0 truncate text-xs font-medium text-gray-300' }, props.label),
        h('button', {
          type: 'button',
          class: 'inline-flex items-center gap-1 rounded-md bg-gray-800 px-2.5 py-1 text-xs font-medium text-gray-200 transition hover:bg-gray-700',
          onClick: () => emit('copy', props.code),
        }, [
          h(Icon, { name: 'copy', size: 'xs' }),
          '复制',
        ]),
      ]),
      h('pre', { class: 'overflow-x-auto p-4 text-sm leading-6 text-gray-100' }, [
        h('code', props.code),
      ]),
    ])
  },
})
</script>

<style scoped>
.guide-section {
  scroll-margin-top: 6rem;
  border: 1px solid rgb(229 231 235);
  border-radius: 0.5rem;
  background: rgb(255 255 255 / 0.9);
  padding: 1.25rem;
  box-shadow: 0 1px 2px rgb(0 0 0 / 0.04);
}

.dark .guide-section {
  border-color: rgb(55 65 81);
  background: rgb(31 32 29 / 0.9);
}

.guide-panel {
  border: 1px solid rgb(229 231 235);
  border-radius: 0.5rem;
  background: rgb(249 250 251 / 0.86);
  padding: 1rem;
}

.dark .guide-panel {
  border-color: rgb(55 65 81);
  background: rgb(23 23 20 / 0.45);
}

.guide-text {
  font-size: 0.875rem;
  line-height: 1.625;
  color: rgb(75 85 99);
}

.dark .guide-text {
  color: rgb(209 213 219);
}
</style>
