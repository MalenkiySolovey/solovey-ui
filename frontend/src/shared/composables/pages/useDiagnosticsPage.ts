import { computed, onMounted, ref } from 'vue'
import { push } from 'notivue'
import HttpUtils from '@/plugins/httputil'
import { i18n } from '@/locales'
import { useUiMode } from '@/uiMode/useUiMode'

export const useDiagnosticsPage = () => {
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

  return {
    categoryCounts,
    categoryLabel,
    categoryOptions,
    checks,
    copyReport,
    counts,
    downloadBundle,
    error,
    generatedAt,
    healthColor,
    healthLabel,
    levelOptions,
    loadLogs,
    loading,
    logEntries,
    logFilters,
    logLevelColor,
    logsLoading,
    nexus,
    refreshAll,
    report,
    sections,
    sourceOptions,
    statusColor,
    statusLabel,
  }
}
