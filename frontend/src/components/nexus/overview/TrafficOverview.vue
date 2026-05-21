<template>
  <article class="nexus-overview-panel nexus-traffic-overview">
    <panel-header :title="$t('nexus.overview.traffic.title')">
      <template #range-tabs>
        <span class="nexus-traffic-overview__range">
          {{ $t('nexus.overview.traffic.range24h') }}
        </span>
      </template>
      <template #action>
        <status-badge :label="stateLabel" :tone="stateTone" />
      </template>
    </panel-header>

    <div class="nexus-traffic-overview__plot">
      <div v-if="loading" class="nexus-overview-panel__loading">
        {{ $t('nexus.overview.traffic.loading') }}
      </div>

      <div v-else-if="!hasHistory" class="nexus-overview-panel__empty">
        {{ emptyCopy }}
      </div>

      <area-series
        v-else
        :aria-label="$t('nexus.overview.traffic.chartAria')"
        :labels="chartLabels"
        :series="chartSeries"
        :value-formatter="formatOverviewSize"
      />
    </div>
  </article>
</template>

<script lang="ts" setup>
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import AreaSeries from '@/components/nexus/primitives/AreaSeries.vue'
import PanelHeader from '@/components/nexus/primitives/PanelHeader.vue'
import StatusBadge from '@/components/nexus/primitives/StatusBadge.vue'
import { formatOverviewSize } from './overviewFormatters'
import type { TrafficSeries } from './selectors/trafficSelectors'

const props = defineProps<{
  loading: boolean
  offline: boolean
  series: TrafficSeries
  unavailable: boolean
}>()

const { t } = useI18n()

const hasHistory = computed(() => {
  return props.series.labels.length > 0
    && [...props.series.download, ...props.series.upload].some((value) => value > 0)
})

const chartLabels = computed(() => props.series.labels.map((label) => {
  const date = new Date(label)
  if (Number.isNaN(date.getTime())) return label

  return date.toLocaleTimeString([], {
    hour: '2-digit',
    minute: '2-digit',
  })
}))

const chartSeries = computed(() => [
  {
    label: t('stats.download'),
    values: props.series.download,
  },
  {
    label: t('stats.upload'),
    values: props.series.upload,
  },
])

const stateLabel = computed(() => {
  if (props.offline) return t('nexus.status.offline')
  if (props.loading) return t('nexus.status.loading')
  if (props.unavailable) return t('nexus.status.unavailable')
  return hasHistory.value ? t('nexus.status.historyReady') : t('nexus.status.noHistory')
})

const stateTone = computed(() => {
  if (props.offline) return 'error'
  if (props.unavailable) return 'warning'
  return hasHistory.value ? 'success' : 'info'
})

const emptyCopy = computed(() => {
  if (props.offline) return t('nexus.overview.traffic.emptyOffline')
  if (props.unavailable) return t('nexus.overview.traffic.emptyUnavailable')
  return t('nexus.overview.traffic.emptyNoHistory')
})
</script>

<style scoped>
.nexus-overview-panel {
  background: var(--nexus-surface-1);
  border: 1px solid var(--nexus-border);
  border-radius: var(--nexus-radius-lg);
  display: grid;
  gap: var(--nexus-gap-3);
  min-width: 0;
  padding: var(--nexus-gap-4);
}

.nexus-traffic-overview__range {
  background: var(--nexus-surface-2);
  border: 1px solid var(--nexus-border);
  border-radius: var(--nexus-radius-sm);
  color: rgb(var(--v-theme-on-surface) / 68%);
  font-size: 0.74rem;
  font-weight: 600;
  letter-spacing: 0;
  line-height: 1.25;
  padding: 3px 6px;
}

.nexus-traffic-overview__plot {
  min-height: 288px;
  min-width: 0;
}

.nexus-overview-panel__empty,
.nexus-overview-panel__loading {
  align-items: center;
  background: var(--nexus-surface-2);
  border: 1px dashed var(--nexus-border-strong);
  border-radius: var(--nexus-radius-md);
  color: rgb(var(--v-theme-on-surface) / 68%);
  display: grid;
  font-size: 0.86rem;
  letter-spacing: 0;
  line-height: 1.4;
  min-height: 288px;
  overflow-wrap: anywhere;
  padding: var(--nexus-gap-4);
  text-align: center;
}
</style>
