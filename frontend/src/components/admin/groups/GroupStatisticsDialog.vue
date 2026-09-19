<template>
  <BaseDialog
    :show="groupId !== null"
    :title="stats ? t('admin.groups.statistics.titleWithName', { name: stats.group_name }) : t('admin.groups.statistics.title')"
    width="wide"
    @close="emit('close')"
  >
    <p class="text-xs leading-relaxed text-gray-500 dark:text-gray-400">
      {{ t("admin.groups.statistics.description") }}
    </p>

    <div class="mt-4 flex flex-wrap items-end gap-3">
      <label class="text-xs text-gray-600 dark:text-gray-300">
        {{ t("admin.groups.statistics.from") }}
        <input v-model="from" type="datetime-local" class="input mt-1" />
      </label>
      <label class="text-xs text-gray-600 dark:text-gray-300">
        {{ t("admin.groups.statistics.to") }}
        <input v-model="to" type="datetime-local" class="input mt-1" />
      </label>
      <button type="button" class="btn btn-secondary" :disabled="loading" @click="load">
        {{ t("admin.groups.statistics.apply") }}
      </button>
      <button type="button" class="btn btn-secondary" :disabled="loading" @click="loadAll">
        {{ t("admin.groups.statistics.allHistory") }}
      </button>
    </div>

    <p v-if="loading" role="status" class="py-8 text-center text-sm text-gray-500 dark:text-gray-400">
      {{ t("admin.groups.statistics.loading") }}
    </p>
    <p v-if="error" role="alert" class="mt-4 text-sm text-red-600 dark:text-red-400">
      {{ error }}
    </p>

    <div v-if="stats && !loading" class="mt-5 space-y-4">
      <div class="grid gap-3 sm:grid-cols-3">
        <div
          v-for="item in amounts"
          :key="item.label"
          class="rounded-lg border border-gray-200 bg-gray-50 p-4 dark:border-dark-700 dark:bg-dark-900/70"
        >
          <p class="text-xs text-gray-500 dark:text-gray-400">{{ item.label }}</p>
          <p class="mt-2 break-all text-lg font-semibold tabular-nums text-gray-900 dark:text-white">
            ${{ money(item.value) }}
          </p>
          <p class="mt-2 text-xs text-gray-500 dark:text-gray-400">{{ item.note }}</p>
        </div>
      </div>

      <dl class="grid grid-cols-2 gap-4 rounded-lg border border-gray-200 p-4 text-sm dark:border-dark-700 sm:grid-cols-3">
        <div>
          <dt class="text-xs text-gray-500 dark:text-gray-400">{{ t("admin.groups.statistics.totalRequests") }}</dt>
          <dd>{{ stats.total_requests.toLocaleString() }}</dd>
        </div>
        <div>
          <dt class="text-xs text-gray-500 dark:text-gray-400">{{ t("admin.groups.statistics.totalTokens") }}</dt>
          <dd>{{ stats.total_tokens.toLocaleString() }}</dd>
        </div>
        <div>
          <dt class="text-xs text-gray-500 dark:text-gray-400">{{ t("admin.groups.statistics.averageDuration") }}</dt>
          <dd>{{ (stats.average_duration_ms / 1000).toFixed(2) }}s</dd>
        </div>
        <div>
          <dt class="text-xs text-gray-500 dark:text-gray-400">{{ t("admin.groups.statistics.totalApiKeys") }}</dt>
          <dd>{{ stats.total_api_keys }}</dd>
        </div>
        <div>
          <dt class="text-xs text-gray-500 dark:text-gray-400">{{ t("admin.groups.statistics.activeApiKeys") }}</dt>
          <dd>{{ stats.active_api_keys }}</dd>
        </div>
        <div>
          <dt class="text-xs text-gray-500 dark:text-gray-400">{{ t("admin.groups.statistics.totalAccounts") }}</dt>
          <dd>{{ stats.total_accounts }}</dd>
        </div>
        <div>
          <dt class="text-xs text-gray-500 dark:text-gray-400">{{ t("admin.groups.statistics.balanceCost") }}</dt>
          <dd>${{ money(stats.balance_cost) }}</dd>
        </div>
        <div>
          <dt class="text-xs text-gray-500 dark:text-gray-400">{{ t("admin.groups.statistics.subscriptionCost") }}</dt>
          <dd>${{ money(stats.subscription_cost) }}</dd>
        </div>
        <div>
          <dt class="text-xs text-gray-500 dark:text-gray-400">{{ t("admin.groups.statistics.zeroChargeRequests") }}</dt>
          <dd>{{ stats.zero_charge_requests }}</dd>
        </div>
      </dl>

      <p v-if="stats.total_requests === 0" class="text-sm text-gray-500 dark:text-gray-400">
        {{ t("admin.groups.statistics.empty") }}
      </p>
      <p class="text-xs text-gray-500 dark:text-gray-400">
        {{ rangeLabel }} · {{ t("admin.groups.statistics.generatedAt", { time: new Date(stats.generated_at).toLocaleString() }) }}
      </p>
    </div>

    <template #footer>
      <button type="button" class="btn btn-secondary" @click="emit('close')">
        {{ t("common.close") }}
      </button>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import BaseDialog from "@/components/common/BaseDialog.vue";
