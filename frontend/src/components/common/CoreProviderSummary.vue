<template>
  <span data-testid="core-provider-summary">
    <template v-if="identity?.state === 'VERIFIED'">{{ identity.name }} · {{ identity.runtimeSemanticVersion }}</template>
    <template v-else>{{ t('hostContext.unavailable') }}</template>
  </span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { readCoreProviderIdentity } from '@/types/coreProvider'

const props = defineProps<{ value?: unknown }>()
const { t } = useI18n()
const identity = computed(() => readCoreProviderIdentity(props.value))
</script>
