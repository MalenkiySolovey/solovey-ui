<template>
  <page-header v-if="nexus" :title="$t('pages.diagnostics')" />

  <page-toolbar v-if="nexus">
    <template #actions>
      <v-btn prepend-icon="lucide:download" variant="tonal" @click="downloadBundle">
        {{ $t('diagnostics.exportBundle') }}
      </v-btn>
      <v-btn prepend-icon="lucide:copy" variant="tonal" @click="copyReport" :disabled="!report">
        {{ $t('copyToClipboard') }}
      </v-btn>
      <v-btn :loading="loading || logsLoading" prepend-icon="lucide:rotate-cw" variant="text" @click="refreshAll">
        {{ $t('actions.refresh') }}
      </v-btn>
    </template>
  </page-toolbar>

  <v-card :flat="nexus" class="diagnostics">
    <template v-if="!nexus">
      <v-card-title>{{ $t('pages.diagnostics') }}</v-card-title>
      <v-divider />
    </template>

    <v-card-text>
      <v-row v-if="!nexus" class="mb-2" justify="end">
        <v-col cols="auto">
          <v-btn prepend-icon="mdi-download" variant="tonal" @click="downloadBundle">
            {{ $t('diagnostics.exportBundle') }}
          </v-btn>
        </v-col>
        <v-col cols="auto">
          <v-btn prepend-icon="mdi-content-copy" variant="tonal" @click="copyReport" :disabled="!report">
            {{ $t('copyToClipboard') }}
          </v-btn>
        </v-col>
        <v-col cols="auto">
          <v-btn :loading="loading || logsLoading" prepend-icon="mdi-refresh" variant="text" @click="refreshAll">
            {{ $t('actions.refresh') }}
          </v-btn>
        </v-col>
      </v-row>

      <v-alert v-if="error" class="mb-4" density="compact" type="error" variant="tonal">
        {{ error }}
      </v-alert>

      <v-row class="mb-4" dense>
        <v-col cols="12" md="4">
          <v-sheet border rounded class="diagnostics__summary">
            <div class="diagnostics__summary-label">{{ $t('diagnostics.health') }}</div>
            <v-chip :color="healthColor" size="small" variant="elevated">
              {{ healthLabel }}
            </v-chip>
          </v-sheet>
        </v-col>
        <v-col cols="12" md="4">
          <v-sheet border rounded class="diagnostics__summary">
            <div class="diagnostics__summary-label">{{ $t('diagnostics.generatedAt') }}</div>
            <strong>{{ generatedAt }}</strong>
          </v-sheet>
        </v-col>
        <v-col cols="12" md="4">
          <v-sheet border rounded class="diagnostics__summary diagnostics__counts">
            <v-chip color="success" size="small" variant="tonal">{{ $t('diagnostics.ok') }} {{ counts.ok }}</v-chip>
            <v-chip color="warning" size="small" variant="tonal">{{ $t('diagnostics.warn') }} {{ counts.warn }}</v-chip>
            <v-chip color="error" size="small" variant="tonal">{{ $t('diagnostics.fail') }} {{ counts.fail }}</v-chip>
          </v-sheet>
        </v-col>
      </v-row>

      <div class="diagnostics__section-title">{{ $t('diagnostics.checks') }}</div>
      <v-table density="compact" class="diagnostics__table">
        <thead>
          <tr>
            <th>{{ $t('status') }}</th>
            <th>{{ $t('type') }}</th>
            <th>{{ $t('diagnostics.message') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="check in checks" :key="check.key">
            <td>
              <v-chip :color="statusColor(check.status)" size="small" variant="tonal">
                {{ statusLabel(check.status) }}
              </v-chip>
            </td>
            <td>
              <strong>{{ check.title }}</strong>
              <div v-if="check.details" class="diagnostics__details">
                {{ JSON.stringify(check.details) }}
              </div>
            </td>
            <td>{{ check.message }}</td>
          </tr>
        </tbody>
      </v-table>

      <div class="diagnostics__section-title mt-6">{{ $t('diagnostics.logInspector') }}</div>
      <v-row dense>
        <v-col cols="12" sm="6" md="2">
          <v-select
            v-model="logFilters.level"
            density="compact"
            hide-details
            item-title="title"
            item-value="value"
            :items="levelOptions"
            :label="$t('diagnostics.level')"
            variant="outlined"
          />
        </v-col>
        <v-col cols="12" sm="6" md="2">
          <v-select
            v-model="logFilters.source"
            density="compact"
            hide-details
            item-title="title"
            item-value="value"
            :items="sourceOptions"
            :label="$t('diagnostics.source')"
            variant="outlined"
          />
        </v-col>
        <v-col cols="12" sm="6" md="3">
          <v-select
            v-model="logFilters.category"
            density="compact"
            hide-details
            item-title="title"
            item-value="value"
            :items="categoryOptions"
            :label="$t('diagnostics.category')"
            variant="outlined"
          />
        </v-col>
        <v-col cols="12" sm="6" md="3">
          <v-text-field
            v-model.trim="logFilters.filter"
            density="compact"
            hide-details
            :label="$t('diagnostics.search')"
            maxlength="64"
            variant="outlined"
            @keyup.enter="loadLogs"
          />
        </v-col>
        <v-col cols="6" sm="4" md="1">
          <v-text-field
            v-model.number="logFilters.count"
            density="compact"
            hide-details
            :label="$t('diagnostics.count')"
            max="500"
            min="1"
            type="number"
            variant="outlined"
            @keyup.enter="loadLogs"
          />
        </v-col>
        <v-col cols="6" sm="4" md="1">
          <v-btn block :loading="logsLoading" variant="tonal" @click="loadLogs">
            {{ $t('actions.refresh') }}
          </v-btn>
        </v-col>
      </v-row>

      <div v-if="categoryCounts.length" class="diagnostics__chips">
        <v-chip
          v-for="[category, count] in categoryCounts"
          :key="category"
          size="small"
          variant="tonal"
        >
          {{ categoryLabel(category) }} {{ count }}
        </v-chip>
      </div>

      <v-table density="compact" class="diagnostics__table diagnostics__logs">
        <thead>
          <tr>
            <th>{{ $t('diagnostics.time') }}</th>
            <th>{{ $t('diagnostics.level') }}</th>
            <th>{{ $t('diagnostics.category') }}</th>
            <th>{{ $t('diagnostics.message') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="(entry, index) in logEntries" :key="entry.timestamp + '-' + entry.source + '-' + index">
            <td class="diagnostics__time">{{ entry.time }}</td>
            <td>
              <v-chip :color="logLevelColor(entry.level)" size="small" variant="tonal">
                {{ entry.level }}
              </v-chip>
            </td>
            <td>
              <strong>{{ categoryLabel(entry.category) }}</strong>
              <div class="diagnostics__details">{{ entry.source }}</div>
            </td>
            <td class="diagnostics__message-cell">
              <div class="diagnostics__log-message">{{ entry.message }}</div>
              <div v-if="entry.hint" class="diagnostics__hint">{{ entry.hint }}</div>
              <div v-if="entry.signals?.length" class="diagnostics__chips diagnostics__signals">
                <v-chip v-for="signal in entry.signals" :key="signal" size="x-small" variant="tonal">
                  {{ signal }}
                </v-chip>
              </div>
            </td>
          </tr>
          <tr v-if="!logsLoading && logEntries.length === 0">
            <td colspan="4" class="diagnostics__empty">{{ $t('diagnostics.emptyLogs') }}</td>
          </tr>
        </tbody>
      </v-table>

      <div class="diagnostics__section-title mt-6">{{ $t('diagnostics.rawReport') }}</div>
      <v-expansion-panels class="mt-2" variant="accordion">
        <v-expansion-panel
          v-for="section in sections"
          :key="section.key"
          :title="section.title"
        >
          <v-expansion-panel-text>
            <v-textarea
              :model-value="section.value"
              class="diagnostics__textarea"
              hide-details
              no-resize
              readonly
              rows="10"
              spellcheck="false"
              variant="outlined"
            />
          </v-expansion-panel-text>
        </v-expansion-panel>
      </v-expansion-panels>
    </v-card-text>
  </v-card>
</template>

<script lang="ts" setup>
import { computed, onMounted, ref } from 'vue'
import { push } from 'notivue'

import PageHeader from '@/components/nexus/primitives/PageHeader.vue'
import PageToolbar from '@/components/nexus/primitives/PageToolbar.vue'
import HttpUtils from '@/plugins/httputil'
import { i18n } from '@/locales'
import { useUiMode } from '@/uiMode/useUiMode'

type DiagnosticStatus = 'ok' | 'warn' | 'fail'

interface DiagnosticCheck {
  key: string
  title: string
  status: DiagnosticStatus
  message: string
  details?: Record<string, unknown>
}

interface LogEntry {
  time: string
  timestamp: number
  level: string
  source: string
  category: string
  message: string
  hint?: string
  signals?: string[]
}

interface DiagnosticsReport {
  generatedAt: number
  health?: {
    status?: string
    counts?: Record<string, number>
  }
  checks?: DiagnosticCheck[]
  system?: Record<string, unknown>
  database?: Record<string, unknown>
  settings?: Record<string, unknown>
  logs?: Record<string, unknown>
  logInsights?: {
    total?: number
    byLevel?: Record<string, number>
    byCategory?: Record<string, number>
  }
}

interface SelectOption {
  title: string
  value: string
}

const { mode } = useUiMode()
const nexus = computed(() => mode.value === 'nexus')
const loading = ref(false)
const logsLoading = ref(false)
const error = ref('')
const report = ref<DiagnosticsReport | null>(null)
const logEntries = ref<LogEntry[]>([])
const logFilters = ref({
  level: 'debug',
  source: '',
  category: '',
  filter: '',
  count: 100,
})

const checks = computed(() => report.value?.checks ?? [])
const counts = computed(() => ({
  ok: report.value?.health?.counts?.ok ?? 0,
  warn: report.value?.health?.counts?.warn ?? 0,
  fail: report.value?.health?.counts?.fail ?? 0,
}))
const healthLabel = computed(() => {
  const status = report.value?.health?.status ?? 'unknown'
  return i18n.global.t(`diagnostics.${status}`)
})
const healthColor = computed(() => {
  switch (report.value?.health?.status) {
    case 'healthy': return 'success'
    case 'degraded': return 'warning'
    case 'down': return 'error'
    default: return 'default'
  }
})
const generatedAt = computed(() => {
  const value = report.value?.generatedAt
  return value ? new Date(value * 1000).toLocaleString() : '-'
})
const sections = computed(() => [
  { key: 'system', title: i18n.global.t('diagnostics.system'), value: stringifySection(report.value?.system) },
  { key: 'database', title: i18n.global.t('diagnostics.database'), value: stringifySection(report.value?.database) },
  { key: 'settings', title: i18n.global.t('diagnostics.settings'), value: stringifySection(report.value?.settings) },
  { key: 'logs', title: i18n.global.t('diagnostics.logs'), value: stringifySection(report.value?.logs) },
  { key: 'logInsights', title: i18n.global.t('diagnostics.logInsights'), value: stringifySection(report.value?.logInsights) },
])
const categoryCounts = computed(() => {
  const countsByCategory = logEntries.value.reduce<Record<string, number>>((acc, entry) => {
    acc[entry.category] = (acc[entry.category] ?? 0) + 1
    return acc
  }, {})
  return Object.entries(countsByCategory).sort((a, b) => b[1] - a[1])
})

const option = (title: string, value: string): SelectOption => ({ title, value })
const levelOptions = computed(() => [
  option('DEBUG', 'debug'),
  option('INFO', 'info'),
  option('WARNING', 'warning'),
  option('ERROR', 'error'),
])
const sourceOptions = computed(() => [
  option(i18n.global.t('diagnostics.all'), ''),
  option('panel', 'panel'),
  option('core', 'core'),
])
const categoryOptions = computed(() => [
  option(i18n.global.t('diagnostics.all'), ''),
  ...['core', 'panel', 'auth', 'subscription', 'config', 'database', 'telegram', 'network', 'audit', 'stats', 'backup', 'import', 'api']
    .map((category) => option(categoryLabel(category), category)),
])

const stringifySection = (value: unknown): string => JSON.stringify(value ?? {}, null, 2)

const statusColor = (status: DiagnosticStatus): string => {
  switch (status) {
    case 'ok': return 'success'
    case 'warn': return 'warning'
    case 'fail': return 'error'
    default: return 'default'
  }
}

const statusLabel = (status: DiagnosticStatus): string => {
  switch (status) {
    case 'ok': return i18n.global.t('diagnostics.ok')
    case 'warn': return i18n.global.t('diagnostics.warn')
    case 'fail': return i18n.global.t('diagnostics.fail')
    default: return status
  }
}

const logLevelColor = (level: string): string => {
  switch (level) {
    case 'error': return 'error'
    case 'warning': return 'warning'
    case 'info': return 'info'
    default: return 'default'
  }
}

const categoryLabel = (category: string): string => {
  const translated = i18n.global.t(`diagnostics.categories.${category}`)
  return translated === `diagnostics.categories.${category}` ? category : translated
}

const loadReport = async () => {
  loading.value = true
  error.value = ''
  try {
    const msg = await HttpUtils.get('api/diagnostics/report')
    if (msg.success) {
      report.value = msg.obj as DiagnosticsReport
    } else if (msg.msg) {
      error.value = msg.msg
    }
  } finally {
    loading.value = false
  }
}

const loadLogs = async () => {
  logsLoading.value = true
  try {
    const count = Math.max(1, Math.min(500, Number(logFilters.value.count) || 100))
    logFilters.value.count = count
    const msg = await HttpUtils.get('api/logs/entries', {
      count,
      level: logFilters.value.level,
      source: logFilters.value.source,
      category: logFilters.value.category,
      filter: logFilters.value.filter,
    })
    if (msg.success) {
      logEntries.value = msg.obj as LogEntry[]
    } else if (msg.msg) {
      error.value = msg.msg
    }
  } finally {
    logsLoading.value = false
  }
}

const refreshAll = async () => {
  await Promise.all([loadReport(), loadLogs()])
}

const copyReport = async () => {
  if (!report.value) return
  try {
    await navigator.clipboard.writeText(JSON.stringify(report.value, null, 2))
    push.success({
      message: i18n.global.t('success') + ': ' + i18n.global.t('copyToClipboard'),
      duration: 5000,
    })
  } catch {
    push.error({
      message: i18n.global.t('failed') + ': ' + i18n.global.t('copyToClipboard'),
      duration: 5000,
    })
  }
}

const downloadBundle = async () => {
  const msg = await HttpUtils.get('api/diagnostics/bundle')
  if (!msg.success) {
    if (msg.msg) error.value = msg.msg
    return
  }
  downloadJson(`solovey-ui-diagnostics-${Date.now()}.json`, msg.obj)
}

const downloadJson = (filename: string, value: unknown) => {
  const blob = new Blob([JSON.stringify(value, null, 2)], { type: 'application/json' })
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = filename
  document.body.appendChild(link)
  link.click()
  link.remove()
  URL.revokeObjectURL(url)
}

onMounted(refreshAll)
</script>

<style scoped>
.diagnostics {
  color: rgb(var(--v-theme-on-surface));
}

.diagnostics :deep(.v-card-text),
.diagnostics :deep(.v-expansion-panel-title),
.diagnostics :deep(.v-expansion-panel-text),
.diagnostics :deep(.v-field),
.diagnostics :deep(.v-field__input),
.diagnostics :deep(.v-label),
.diagnostics :deep(.v-select__selection),
.diagnostics :deep(.v-select__selection-text),
.diagnostics :deep(textarea),
.diagnostics :deep(pre),
.diagnostics :deep(code) {
  color: rgb(var(--v-theme-on-surface));
}

.diagnostics__summary {
  align-items: center;
  display: flex;
  gap: 10px;
  justify-content: space-between;
  min-height: 56px;
  padding: 12px 14px;
}

.diagnostics__summary-label {
  color: rgba(var(--v-theme-on-surface), .72);
  font-size: 0.78rem;
  font-weight: 600;
  text-transform: uppercase;
}

.diagnostics__counts,
.diagnostics__chips {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  justify-content: flex-start;
}

.diagnostics__section-title {
  color: rgb(var(--v-theme-on-surface));
  font-size: 0.95rem;
  font-weight: 700;
  margin-bottom: 10px;
}

.diagnostics__table {
  border: 1px solid rgba(var(--v-border-color), var(--v-border-opacity));
  border-radius: 6px;
  color: rgb(var(--v-theme-on-surface));
}

.diagnostics__table :deep(td),
.diagnostics__table :deep(th) {
  color: rgb(var(--v-theme-on-surface));
}

.diagnostics__logs {
  margin-top: 12px;
}

.diagnostics__details,
.diagnostics__hint {
  color: rgba(var(--v-theme-on-surface), .72);
  font-size: 0.76rem;
  margin-top: 2px;
  overflow-wrap: anywhere;
}

.diagnostics__time {
  white-space: nowrap;
}

.diagnostics__message-cell {
  min-width: 280px;
}

.diagnostics__log-message,
.diagnostics__textarea :deep(textarea),
.diagnostics__details {
  font-family: var(--nexus-font-mono, "Cascadia Mono", Consolas, "Courier New", monospace);
}

.diagnostics__log-message {
  font-size: 0.82rem;
  line-height: 1.45;
  overflow-wrap: anywhere;
  user-select: text;
}

.diagnostics__signals {
  margin-top: 6px;
}

.diagnostics__empty {
  color: rgba(var(--v-theme-on-surface), .72);
  padding: 18px;
  text-align: center;
}

.diagnostics__textarea :deep(textarea) {
  color: rgb(var(--v-theme-on-surface));
  font-size: 0.82rem;
  line-height: 1.45;
}
</style>
