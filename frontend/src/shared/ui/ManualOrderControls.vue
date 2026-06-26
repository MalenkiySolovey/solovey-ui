<template>
  <div class="manual-order-controls">
    <ManualSortButton
      :disabled="sortDisabled"
      @sort="emit('sort', $event)"
    />
    <v-btn
      v-if="orderable"
      color="warning"
      variant="tonal"
      :loading="saving"
      :disabled="!dirty"
      @click="emit('save')"
    >
      {{ $t('actions.saveOrder') }}
    </v-btn>
    <v-btn
      v-if="orderable"
      variant="text"
      :disabled="!dirty || saving"
      @click="emit('cancel')"
    >
      {{ $t('actions.cancelOrder') }}
    </v-btn>
  </div>
</template>

<script lang="ts" setup>
import ManualSortButton from '@/components/ManualSortButton.vue'
import type { ManualSortDirection } from '@/shared/composables/dragSelection/manualReorder'

withDefaults(defineProps<{
  sortDisabled?: boolean
  dirty?: boolean
  saving?: boolean
  orderable?: boolean
}>(), {
  sortDisabled: false,
  dirty: false,
  saving: false,
  orderable: true,
})

const emit = defineEmits<{
  sort: [direction: ManualSortDirection]
  save: []
  cancel: []
}>()
</script>

<style scoped>
.manual-order-controls {
  align-items: center;
  display: inline-flex;
  flex-wrap: wrap;
  gap: 8px;
  vertical-align: middle;
}
</style>
