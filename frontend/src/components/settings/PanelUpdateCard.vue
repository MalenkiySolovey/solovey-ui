<template>
  <section class="panel-update">
    <div class="panel-update__heading">
      <v-icon color="primary" icon="lucide:cloud-download" />
      <div>
        <h3>{{ $t('update.title') }}</h3>
        <p>{{ $t('update.subtitle') }}</p>
      </div>
    </div>

    <div class="panel-update__row">
      <v-btn-toggle v-model="channel" color="primary" density="comfortable" divided mandatory variant="outlined">
        <v-btn value="main">{{ $t('update.channelMain') }}</v-btn>
        <v-btn value="beta">{{ $t('update.channelBeta') }}</v-btn>
      </v-btn-toggle>
      <v-btn color="primary" prepend-icon="lucide:refresh-cw" :loading="checking" variant="tonal" @click="checkUpdates">
        {{ $t('update.check') }}
      </v-btn>
    </div>

    <div class="panel-update__versions">
      <span>{{ $t('update.current') }}: <strong>{{ status?.current || '-' }}</strong></span>
      <v-icon icon="lucide:arrow-right" size="small" />
      <span>{{ $t('update.available') }}: <strong>{{ status?.latest || '-' }}</strong></span>
      <v-chip v-if="status?.latest" :color="status.prerelease ? 'warning' : 'success'" density="compact" label size="small">
        {{ status.prerelease ? $t('update.beta') : $t('update.stable') }}
      </v-chip>
    </div>

    <v-alert v-if="status?.checkError" density="compact" type="warning" variant="tonal">{{ $t('update.checkFailed') }}</v-alert>
    <v-alert v-else-if="status && !status.updateAvailable && status.latest" density="compact" type="success" variant="tonal">
      {{ $t('update.upToDate') }}
    </v-alert>
    <v-alert v-else-if="status?.updateAvailable && !status.assetAvailable" density="compact" type="warning" variant="tonal">
      {{ $t('update.assetUnavailable') }}
    </v-alert>

    <v-sheet v-if="status?.updateAvailable && status.releaseNotes" class="panel-update__notes" rounded>
      <div class="panel-update__notes-title">{{ $t('update.releaseNotes') }}</div>
      <div class="panel-update__notes-body">
        <template v-for="(block, index) in releaseNoteBlocks" :key="index">
          <component
            :is="headingTag(block.level)"
            v-if="block.type === 'heading'"
            class="panel-update__notes-heading"
          >
            <template v-for="(segment, segmentIndex) in block.inline" :key="segmentIndex">
              <code v-if="segment.type === 'code'">{{ segment.text }}</code>
              <strong v-else-if="segment.type === 'strong'">{{ segment.text }}</strong>
              <span v-else>{{ segment.text }}</span>
            </template>
          </component>
          <p v-else-if="block.type === 'paragraph'" class="panel-update__notes-paragraph">
            <template v-for="(segment, segmentIndex) in block.inline" :key="segmentIndex">
              <code v-if="segment.type === 'code'">{{ segment.text }}</code>
              <strong v-else-if="segment.type === 'strong'">{{ segment.text }}</strong>
              <span v-else>{{ segment.text }}</span>
            </template>
          </p>
          <component
            :is="block.ordered ? 'ol' : 'ul'"
            v-else-if="block.type === 'list'"
            class="panel-update__notes-list"
          >
            <li v-for="(item, itemIndex) in block.items" :key="itemIndex">
              <template v-for="(segment, segmentIndex) in item" :key="segmentIndex">
                <code v-if="segment.type === 'code'">{{ segment.text }}</code>
                <strong v-else-if="segment.type === 'strong'">{{ segment.text }}</strong>
                <span v-else>{{ segment.text }}</span>
              </template>
            </li>
          </component>
          <pre v-else-if="block.type === 'code'" class="panel-update__notes-code"><code>{{ block.text }}</code></pre>
          <v-divider v-else-if="block.type === 'rule'" class="my-3" />
        </template>
      </div>
    </v-sheet>

    <div v-if="jobActive" class="panel-update__progress">
      <v-progress-linear color="primary" indeterminate rounded />
      <span>{{ $t(`update.stage.${status?.job?.stage || 'idle'}`) }}</span>
    </div>
    <v-alert v-else-if="status?.job?.stage === 'failed'" density="compact" type="error" variant="tonal">
      {{ $t('update.failed') }}<span v-if="status.job.error"> - {{ status.job.error }}</span>
    </v-alert>

    <div class="panel-update__actions">
      <v-btn color="primary" :disabled="!canUpdate" prepend-icon="lucide:arrow-up-circle" @click="openConfirm">
        {{ $t('update.update') }}
      </v-btn>
    </div>

    <v-dialog v-model="confirm" max-width="460">
      <v-card>
        <v-card-title>{{ $t('update.confirmTitle') }}</v-card-title>
        <v-card-text>
          <v-alert class="mb-3" density="compact" type="warning" variant="tonal">{{ $t('update.restartWarning') }}</v-alert>
          <p class="mb-3">{{ $t('update.confirmTo', { version: status?.latest }) }}</p>
          <v-text-field v-model="password" autocomplete="current-password" density="comfortable" :label="$t('update.password')" type="password" variant="outlined" />
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" @click="confirm = false">{{ $t('cancel') }}</v-btn>
          <v-btn color="primary" :disabled="!password" :loading="applying" @click="runUpdate">{{ $t('update.update') }}</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>
  </section>