import { getStats, type GroupDetailStats } from "@/api/admin/groups";
import { extractApiErrorMessage } from "@/utils/apiError";

const props = defineProps<{ groupId: number | null }>();
const emit = defineEmits<{ close: [] }>();

const { t } = useI18n();
const stats = ref<GroupDetailStats | null>(null);
const loading = ref(false);
const error = ref("");
const from = ref("");
const to = ref("");
let controller: AbortController | undefined;
let version = 0;

const money = (value: number) =>
  value.toLocaleString("en-US", {
    minimumFractionDigits: 2,
    maximumFractionDigits: 10,
  });

const amounts = computed(() =>
  stats.value
    ? [
        {
          label: t("admin.groups.statistics.actualCost"),
          value: stats.value.total_actual_cost,
          note: t("admin.groups.statistics.actualCostNote"),
        },
        {
          label: t("admin.groups.statistics.bookCost"),
          value: stats.value.total_cost,
          note: t("admin.groups.statistics.bookCostNote"),
        },
        {
          label: t("admin.groups.statistics.accountCost"),
          value: stats.value.total_account_cost,
          note: t("admin.groups.statistics.accountCostNote"),
        },
      ]
    : [],
);

const rangeLabel = computed(() => {
  if (stats.value?.from || stats.value?.to) {
    return t("admin.groups.statistics.rangeSelected");
  }
  return t("admin.groups.statistics.rangeAll");
});

const toISOStringParam = (value: string, label: string) => {
  if (!value) return undefined;
  const date = new Date(value);
  if (!Number.isFinite(date.getTime())) {
    error.value = t("admin.groups.statistics.invalidTime", { field: label });
    return null;
  }
  return date.toISOString();
};

async function load() {
  if (props.groupId === null) return;

  const fromParam = toISOStringParam(from.value, t("admin.groups.statistics.from"));
  if (fromParam === null) return;
  const toParam = toISOStringParam(to.value, t("admin.groups.statistics.to"));
  if (toParam === null) return;
  if (fromParam && toParam && fromParam >= toParam) {
    error.value = t("admin.groups.statistics.invalidRange");
    return;
  }

  const current = ++version;
  controller?.abort();
  controller = new AbortController();
  loading.value = true;
  error.value = "";

  try {
    const result = await getStats(
      props.groupId,
      { from: fromParam, to: toParam },
      controller.signal,
    );
    if (current === version) {
      stats.value = result;
    }
  } catch (err) {
    if (current === version) {
      stats.value = null;
      error.value = extractApiErrorMessage(err, t("admin.groups.statistics.failedToLoad"));
    }
  } finally {
    if (current === version) {
      loading.value = false;
    }
  }
}

function loadAll() {
  from.value = "";
  to.value = "";
  void load();
}

watch(
  () => props.groupId,
  () => {
    version++;
    controller?.abort();
    stats.value = null;
    error.value = "";
    from.value = "";
    to.value = "";
    void load();
  },
  { immediate: true },
);

onUnmounted(() => {
  version++;
  controller?.abort();
});
</script>
