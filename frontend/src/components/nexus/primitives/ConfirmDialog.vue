<template>
  <v-dialog
    :aria-label="title"
    :model-value="modelValue"
    max-width="420"
    @update:model-value="onModel"
  >
    <v-card class="nexus-confirm rounded-xl">
      <v-card-title class="nexus-confirm__title">
        <v-icon :color="tone" :icon="toneIcon" />
        <span>{{ title }}</span>
      </v-card-title>

      <v-card-text v-if="message" class="nexus-confirm__message">
        {{ message }}
      </v-card-text>

      <v-card-text v-if="requirePassword">
        <v-text-field
          v-model="password"
          autocomplete="current-password"
          hide-details
          :label="$t('admin.oldPass')"
          type="password"
        />
      </v-card-text>

      <v-card-actions>
        <v-spacer />
        <v-btn variant="text" @click="cancel">
          {{ cancelLabel ?? $t('actions.close') }}
        </v-btn>
        <v-btn
          :color="tone"
          :disabled="requirePassword && !password"
          :loading="loading"
          variant="flat"
          @click="onConfirm"
        >
          {{ confirmLabel ?? $t('actions.del') }}
        </v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>
</template>

<script lang="ts" setup>
import { computed, ref, watch } from 'vue'

import type { ConfirmTone } from './useConfirm'

const props = withDefaults(defineProps<{
  modelValue: boolean
  title: string
  message?: string
  confirmLabel?: string
  cancelLabel?: string
  tone?: ConfirmTone
  requirePassword?: boolean
  loading?: boolean
}>(), {
  tone: 'error',
})

const emit = defineEmits<{
  confirm: [password?: string]
  cancel: []
  'update:modelValue': [value: boolean]
}>()

const password = ref('')

// Clear the password field whenever the dialog reopens.
watch(() => props.modelValue, open => {
  if (open) password.value = ''
})

const toneIcon = computed(() =>
  props.tone === 'error' ? 'lucide:alert-circle' : 'lucide:info',
)

const onConfirm = () => emit('confirm', props.requirePassword ? password.value : undefined)
const cancel = () => emit('cancel')
const onModel = (value: boolean) => {
  emit('update:modelValue', value)
  if (!value) emit('cancel')
}
</script>

<style scoped>
.nexus-confirm__title {
  align-items: center;
  display: flex;
  font-size: 1rem;
  font-weight: 650;
  gap: var(--nexus-gap-2);
  letter-spacing: 0;
}

.nexus-confirm__message {
  color: var(--nexus-text-secondary);
  font-size: 0.875rem;
}
</style>
