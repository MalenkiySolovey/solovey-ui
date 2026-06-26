<template>
  <v-card
    rounded="xl"
    elevation="5"
    min-width="200"
    :title="title"
    class="classic-config-card"
    :class="{ 'classic-config-card--selected': selected }"
  >
    <div v-if="selectMode" class="classic-config-card__select manual-drag-no-drag">
      <v-checkbox-btn
        :model-value="selected"
        :aria-label="$t('table.selectRow')"
        density="compact"
        @update:model-value="emit('update:selected', Boolean($event))"
      />
    </div>
    <v-card-subtitle style="margin-top: -15px;">
      <v-row><v-col>{{ subtitle }}</v-col></v-row>
    </v-card-subtitle>
    <v-card-text>
      <v-row v-for="row in rows" :key="row.label">
        <v-col>{{ row.label }}</v-col>
        <v-col>{{ row.value }}</v-col>
      </v-row>
    </v-card-text>
    <v-divider></v-divider>
    <v-card-actions style="padding: 0;">
      <v-btn icon="mdi-file-edit" @click="emit('edit')">
        <v-icon /><v-tooltip activator="parent" location="top" :text="$t('actions.edit')"></v-tooltip>
      </v-btn>
      <v-btn icon="mdi-file-remove" style="margin-inline-start:0;" color="warning" @click="emit('update:deleteOpen', true)">
        <v-icon /><v-tooltip activator="parent" location="top" :text="$t('actions.del')"></v-tooltip>
      </v-btn>
      <v-overlay
        :model-value="deleteOpen"
        contained
        class="align-center justify-center"
        @update:model-value="emit('update:deleteOpen', Boolean($event))"
      >
        <v-card :title="$t('actions.del')" rounded="lg">
          <v-divider></v-divider>
          <v-card-text>{{ $t('confirm') }}</v-card-text>
          <v-card-actions>
            <v-btn color="error" variant="outlined" @click="emit('delete')">{{ $t('yes') }}</v-btn>
            <v-btn color="success" variant="outlined" @click="emit('update:deleteOpen', false)">{{ $t('no') }}</v-btn>
          </v-card-actions>
        </v-card>
      </v-overlay>
    </v-card-actions>
  </v-card>
</template>

<script setup lang="ts">
defineProps<{
  deleteOpen?: boolean
  rows: Array<{ label: string; value: unknown }>
  selected?: boolean
  selectMode?: boolean
  subtitle: string
  title: string | number
}>()

const emit = defineEmits<{
  delete: []
  edit: []
  'update:deleteOpen': [value: boolean]
  'update:selected': [value: boolean]
}>()
</script>

<style scoped>
.classic-config-card {
  position: relative;
}

.classic-config-card--selected {
  outline: 2px solid rgb(var(--v-theme-primary));
  outline-offset: 2px;
}

.classic-config-card__select {
  position: absolute;
  right: 8px;
  top: 8px;
  z-index: 2;
}
</style>
