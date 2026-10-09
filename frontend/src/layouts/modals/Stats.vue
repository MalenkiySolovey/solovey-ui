<template>
  <v-dialog transition="dialog-bottom-transition" width="800">
    <v-card class="rounded-lg" :loading="loading">
      <v-card-title>
        <v-row>
          <v-col cols="auto">{{ $t('stats.graphTitle') }}</v-col>
          <v-spacer></v-spacer>
          <v-col cols="auto"><v-icon icon="mdi-close" :aria-label="$t('close')" @click="$emit('close')"></v-icon></v-col>
        </v-row>
      </v-card-title>
      <v-divider></v-divider>
      <v-card-text style="padding: 0 16px;">
        <div style="text-align: center; margin: 5px;">{{ $t('objects.' + resource) + ' : ' + tag }}</div>
        <v-row align="center" dense>
          <v-col cols="auto"><v-btn prepend-icon="mdi-refresh" :aria-busy="loading" @click="loadData">{{ $t('stats.refresh') }}</v-btn></v-col>
          <v-col cols="auto"><v-switch v-model="autoRefresh" :label="$t('stats.autoRefresh')" density="compact" hide-details /></v-col>
          <v-col><v-autocomplete v-model="timeZone" :items="zones" :label="$t('stats.timeZone')" density="compact" hide-details /></v-col>
        </v-row>
        <v-radio-group v-model="limit" density="compact" :loading="loading" inline hide-details>
          <v-radio v-for="p in periods" :label="p.title" :value="p.value"></v-radio>
        </v-radio-group>
        <div v-if="loaded" class="text-caption my-2" :title="$t('stats.windowTotalHint')" role="status">
          {{ $t('stats.windowTotal') }}: {{ sizeFormat(total.upload + total.download) }}
          · {{ $t('stats.upload') }}: {{ sizeFormat(total.upload) }}
          · {{ $t('stats.download') }}: {{ sizeFormat(total.download) }}
        </div>
        <v-container id="container" style="height:40vh;">
          <v-skeleton-loader v-if="loading" class="mx-auto border" width="95%" type="image" />
          <template v-else>
            <v-alert v-if="alert" :text="$t('noData')" type="warning" variant="outlined" />
            <Line v-if="loaded" :data="usage" :options="<any>options" />
          </template>
        </v-container>
      </v-card-text>
    </v-card>
  </v-dialog>
</template>

<script lang="ts">
import { defineComponent } from 'vue'
import { dateLocale, i18n } from '@/locales'
import { loadStats } from '@/shared/composables/useOperationsData'
import { trafficLabelFormatter, trafficTimeZoneOptions, useTrafficTimeZone } from '@/shared/composables/trafficTimeZone'
import { HumanReadable } from '@/plugins/utils'
import { Chart as ChartJS, CategoryScale, LinearScale, PointElement, LineElement, Title, Tooltip, Legend, Filler } from 'chart.js'
import { Line } from 'vue-chartjs'
ChartJS.register(CategoryScale, LinearScale, PointElement, LineElement, Title, Tooltip, Legend, Filler)
ChartJS.defaults.font.family = 'Vazirmatn'
const autoRefreshKey = 'sui:stats:auto-refresh'
function savedAutoRefresh(): boolean {
  try { return localStorage.getItem(autoRefreshKey) === 'true' } catch { return false }
}
type ChartWindow = { timestamps: number[], upload: (number | null)[], download: (number | null)[] }

