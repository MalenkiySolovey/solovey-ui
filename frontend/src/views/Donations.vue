<template>
  <page-header v-if="nexus" :title="$t('support.title')" />
  <v-card :flat="nexus">
    <v-card-title v-if="!nexus">{{ $t('support.title') }}</v-card-title>
    <v-divider v-if="!nexus" />
    <v-card-text class="support-page">
      <img :alt="$t('support.imageAlt')" class="support-page__logo" :src="logoUrl" />
      <p class="support-page__intro">{{ $t('support.intro') }}</p>
      <v-alert density="comfortable" type="info" variant="tonal">
        {{ $t('support.noPaymentDetails') }}
      </v-alert>
      <div class="support-page__actions">
        <v-btn
          color="primary"
          href="https://github.com/MalenkiySolovey/solovey-ui"
          prepend-icon="lucide:github"
          rel="noopener noreferrer"
          target="_blank"
          variant="flat"
        >
          {{ $t('support.github') }}
        </v-btn>
        <v-btn
          href="https://github.com/MalenkiySolovey/solovey-ui/issues"
          prepend-icon="lucide:message-square"
          rel="noopener noreferrer"
          target="_blank"
          variant="tonal"
        >
          {{ $t('support.issues') }}
        </v-btn>
      </div>
    </v-card-text>
  </v-card>
</template>

<script lang="ts" setup>
import { computed } from 'vue'

import logoUrl from '@/assets/logo.png'
import PageHeader from '@/components/nexus/primitives/PageHeader.vue'
import { useUiMode } from '@/uiMode/useUiMode'

const { mode } = useUiMode()
const nexus = computed(() => mode.value === 'nexus')
</script>

<style scoped>
.support-page {
  display: grid;
  gap: 20px;
  margin-inline: auto;
  max-width: 720px;
  padding-block: 32px;
}

.support-page__logo {
  height: 112px;
  justify-self: center;
  object-fit: contain;
  width: 112px;
}

.support-page__intro {
  color: rgba(var(--v-theme-on-surface), 0.78);
  line-height: 1.65;
  margin: 0;
  text-align: center;
}

.support-page__actions {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  justify-content: center;
}
</style>
