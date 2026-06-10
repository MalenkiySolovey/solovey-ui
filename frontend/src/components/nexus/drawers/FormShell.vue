<template>
  <!-- Nexus: right-side drawer; Classic: centered dialog. One shell so every
       entity form is presented identically per mode without duplicating bodies. -->
  <entity-drawer
    v-if="mode === 'nexus'"
    :dirty="dirty"
    :loading="loading"
    :model-value="modelValue"
    :save-disabled="saveDisabled"
    :saving="loading"
    :title="title"
    :width="width"
    @close="emit('close')"
    @save="emit('save')"
    @update:model-value="emit('update:modelValue', $event)"
  >
    <slot />
  </entity-drawer>

  <v-dialog
    v-else
    :model-value="modelValue"
    transition="dialog-bottom-transition"
    :width="dialogWidth"
    @update:model-value="emit('update:modelValue', $event)"
  >
    <v-card class="rounded-lg" :loading="loading">
      <v-card-title>{{ title }}</v-card-title>
      <v-divider />
      <v-card-text style="padding: 0 16px; overflow-y: auto;">
        <slot />
      </v-card-text>
      <v-card-actions>
        <v-spacer />
        <v-btn color="primary" variant="outlined" @click="emit('close')">
          {{ $t('actions.close') }}
        </v-btn>
        <v-btn color="primary" variant="tonal" :disabled="saveDisabled" :loading="loading" @click="emit('save')">
          {{ $t('actions.save') }}
        </v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>
</template>

<script lang="ts" setup>
import { useUiMode } from '@/uiMode/useUiMode'
import EntityDrawer from './EntityDrawer.vue'

withDefaults(defineProps<{
  // Optional like v-dialog's: the host modal's v-model falls through to here,
  // so callers need not bind it explicitly (and vue-tsc won't demand it).
  modelValue?: boolean
  title: string
  width?: number
  dialogWidth?: number | string
  loading?: boolean
  dirty?: boolean
  saveDisabled?: boolean
}>(), {
  modelValue: false,
  width: 720,
  dialogWidth: 800,
})

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  save: []
  close: []
}>()

const { mode } = useUiMode()
</script>