export default defineComponent({
  components: { Line },
  props: { visible: Boolean, resource: { type: String, default: '' }, tag: { type: String, default: '' } },
  emits: ['close'],
  setup() {
    return { ...useTrafficTimeZone(), zones: trafficTimeZoneOptions() }
  },
  data() {
    return {
      loading: false, loaded: false, alert: false, active: false, disposed: false,
      intervalId: undefined as ReturnType<typeof setInterval> | undefined,
      controller: null as AbortController | null,
      requestEpoch: 0,
      autoRefresh: savedAutoRefresh(),
      chart: null as ChartWindow | null,
      limit: 1,
      periods: [1, 6, 12, 24, 48, 240, 480, 720, 1440, 2160].map(hours => ({
        value: hours,
        title: i18n.global.n(hours < 24 ? hours : hours / 24) + i18n.global.t(hours < 24 ? 'date.h' : 'date.d'),
      })),
      options: {
        responsive: true, maintainAspectRatio: false,
        interaction: { intersect: false, mode: 'index' },
        elements: { point: { pointStyle: 'crossRot' } },
        plugins: { tooltip: { callbacks: { footer: (items: any[]) => HumanReadable.sizeFormat(items.reduce((sum, item) => sum + (item.raw ?? 0), 0)) } } },
        scales: { y: { grid: { color: '#777777' }, beginAtZero: true, ticks: { callback: (label: any) => label == 0 ? 0 : HumanReadable.sizeFormat(label, 0), count: 10 } } },
      },
    }
  },
  computed: {
    requestContext(): string { return JSON.stringify([this.visible, this.resource, this.tag, this.limit]) },
    usage(): any {
      const label = trafficLabelFormatter(this.timeZone, dateLocale())
      return {
        labels: this.chart?.timestamps.map(label) ?? [],
        datasets: [
          { label: i18n.global.t('stats.upload'), backgroundColor: 'rgba(255, 165, 0, 0.4)', borderColor: 'rgba(255, 165, 0)', fill: true, data: this.chart?.upload ?? [] },
          { label: i18n.global.t('stats.download'), backgroundColor: 'rgba(0, 128, 0, 0.2)', borderColor: 'rgba(0, 128, 0)', fill: true, data: this.chart?.download ?? [] },
        ],
      }
    },
    total(): { upload: number, download: number } {
      const sum = (values: (number | null)[] | undefined) => values?.reduce<number>((total, value) => total + (value ?? 0), 0) ?? 0
      return { upload: sum(this.chart?.upload), download: sum(this.chart?.download) }
    },
  },
  methods: {
    sizeFormat: HumanReadable.sizeFormat,
    pageVisible(): boolean { return typeof document === 'undefined' || !document.hidden },
    cancelLoad() {
      this.requestEpoch++
      this.controller?.abort()
      this.controller = null
      this.loading = false
    },
    async loadData() {
      if (!this.active || this.disposed || !this.visible || !this.pageVisible()) return
      this.cancelLoad()
      const epoch = this.requestEpoch
      const resource = this.resource, tag = this.tag, hours = this.limit
      const controller = new AbortController()
      this.controller = controller
      this.loading = true
      const end = Date.now() / 1000
      const bucketCount = 360, duration = hours * 3600 / bucketCount, start = end - hours * 3600
      const current = () => epoch === this.requestEpoch && resource === this.resource && tag === this.tag && hours === this.limit && !this.disposed && this.visible && this.pageVisible()
      try {
        const response = await loadStats(resource, tag, hours, controller.signal)
        if (!current()) return
        if (!response.success || !Array.isArray(response.obj)) { this.resetChart(); this.alert = true; return }
        const chart: ChartWindow = {
          timestamps: Array.from({ length: bucketCount }, (_, index) => start + duration * (index + 1)),
          upload: Array<number | null>(bucketCount).fill(null), download: Array<number | null>(bucketCount).fill(null),
        }
        for (const sample of response.obj) {
          if (typeof sample?.direction !== 'boolean' || typeof sample.dateTime !== 'number' || typeof sample.traffic !== 'number') continue
          const timestamp = sample.dateTime, traffic = sample.traffic
          if (!Number.isFinite(timestamp) || !Number.isFinite(traffic) || traffic < 0 || timestamp < start || timestamp > end) continue
          const bucket = Math.min(bucketCount - 1, Math.floor((timestamp - start) / duration))
          const series = sample.direction ? chart.upload : chart.download
          const next = (series[bucket] ?? 0) + traffic
          if (Number.isFinite(next)) series[bucket] = next
        }
        this.chart = chart
        this.loaded = true
        this.alert = false
      } catch {
        if (current()) { this.resetChart(); this.alert = true }
      } finally {
        if (current()) { this.loading = false; this.controller = null }
      }
    },
    startTimer() {
      this.stopTimer()
      if (!this.active || this.disposed || !this.visible || !this.autoRefresh || !this.pageVisible()) return
      this.intervalId = setInterval(() => { if (!this.loading && this.pageVisible()) void this.loadData() }, 10000)
    },
    stopTimer() {
      if (this.intervalId !== undefined) clearInterval(this.intervalId)
      this.intervalId = undefined
    },
    resetChart() { this.loaded = false; this.alert = false; this.chart = null },
    refreshView() {
      this.cancelLoad()
      this.stopTimer()
      this.resetChart()
      if (this.visible && this.pageVisible()) { void this.loadData(); this.startTimer() }
    },
    visibilityChanged() {
      this.cancelLoad()
      this.stopTimer()
      if (this.visible && this.pageVisible()) { void this.loadData(); this.startTimer() }
    },
  },
  watch: {
    requestContext() { if (this.active) this.refreshView() },
    autoRefresh(value: boolean) {
      try { localStorage.setItem(autoRefreshKey, String(value)) } catch { /* Optional preference works without storage. */ }
      this.startTimer()
    },
  },
  mounted() {
    this.active = true
    document.addEventListener('visibilitychange', this.visibilityChanged)
    this.refreshView()
  },
  beforeUnmount() {
    this.disposed = true
    this.active = false
    this.cancelLoad()
    this.stopTimer()
    document.removeEventListener('visibilitychange', this.visibilityChanged)
  },
})
</script>
