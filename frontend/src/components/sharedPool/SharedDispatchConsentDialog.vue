<template>
  <BaseDialog :show="true" :title="t('sharedPool.authorizeDispatch')" @close="emit('close')">
    <p class="mb-3 text-sm">{{ account.name }} · {{ t('sharedPool.dispatchConsentHint') }}</p>
    <SharedSettlementNotice :config="config" :platform="account.platform" />
    <SharedRevenueSplit class="mt-3" :config="config" :use-random-proxy="account.proxy_mode === 'random'" inline />
    <p v-if="!available" class="mt-3 text-sm text-amber-600">{{ t('sharedPool.settlementRequired') }}</p>
    <template #footer><button class="btn btn-secondary" @click="emit('close')">{{ t('common.cancel') }}</button><button class="btn btn-primary" :disabled="!available" @click="emit('confirm')">{{ t('sharedPool.authorizeDispatch') }}</button></template>
  </BaseDialog>
</template>
<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import type { SharedAccount, SharedConfig } from '@/api/sharedPool'
import SharedSettlementNotice from './SharedSettlementNotice.vue'
import SharedRevenueSplit from './SharedRevenueSplit.vue'
import { hasSettlementPolicy } from './settlementPolicy'
const props = defineProps<{ account: SharedAccount; config: SharedConfig }>()
const emit = defineEmits<{ close: []; confirm: [] }>()
const { t } = useI18n()
const available = computed(() => hasSettlementPolicy(props.config))
</script>
