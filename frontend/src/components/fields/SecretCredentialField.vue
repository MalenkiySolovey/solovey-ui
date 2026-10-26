<template>
  <div>
    <v-alert v-if="!available" type="info" variant="tonal" density="compact" class="mb-2">{{ $t('capability.historical') }}</v-alert>
    <v-select :model-value="mode" @update:model-value="selectMode" :disabled="!available" :items="choices" :label="label" />
    <v-textarea v-if="mode === 'replace' && multiline" :model-value="replacement" @update:model-value="replace" rows="4" auto-grow
      autocomplete="off" :disabled="!available" :label="$t('secretCredential.replacement')"
      :error-messages="replacement ? [] : [$t('secretCredential.required')]" />
    <v-text-field v-else-if="mode === 'replace'" :model-value="replacement" @update:model-value="replace" type="password"
      autocomplete="new-password" :disabled="!available" :label="$t('secretCredential.replacement')"
      :error-messages="replacement ? [] : [$t('secretCredential.required')]" />
    <div v-else class="text-caption">{{ $t(baseline?.present ? 'secretCredential.present' : 'secretCredential.absent') }}</div>
  </div>
</template>

<script setup lang="ts">
import { computed, shallowRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { unchangedSecret } from '@/features/secretCredentialDraft'

const props = defineProps<{ modelValue?: unknown; available: boolean; label: string; multiline?: boolean }>()
const emit = defineEmits<{ 'update:modelValue': [value: unknown] }>()
const { t } = useI18n()
const baseline = shallowRef(unchangedSecret(props.modelValue))
watch(() => props.modelValue, value => {
  const unchanged = unchangedSecret(value)
  if (unchanged || value === undefined) baseline.value = unchanged
})
const mode = computed(() => {
  const data = props.modelValue as { intent?: string } | undefined
  return data?.intent === 'replace' || data?.intent === 'clear' ? data.intent : 'unchanged'
})
const replacement = computed(() => mode.value === 'replace' ? (props.modelValue as { value: string }).value : '')
const choices = computed(() => [
  { title: t('secretCredential.keep'), value: 'unchanged' },
  { title: t('secretCredential.replace'), value: 'replace' },
  { title: t('secretCredential.clear'), value: 'clear' },
])
function selectMode(value: string) {
  if (!props.available) return
  if (value === 'replace') replace('')
  else if (value === 'clear') emit('update:modelValue', { intent: 'clear' })
  else emit('update:modelValue', baseline.value)
}
function replace(value: string) {
  if (props.available) emit('update:modelValue', { intent: 'replace', value: value ?? '' })
}
</script>
