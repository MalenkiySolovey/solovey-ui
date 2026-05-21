<template>
  <section class="nexus-overview">
    <kpi-row
      :loading="dashboardLoading"
      :summary="kpiSummary"
      :traffic="trafficSeries"
      :ws-state="ws.state"
    />

    <div class="nexus-overview__primary">
      <traffic-overview
        :loading="trafficLoading"
        :offline="!browserOnline"
        :series="trafficSeries"
        :unavailable="trafficUnavailable"
      />

      <system-status
        :loading="statusLoading"
        :metrics="systemMetrics"
        :offline="!browserOnline"
        :status="systemStatus"
        :unavailable="statusUnavailable"
        :ws-state="ws.state"
      />
    </div>

    <div class="nexus-overview__secondary">
      <top-clients :clients="topClients" :loading="storeLoading" />
      <recent-events
        :events="auditEvents"
        :loading="auditLoading"
        :offline="!browserOnline"
        :unavailable="auditUnavailable"
      />
    </div>

    <protocol-summaries
      :loading="storeLoading"
      :summaries="protocolSummaries"
    />
  </section>
</template>

<script lang="ts" setup>
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'

import KpiRow from '@/components/nexus/overview/KpiRow.vue'
import ProtocolSummaries from '@/components/nexus/overview/ProtocolSummaries.vue'
import RecentEvents from '@/components/nexus/overview/RecentEvents.vue'
import SystemStatus from '@/components/nexus/overview/SystemStatus.vue'
import TopClients from '@/components/nexus/overview/TopClients.vue'
import TrafficOverview from '@/components/nexus/overview/TrafficOverview.vue'
import { mapAuditDisplayItems } from '@/components/nexus/overview/selectors/auditMapper'
import { selectKpiSummary } from '@/components/nexus/overview/selectors/kpiSelectors'
import { selectProtocolSummaries } from '@/components/nexus/overview/selectors/protocolSummarySelectors'
import { selectSystemStatus } from '@/components/nexus/overview/selectors/systemStatusSelectors'
import { selectTopClients } from '@/components/nexus/overview/selectors/topClientsSelectors'
import { selectTrafficSeries } from '@/components/nexus/overview/selectors/trafficSelectors'
import {
  auditEventsFromPayload,
  networkRateFromSamples,
  overviewInboundTags,
  overviewStatusMetrics,
  overviewStatusNetworkSample,
  payloadItems,
  type NetworkTrafficRate,
} from '@/components/nexus/overview/overviewPayloads'
import HttpUtils from '@/plugins/httputil'
import Data from '@/store/modules/data'
import Ws from '@/store/ws'

const data = Data()
const ws = Ws()

const browserOnline = ref(typeof navigator === 'undefined' ? true : navigator.onLine)
const nowSec = ref(Math.floor(Date.now() / 1000))
const statusPayload = ref<unknown>()
const statusLoading = ref(true)
const statusLoaded = ref(false)
const statusUnavailable = ref(false)
const auditEvents = ref(mapAuditDisplayItems())
const auditLoading = ref(true)
const auditUnavailable = ref(false)
const trafficStats = ref<unknown[]>([])
const trafficLoading = ref(false)
const trafficUnavailable = ref(false)
const liveTraffic = ref<NetworkTrafficRate>({
  downloadBps: 0,
  uploadBps: 0,
})

let statusInterval: ReturnType<typeof setInterval> | undefined
let trafficRequestSerial = 0
let statusRequestPending = false
let previousNetworkSample = overviewStatusNetworkSample()

const storeLoading = computed(() => data.lastLoad === 0)
const dashboardLoading = computed(() => storeLoading.value || statusLoading.value)
const systemStatus = computed(() => selectSystemStatus(statusPayload.value, nowSec.value))
const systemMetrics = computed(() => overviewStatusMetrics(statusPayload.value))
const inboundTrafficTags = computed(() => overviewInboundTags(data.inbounds))
const trafficSeries = computed(() => selectTrafficSeries({
  range: '24h',
  stats: trafficStats.value,
}))
const topClients = computed(() => selectTopClients({
  clients: data.clients,
  onlines: data.onlines,
}))
const protocolSummaries = computed(() => selectProtocolSummaries({
  inbounds: data.inbounds,
  onlines: data.onlines,
}))
const kpiHealth = computed(() => {
  if (!browserOnline.value) {
    return {
      online: false,
      singboxRunning: statusLoaded.value ? systemStatus.value.singboxRunning : undefined,
    }
  }

  if (!statusLoaded.value) {
    return {
      online: undefined,
      singboxRunning: undefined,
    }
  }

  return {
    online: !statusUnavailable.value,
    singboxRunning: systemStatus.value.singboxRunning,
  }
})
const kpiSummary = computed(() => selectKpiSummary({
  inbounds: data.inbounds,
  onlines: data.onlines,
  liveTraffic: liveTraffic.value,
  health: kpiHealth.value,
}))

