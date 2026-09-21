<template>
  <AccountTestModal :show="true" :account="account" api-scope="shared-pool" @close="emit('close')" @completed="emit('completed')" />
</template>

<script setup lang="ts">
import AccountTestModal from '@/components/admin/account/AccountTestModal.vue'
import type { SharedAccount } from '@/api/sharedPool'

defineProps<{ account: SharedAccount }>()
const emit = defineEmits<{ close: []; completed: [] }>()
</script>
