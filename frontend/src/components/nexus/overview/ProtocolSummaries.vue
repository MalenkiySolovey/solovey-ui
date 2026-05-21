<template>
  <article class="nexus-protocol-summaries">
    <panel-header :title="$t('nexus.overview.protocols.title')">
      <template #action>
        <span class="nexus-protocol-summaries__count">
          {{ $t('nexus.overview.protocols.groups', { count: summaries.length }) }}
        </span>
      </template>
    </panel-header>

    <div v-if="loading" class="nexus-protocol-summaries__state">
      {{ $t('nexus.overview.protocols.loading') }}
    </div>

    <div v-else-if="summaries.length === 0" class="nexus-protocol-summaries__state">
      {{ $t('nexus.overview.protocols.empty') }}
    </div>

    <div v-else class="nexus-protocol-summaries__grid">
      <section
        v-for="summary in summaries"
        :key="summary.type"
        class="nexus-protocol-summaries__item"
      >
        <protocol-card
          :conn-count="summary.activeInbounds"
          :status="summary.activeInbounds > 0 ? $t('nexus.status.online') : $t('nexus.status.idle')"
          :status-tone="summary.activeInbounds > 0 ? 'success' : 'info'"
          :totals="$t('nexus.overview.protocols.inboundTags', { count: summary.totalInbounds })"
          :type="summary.type"
        />

        <div class="nexus-protocol-summaries__tags">
          <span v-if="summary.tags.length === 0">
            {{ $t('nexus.overview.protocols.noTag') }}
          </span>
          <span v-for="tag in summary.tags" :key="tag">{{ tag }}</span>
        </div>
      </section>
    </div>
  </article>
</template>

<script lang="ts" setup>
import PanelHeader from '@/components/nexus/primitives/PanelHeader.vue'
import ProtocolCard from '@/components/nexus/primitives/ProtocolCard.vue'
import type { ProtocolSummary } from './selectors/protocolSummarySelectors'

defineProps<{
  loading: boolean
  summaries: ProtocolSummary[]
}>()
</script>

<style scoped>
.nexus-protocol-summaries {
  background: var(--nexus-surface-1);
  border: 1px solid var(--nexus-border);
  border-radius: var(--nexus-radius-lg);
  display: grid;
  gap: var(--nexus-gap-3);
  min-width: 0;
  padding: var(--nexus-gap-4);
}

.nexus-protocol-summaries__count {
  color: rgb(var(--v-theme-on-surface) / 68%);
  font-size: 0.76rem;
  letter-spacing: 0;
}

.nexus-protocol-summaries__grid {
  display: grid;
  gap: var(--nexus-gap-3);
  grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
  min-width: 0;
}

.nexus-protocol-summaries__item {
  display: grid;
  gap: var(--nexus-gap-2);
  min-width: 0;
}

.nexus-protocol-summaries__tags {
  display: flex;
  flex-wrap: wrap;
  gap: var(--nexus-gap-1);
  min-width: 0;
}

.nexus-protocol-summaries__tags span {
  background: var(--nexus-surface-2);
  border: 1px solid var(--nexus-border);
  border-radius: var(--nexus-radius-sm);
  color: rgb(var(--v-theme-on-surface) / 68%);
  font-size: 0.72rem;
  letter-spacing: 0;
  line-height: 1.3;
  max-width: 100%;
  overflow-wrap: anywhere;
  padding: 3px 6px;
}

.nexus-protocol-summaries__state {
  align-items: center;
  background: var(--nexus-surface-2);
  border: 1px dashed var(--nexus-border-strong);
  border-radius: var(--nexus-radius-md);
  color: rgb(var(--v-theme-on-surface) / 68%);
  display: grid;
  font-size: 0.86rem;
  letter-spacing: 0;
  line-height: 1.4;
  min-height: 112px;
  overflow-wrap: anywhere;
  padding: var(--nexus-gap-4);
  text-align: center;
}
</style>