const loadStatus = async () => {
  if (statusRequestPending) return

  if (!browserOnline.value) {
    statusLoading.value = false
    statusUnavailable.value = true
    previousNetworkSample = undefined
    liveTraffic.value = { downloadBps: 0, uploadBps: 0 }
    return
  }

  statusRequestPending = true
  statusLoading.value = !statusLoaded.value
  const msg = await HttpUtils.get('api/status', {
    r: 'sys,sbd,net,cpu,mem,dsk',
  })
  nowSec.value = Math.floor(Date.now() / 1000)

  if (msg.success) {
    statusPayload.value = msg.obj
    statusLoaded.value = true
    statusUnavailable.value = false

    const networkSample = overviewStatusNetworkSample(msg.obj)
    const rate = networkRateFromSamples(previousNetworkSample, networkSample)
    if (rate) {
      liveTraffic.value = rate
    }
    previousNetworkSample = networkSample
  } else {
    statusUnavailable.value = true
    previousNetworkSample = undefined
    liveTraffic.value = { downloadBps: 0, uploadBps: 0 }
  }

  statusLoading.value = false
  statusRequestPending = false
}

const loadAuditEvents = async () => {
  if (!browserOnline.value) {
    auditLoading.value = false
    auditUnavailable.value = true
    return
  }

  auditLoading.value = true
  const msg = await HttpUtils.get('api/security/audit', { limit: 6 })

  if (msg.success) {
    auditEvents.value = mapAuditDisplayItems(auditEventsFromPayload(msg.obj))
    auditUnavailable.value = false
  } else {
    auditEvents.value = []
    auditUnavailable.value = true
  }

  auditLoading.value = false
}

const loadTrafficHistory = async (tags: string[]) => {
  const requestSerial = ++trafficRequestSerial

  if (!browserOnline.value || tags.length === 0) {
    trafficStats.value = []
    trafficLoading.value = false
    trafficUnavailable.value = !browserOnline.value
    return
  }

  trafficLoading.value = true
  const results = await Promise.all(tags.map((tag) => HttpUtils.get('api/stats', {
    limit: 24,
    resource: 'inbound',
    tag,
  })))

  if (requestSerial !== trafficRequestSerial) return

  trafficStats.value = results.flatMap((result) => {
    return result.success ? payloadItems(result.obj) : []
  })
  trafficUnavailable.value = !results.some((result) => result.success)
  trafficLoading.value = false
}

const setOnline = () => {
  browserOnline.value = true
  void loadStatus()
  void loadAuditEvents()
  void loadTrafficHistory(inboundTrafficTags.value)
}

const setOffline = () => {
  browserOnline.value = false
  statusUnavailable.value = true
  auditUnavailable.value = true
  trafficUnavailable.value = true
  previousNetworkSample = undefined
  liveTraffic.value = { downloadBps: 0, uploadBps: 0 }
}

watch(inboundTrafficTags, (tags) => {
  void loadTrafficHistory(tags)
}, { immediate: true })

onMounted(() => {
  if (data.lastLoad === 0) {
    void data.loadData()
  }

  window.addEventListener('online', setOnline)
  window.addEventListener('offline', setOffline)
  void loadStatus()
  void loadAuditEvents()
  statusInterval = setInterval(() => {
    void loadStatus()
  }, 10000)
})

onBeforeUnmount(() => {
  if (statusInterval) clearInterval(statusInterval)
  window.removeEventListener('online', setOnline)
  window.removeEventListener('offline', setOffline)
})
</script>

<style scoped>
.nexus-overview {
  display: grid;
  gap: var(--nexus-gap-4);
  min-width: 0;
}

.nexus-overview__primary,
.nexus-overview__secondary {
  display: grid;
  gap: var(--nexus-gap-4);
  min-width: 0;
}

.nexus-overview__primary {
  grid-template-columns: minmax(0, 2fr) minmax(320px, 1fr);
}

.nexus-overview__secondary {
  grid-template-columns: repeat(2, minmax(0, 1fr));
}

@media (max-width: 1264px) {
  .nexus-overview__primary {
    grid-template-columns: minmax(0, 1.55fr) minmax(280px, 1fr);
  }
}

@media (max-width: 960px) {
  .nexus-overview__primary,
  .nexus-overview__secondary {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
