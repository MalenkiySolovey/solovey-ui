<template>
  <page-header v-if="nexus" :title="$t('pages.singBoxConfig')" />

  <page-toolbar v-if="nexus">
    <template #actions>
      <v-btn prepend-icon="lucide:copy" variant="tonal" @click="copyConfig">
        {{ $t('copyToClipboard') }}
      </v-btn>
      <v-btn
        :loading="refreshing"
        prepend-icon="lucide:rotate-cw"
        variant="text"
        @click="refreshConfig"
      >
        {{ $t('actions.refresh') }}
      </v-btn>
    </template>
  </page-toolbar>

  <v-card :flat="nexus">
    <template v-if="!nexus">
      <v-card-title>{{ $t('pages.singBoxConfig') }}</v-card-title>
      <v-divider />
    </template>

    <v-card-text>
      <v-row v-if="!nexus" class="mb-2" justify="end">
        <v-col cols="auto">
          <v-btn prepend-icon="mdi-content-copy" variant="tonal" @click="copyConfig">
            {{ $t('copyToClipboard') }}
          </v-btn>
        </v-col>
        <v-col cols="auto">
          <v-btn
            :loading="refreshing"
            prepend-icon="mdi-refresh"
            variant="text"
            @click="refreshConfig"
          >
            {{ $t('actions.refresh') }}
          </v-btn>
        </v-col>
      </v-row>

      <v-textarea
        :model-value="configText"
        class="sing-box-config__textarea"
        hide-details
        no-resize
        readonly
        rows="28"
        spellcheck="false"
        variant="outlined"
      />
    </v-card-text>
  </v-card>
</template>

<script lang="ts" setup>
import PageHeader from '@/components/nexus/primitives/PageHeader.vue'
import PageToolbar from '@/components/nexus/primitives/PageToolbar.vue'
import { useSingBoxConfigPage } from '@/shared/composables/pages/useSingBoxConfigPage'

const { configText, copyConfig, nexus, refreshConfig, refreshing } = useSingBoxConfigPage()
</script>

<style scoped>
.sing-box-config__textarea :deep(textarea) {
  font-family: var(--nexus-font-mono, "Cascadia Mono", Consolas, "Courier New", monospace);
  font-size: 0.82rem;
  line-height: 1.45;
}
</style>
