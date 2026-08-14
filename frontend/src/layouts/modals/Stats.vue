<template>
  <v-dialog transition="dialog-bottom-transition" width="800">
    <v-card class="rounded-lg" :loading="loading">
      <v-card-title>
        <v-row>
          <v-col cols="auto">
            {{ $t('stats.graphTitle') }}
          </v-col>
          <v-spacer></v-spacer>
          <v-col cols="auto"><v-icon icon="mdi-close" @click="$emit('close')"></v-icon></v-col>
        </v-row>
      </v-card-title>
      <v-divider></v-divider>
      <v-card-text style="padding: 0 16px;">
        <div style="text-align: center; margin: 5px;">
          {{ $t('objects.' + resource) + " : " + tag }}
        </div>
        <v-radio-group v-model="limit" @change="loadData" density="compact" :loading="loading" inline hide-details>
          <v-radio v-for="p in periods" :label="p.title" :value="p.value"></v-radio>
        </v-radio-group>
          <v-container id="container" style="height:40vh;">
            <v-skeleton-loader
            class="mx-auto border"
            width="95%"
            type="image"
            v-if="loading"
          ></v-skeleton-loader>
          <template v-else>
            <v-alert :text="$t('noData')" type="warning" variant="outlined" v-if="alert"></v-alert>
            <Line v-if="loaded" :data="usage" :options="<any>options" />
          </template>
        </v-container>
      </v-card-text>
    </v-card>
  </v-dialog>
</template>

<script lang="ts">
import { dateLocale, i18n } from '@/locales'
import { loadStats } from '@/shared/composables/useOperationsData'
import { HumanReadable } from '@/plugins/utils'
import {
  Chart as ChartJS,
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  Title,
  Tooltip,
  Legend,
  Filler,
} from 'chart.js'
import { Line } from 'vue-chartjs'
ChartJS.register(
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  Title,
  Tooltip,
  Legend,
  Filler
)
ChartJS.defaults.font.family = 'Vazirmatn'
export default {
  components: {
    Line
  },
  props: ['visible','resource','tag'],
  data() {
    return {
      loading: false,
      loaded: false,
      alert: false,
      intervalId: <any>0,
      limit: 1,
      periods: [
        { value: 1, title: i18n.global.n(1) + i18n.global.t('date.h')},
        { value: 6, title: i18n.global.n(6) + i18n.global.t('date.h')},
        { value: 12, title: i18n.global.n(12) + i18n.global.t('date.h')},
        { value: 24, title: i18n.global.n(1) + i18n.global.t('date.d')},
        { value: 48, title: i18n.global.n(2) + i18n.global.t('date.d')},
        { value: 240, title: i18n.global.n(10) + i18n.global.t('date.d')},
        { value: 480, title: i18n.global.n(20) + i18n.global.t('date.d')},
        { value: 720, title: i18n.global.n(30) + i18n.global.t('date.d')},
        { value: 1440, title: i18n.global.n(60) + i18n.global.t('date.d')},
        { value: 2160, title: i18n.global.n(90) + i18n.global.t('date.d')},
      ],
      options: {
        responsive: true,
        maintainAspectRatio: false,
        interaction: {
          intersect: false,
          mode: 'index',
        },
        elements: {
          point: { pointStyle: 'crossRot' }
        },
        plugins: {
          tooltip: {
            callbacks: {
              text: (ctx:any) => {
                const {axis = 'xy', intersect, mode} = ctx.chart.options.interaction
                return 'Mode: ' + mode + ', axis: ' + axis + ', intersect: ' + intersect
              },
              footer: (items:any[]) => {
                return HumanReadable.sizeFormat(items.reduce((acc, c) => acc + c.raw, 0))
              }
            }
          }
        },
        scales: {
          y: {
            grid: {
              color: '#777777',
            },
            beginAtZero: true,
            ticks: {
              callback: function(label:any, index: number) {
                return label == 0 ? 0 : HumanReadable.sizeFormat(label,0)
              },
              count: 10
            }
          }
        }
      },
      usage: <any>{},
    }
  },
  methods: {
    async loadData() {
      if (this.loading) return
      this.loading = true
      try {
        const data = await loadStats(this.resource, this.tag, this.limit)
        if (data.success && Array.isArray(data.obj)) {
          const locale = dateLocale()
          const bucketCount = 360
          const bucketDuration = this.limit * 3600 * 1000 / bucketCount
          const now = Date.now()
          const start = now - bucketDuration * bucketCount
          const labels = Array.from({ length: bucketCount }, (_, index) =>
            this.genLable(start + bucketDuration * (index + 1), locale))
          const uplinkData = Array<number | null>(bucketCount).fill(null)
          const downlinkData = Array<number | null>(bucketCount).fill(null)

          for (const sample of data.obj) {
            const timestamp = Number(sample?.dateTime) * 1000
            const traffic = Number(sample?.traffic)
            if (!Number.isFinite(timestamp) || !Number.isFinite(traffic)) continue
            const bucket = Math.floor((timestamp - start) / bucketDuration)
            if (bucket < 0 || bucket >= bucketCount) continue
            const series = sample?.direction ? uplinkData : downlinkData
            series[bucket] = (series[bucket] ?? 0) + traffic
          }

          this.usage = {
            labels,
            datasets: [
              {
                label: i18n.global.t('stats.upload'),
                backgroundColor: 'rgba(255, 165, 0, 0.4)',
                borderColor: 'rgba(255, 165, 0)',
                fill: true,
                data: uplinkData
              },
              {
                label: i18n.global.t('stats.download'),
                backgroundColor: 'rgba(0, 128, 0, 0.2)',
                borderColor: 'rgba(0, 128, 0)',
                fill: true,
                data: downlinkData
              }
            ],
          }
          this.loaded = true
          this.alert = false
        } else {
          this.alert = true
          this.loaded = false
        }
      } finally {
        this.loading = false
      }
    },
    genLable(step:number, locale: string) {
      return new Date(step).toLocaleString(locale,{
        month: '2-digit',
        day: '2-digit',
        hour: '2-digit',
        minute: '2-digit',
        hour12: false,
      })
    },
    startTimer() {
      this.stopTimer()
      this.intervalId = setInterval(() => {
        if (document.hidden) return
        void this.loadData()
      }, 10000)
    },
    stopTimer() {
      if (this.intervalId && this.intervalId != 0) clearInterval(this.intervalId)
      this.intervalId = 0
    },
    resetChart() {
      this.loaded = false
      this.alert = false
      this.usage = {}
    },
  },
  watch: {
    visible(v) {
      if (v) {
        this.limit = 1
        void this.loadData()
        this.startTimer()
      } else {
        this.stopTimer()
        this.resetChart()
      }
    }
  },
  beforeUnmount() {
    this.stopTimer()
  }
}
</script>