</template>

<script lang="ts" setup>
import { computed } from 'vue'

import { parseMarkdownBlocks } from '@/plugins/markdown'
import { usePanelUpdate } from '@/shared/composables/usePanelUpdate'

const {
  applying,
  canUpdate,
  channel,
  checkUpdates,
  checking,
  confirm,
  jobActive,
  openConfirm,
  password,
  runUpdate,
  status,
} = usePanelUpdate()

const releaseNoteBlocks = computed(() => parseMarkdownBlocks(status.value?.releaseNotes || ''))
const headingTag = (level = 3) => `h${Math.min(Math.max(level + 2, 4), 6)}`
</script>

<style scoped>
.panel-update { border: 1px solid rgba(var(--v-theme-on-surface), 0.12); border-radius: 8px; display: grid; gap: 14px; min-width: 0; padding: 16px; }
.panel-update__heading { align-items: flex-start; display: flex; gap: 12px; min-width: 0; }
.panel-update__heading h3 { font-size: 1rem; font-weight: 600; margin: 0; }
.panel-update__heading p { color: rgba(var(--v-theme-on-surface), 0.72); font-size: 0.875rem; margin: 2px 0 0; }
.panel-update__row, .panel-update__versions { align-items: center; display: flex; flex-wrap: wrap; gap: 12px; }
.panel-update__row { justify-content: space-between; }
.panel-update__versions { font-size: 0.9rem; }
.panel-update__notes { background: rgba(var(--v-theme-surface-variant), 0.18); max-height: 220px; overflow: auto; padding: 12px; }
.panel-update__notes-title { font-size: 0.8rem; font-weight: 600; margin-bottom: 6px; opacity: 0.8; }
.panel-update__notes-body { font-size: 0.84rem; line-height: 1.5; word-break: break-word; }
.panel-update__notes-heading { font-size: 0.92rem; font-weight: 650; margin: 10px 0 4px; }
.panel-update__notes-heading:first-child,
.panel-update__notes-paragraph:first-child,
.panel-update__notes-list:first-child,
.panel-update__notes-code:first-child { margin-top: 0; }
.panel-update__notes-paragraph { margin: 0 0 8px; }
.panel-update__notes-list { margin: 0 0 8px 18px; padding: 0; }
.panel-update__notes-code { background: rgba(var(--v-theme-on-surface), 0.08); border-radius: 6px; margin: 0 0 8px; overflow: auto; padding: 8px; }
.panel-update__notes-body code { font-family: ui-monospace, SFMono-Regular, Consolas, monospace; font-size: 0.82em; }
.panel-update__progress { display: grid; gap: 6px; }
.panel-update__actions { display: flex; justify-content: flex-end; }
</style>
