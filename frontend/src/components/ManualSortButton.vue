<template>
  <div
    class="manual-sort-buttons"
    role="group"
    :aria-label="groupLabel"
  >
    <v-btn
      class="manual-sort-buttons__button"
      :aria-label="ascLabel"
      :color="color"
      :density="density"
      :disabled="disabled"
      :size="size"
      :title="ascLabel"
      :variant="variant"
      @click="emit('sort', 'asc')"
    >
      A-Z
    </v-btn>
    <v-btn
      class="manual-sort-buttons__button"
      :aria-label="descLabel"
      :color="color"
      :density="density"
      :disabled="disabled"
      :size="size"
      :title="descLabel"
      :variant="variant"
      @click="emit('sort', 'desc')"
    >
      Z-A
    </v-btn>
  </div>
</template>

<script lang="ts" setup>
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import type { ManualSortDirection } from '@/composables/useManualReorder'

type ButtonVariant = 'flat' | 'text' | 'elevated' | 'outlined' | 'plain' | 'tonal'

const props = withDefaults(defineProps<{
  disabled?: boolean
  color?: string
  density?: 'default' | 'comfortable' | 'compact'
  size?: string
  variant?: ButtonVariant
}>(), {
  disabled: false,
  color: 'secondary',
  density: 'compact',
  size: 'small',
  variant: 'tonal',
})

const emit = defineEmits<{
  sort: [direction: ManualSortDirection]
}>()

const { t } = useI18n()

const ascLabel = computed(() => t('actions.sortByNameAsc'))
const descLabel = computed(() => t('actions.sortByNameDesc'))
const groupLabel = computed(() => `${ascLabel.value} / ${descLabel.value}`)
</script>

<style scoped>
.manual-sort-buttons {
  align-items: center;
  display: inline-flex;
  gap: 4px;
  vertical-align: middle;
}

.manual-sort-buttons__button {
  letter-spacing: 0;
  min-width: 48px;
  padding-inline: 10px;
}
</style>
