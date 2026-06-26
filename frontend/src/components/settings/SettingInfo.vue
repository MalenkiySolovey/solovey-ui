<template>
  <v-tooltip
    content-class="setting-info-tooltip"
    location="top"
    max-width="320"
    open-on-click
    :text="text"
  >
    <template #activator="{ props: activatorProps }">
      <v-icon
        v-bind="activatorProps"
        :aria-label="text"
        class="setting-info-icon"
        icon="mdi-information-outline"
        role="img"
        :size="size"
        tabindex="0"
      />
    </template>
  </v-tooltip>
</template>

<script lang="ts" setup>
withDefaults(defineProps<{ text: string; size?: string | number }>(), {
  size: 'small',
})
</script>

<style scoped>
.setting-info-icon {
  cursor: help;
  opacity: 0.62;
}

.setting-info-icon:hover,
.setting-info-icon:focus-visible {
  opacity: 1;
}
</style>

<style>
.setting-info-tooltip {
  background: rgb(24 27 33) !important;
  color: rgb(248 250 252) !important;
  opacity: 1 !important;
}

.setting-info-field .v-field-label.v-field-label--floating {
  max-width: none !important;
  overflow: visible !important;
  text-overflow: clip !important;
  white-space: nowrap;
}
</style>
