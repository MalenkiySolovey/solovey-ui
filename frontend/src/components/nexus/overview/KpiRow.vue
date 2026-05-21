<template>
  <div class="nexus-overview-kpis">
    <kpi-card
      :delta="$t('nexus.overview.kpi.liveTrafficDelta')"
      :label="$t('nexus.overview.kpi.liveTraffic')"
      :value="loading ? '-' : formatOverviewRate(summary.liveTrafficBps)"
    >
      <template #trend>
        <area-spark
          :aria-label="$t('nexus.overview.kpi.trafficTrend')"
          :labels="traffic.labels"
          :values="trafficTrend"
        />
      </template>
    </kpi-card>

    <kpi-card
      :delta="wsStateLabel"
      :label="$t('nexus.overview.kpi.onlineClients')"
      :value="formatOverviewCount(summary.onlineClients)"
    >
      <template #trend>
        <div class="nexus-overview-kpis__signal">
          {{ $t('nexus.overview.kpi.clientSignal') }}
        </div>
      </template>
    </kpi-card>

    <kpi-card
      :delta="$t('nexus.overview.kpi.activeInbounds', { count: formatOverviewCount(summary.activeInbounds) })"
      :label="$t('nexus.overview.kpi.enabledInbounds')"
      :value="formatOverviewCount(summary.totalInbounds)"
    >
      <template #trend>
        <div class="nexus-overview-kpis__signal">
          {{ $t('nexus.overview.kpi.inboundOnlineTags', { count: formatOverviewCount(summary.activeInbounds) }) }}
        </div>
      </template>
    </kpi-card>

    <kpi-card :label="$t('nexus.overview.kpi.health')" :value="healthLabel">
      <template #trend>
        <div class="nexus-overview-kpis__health">
          <status-badge :label="healthLabel" :tone="healthTone" />
          <span>{{ healthDetail }}</span>
        </div>
      </template>
    </kpi-card>
  </div>
</template>

<script lang="ts" setup>
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import AreaSpark from '@/components/nexus/primitives/AreaSpark.vue'
import KpiCard from '@/components/nexus/primitives/KpiCard.vue'
import StatusBadge from '@/components/nexus/primitives/StatusBadge.vue'
import type { WsConnectionState } from '@/store/ws'
import {
  formatOverviewCount,
  formatOverviewRate,
} from './overviewFormatters'
import type { KpiSummary } from './selectors/kpiSelectors'
import type { TrafficSeries } from './selectors/trafficSelectors'

const props = defineProps<{
  loading: boolean
  summary: KpiSummary
  traffic: TrafficSeries
  wsState: WsConnectionState
}>()

const { t } = useI18n()

const trafficTrend = computed(() => {
  return props.traffic.download.map((download, index) => {
    return download + (props.traffic.upload[index] ?? 0)
  })
})

const healthLabel = computed(() => {
  if (props.summary.health === 'healthy') return t('nexus.status.healthy')
  if (props.summary.health === 'down') return t('nexus.status.down')
  return t('nexus.status.degraded')
})

const healthTone = computed(() => {
  if (props.summary.health === 'healthy') return 'success'
  if (props.summary.health === 'down') return 'error'
  return 'warning'
})

const healthDetail = computed(() => {
  if (props.loading) return t('nexus.overview.kpi.healthWaiting')
  if (props.summary.health === 'healthy') return t('nexus.overview.kpi.healthHealthy')
  if (props.summary.health === 'down') return t('nexus.overview.kpi.healthDown')
  return t('nexus.overview.kpi.healthDegraded')
})

const wsStateLabel = computed(() => {
  if (props.wsState === 'connected') return t('nexus.status.realtime')
  if (props.wsState === 'reconnecting') return t('nexus.status.reconnecting')
  return t('nexus.status.pollFallback')
})
</script>

<style scoped>
.nexus-overview-kpis {
  display: grid;
  gap: var(--nexus-gap-4);
  grid-template-columns: repeat(4, minmax(0, 1fr));
  min-width: 0;
}

.nexus-overview-kpis__health,
.nexus-overview-kpis__signal {
  color: rgb(var(--v-theme-on-surface) / 68%);
  font-size: 0.78rem;
  letter-spacing: 0;
  line-height: 1.35;
  min-width: 0;
  overflow-wrap: anywhere;
}

.nexus-overview-kpis__health {
  align-items: center;
  display: flex;
  flex-wrap: wrap;
  gap: var(--nexus-gap-2);
}

@media (max-width: 1264px) {
  .nexus-overview-kpis {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (max-width: 600px) {
  .nexus-overview-kpis {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
