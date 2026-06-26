<template>
  <v-btn
    :color="active ? 'primary' : inactiveColor"
    :disabled="disabled"
    :size="size"
    :variant="active ? 'flat' : inactiveVariant"
    @click="$emit('toggle')"
  >
    <v-icon start icon="mdi-format-list-checks" />
    {{ $t('table.selectRow') }}
  </v-btn>
  <v-btn
    v-if="active && count > 0"
    color="error"
    :size="size"
    variant="tonal"
    @click="$emit('delete')"
  >
    <v-icon start icon="mdi-delete" />
    {{ $t('actions.delbulk') }} ({{ count }})
  </v-btn>
</template>

<script lang="ts" setup>
withDefaults(defineProps<{
  active: boolean
  count: number
  disabled?: boolean
  inactiveColor?: string
  inactiveVariant?: 'text' | 'flat' | 'elevated' | 'tonal' | 'outlined' | 'plain'
  size?: string
}>(), {
  disabled: false,
  inactiveColor: undefined,
  inactiveVariant: 'tonal',
  size: undefined,
})

defineEmits<{
  delete: []
  toggle: []
}>()
</script>
