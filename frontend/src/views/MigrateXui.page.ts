import Data from '@/store/modules/data'
import Ws from '@/store/ws'
import { defineComponent } from 'vue'
import { applyXuiMigration, planXuiMigration, rollbackXuiMigration } from '@/shared/composables/useXuiMigrationOperations'
import api from '@/plugins/api'
import MigrationResultStep from '@/components/migration/MigrationResultStep.vue'

type PlanItem = {
  rowKey?: string
  kind: string
  srcId: any
  srcTag?: string
  dstTag: string
  action: string
  conflict: boolean
  previewJson: any
  warnings?: string[]
}

type MigrationPlan = {
  items: PlanItem[]
  source: { hash: string }
  defaults: Record<string, any>
}

const generatedAdminsAutoClearMs = 5 * 60 * 1000

export default defineComponent({
  components: { MigrationResultStep },
  data() {
    return {
      step: 1,
      maxStep: 1,
      file: null as File | File[] | null,
      strategy: 'merge',
      // Default the optional categories ON so a migration carries over routes,
      // panel settings and traffic history automatically. Admin import stays
      // opt-in (skip) because it creates/overwrites credentials.
      includeSettings: true,
      includeHistory: true,
      includeRouting: true,
      adminMode: 'skip',
      loading: false,
      rollbackLoading: false,
      kindFilter: 'all',
      search: '',
      plan: null as MigrationPlan | null,
      report: null as any,
      progress: null as any,
      applyError: '',
      rollbackError: '',
      generatedAdminsRevealed: false,
      generatedAdminsClearTimer: undefined as ReturnType<typeof setTimeout> | undefined,
    }
  },
  computed: {
    selectedFile(): File | null {
      if (Array.isArray(this.file)) return this.file[0] ?? null
      return this.file
    },
    stepItems(): any[] {
      return [
        { value: 1, title: this.$t('migrateXui.steps.upload'), icon: 'mdi-upload' },
        { value: 2, title: this.$t('migrateXui.steps.review'), icon: 'mdi-format-list-checks' },
        { value: 3, title: this.$t('migrateXui.steps.progress'), icon: 'mdi-progress-clock' },
        { value: 4, title: this.$t('migrateXui.steps.result'), icon: 'mdi-check-circle' },
      ]
    },
    strategyItems(): any[] {
      return ['merge', 'replace', 'skip'].map(value => ({ value, title: this.$t(`migrateXui.actions.${value}`) }))
    },
    actionItems(): any[] {
      return ['create', 'merge', 'replace', 'skip'].map(value => ({ value, title: this.$t(`migrateXui.actions.${value}`) }))
    },
    adminModeItems(): any[] {
      return ['skip', 'new_password', 'reset_required'].map(value => ({ value, title: this.$t(`migrateXui.adminModes.${value}`) }))
    },
    kindFilterItems(): any[] {
      const kinds = ['tls', 'inbound', 'endpoint', 'client', 'setting', 'admin', 'historical', 'routing']
      return [
        { value: 'all', title: this.$t('migrateXui.allKinds') },
        ...kinds.map(value => ({ value, title: this.kindTitle(value) })),
      ]
    },
    headers(): any[] {
      return [
        { title: this.$t('migrateXui.import'), key: 'import', sortable: false, width: 72 },
        { title: this.$t('migrateXui.kind'), key: 'kind', width: 120 },
        { title: this.$t('migrateXui.source'), key: 'srcTag' },
        { title: this.$t('migrateXui.destination'), key: 'dstTag', sortable: false },
        { title: this.$t('migrateXui.action'), key: 'action', sortable: false, width: 180 },
        { title: this.$t('migrateXui.conflict'), key: 'conflict', width: 120 },
      ]
    },
    filteredItems(): PlanItem[] {
      const needle = this.search.toLowerCase()
      return (this.plan?.items ?? []).filter((item) => {
        if (this.kindFilter !== 'all' && item.kind !== this.kindFilter) return false
        if (!needle) return true
        return [item.kind, item.srcTag, item.srcId, item.dstTag, item.action].join(' ').toLowerCase().includes(needle)
      })
    },
    totalItems(): number {
      return this.plan?.items.length ?? 0
    },
    selectedCount(): number {
      return (this.plan?.items ?? []).filter(item => item.action !== 'skip').length
    },
    wsProgress(): any {
      return Ws().xuiImportProgress
    },
    activeProgress(): any {
      return this.progress || this.wsProgress
    },
    progressPercent(): number {
      return Number(this.activeProgress?.percent ?? 0)
    },
    summaryText(): string {
      return JSON.stringify(this.report?.summary ?? {}, null, 2)
    },
    generatedAdmins(): any[] {
      if (Array.isArray(this.report?.generatedAdmins)) return this.report.generatedAdmins
      if (Array.isArray(this.report?.generated_admins)) return this.report.generated_admins
      return []
    },
    hasGeneratedAdmins(): boolean {
      return this.generatedAdmins.length > 0
    },
    generatedAdminsText(): string {
      return JSON.stringify(this.generatedAdmins, null, 2)
    },
  },
  watch: {
    wsProgress(value: any) {
      if (value && this.step === 3) {
        this.progress = value
      }
    },
  },
  mounted() {
    Ws().connect()
  },
  beforeUnmount() {
    this.clearGeneratedAdminsTimer()
  },
  methods: {
    async buildPlan() {
      if (!this.selectedFile) return
      this.loading = true
      this.applyError = ''
      const formData = new FormData()
      formData.append('db', this.selectedFile)
      formData.append('strategy', this.strategy)
      formData.append('includeSettings', this.includeSettings ? '1' : '0')
      formData.append('includeHistory', this.includeHistory ? '1' : '0')
      formData.append('includeRouting', this.includeRouting ? '1' : '0')
      formData.append('adminMode', this.adminMode)
      const msg = await planXuiMigration(formData)
      this.loading = false
      if (!msg.success) return
      const plan = msg.obj as MigrationPlan
      plan.items = (plan.items ?? []).map((item, index) => ({
        ...item,
        rowKey: `${item.kind}:${String(item.srcId)}:${index}`,
      }))
      this.plan = plan
      this.clearGeneratedAdminsTimer()
      this.generatedAdminsRevealed = false
      this.report = null
      this.progress = null
      this.maxStep = Math.max(this.maxStep, 2)
      this.step = 2
    },
    async applyPlan() {
      if (!this.selectedFile || !this.plan) return
      this.applyError = ''
      this.progress = { step: 'queued', current: 0, total: Math.max(this.selectedCount, 1), percent: 0 }
      this.maxStep = Math.max(this.maxStep, 3)
      this.step = 3
      const formData = new FormData()
      formData.append('db', this.selectedFile)
      formData.append('plan', JSON.stringify(this.plan))
      const msg = await applyXuiMigration(formData)
      if (!msg.success) {
        this.step = 2
        this.progress = null
        this.applyError = msg.msg || this.$t('migrateXui.applyFailedFallback')
        return
      }
      this.report = msg.obj
      this.generatedAdminsRevealed = false
      this.scheduleGeneratedAdminsClear()
      this.progress = { step: 'done', current: this.selectedCount, total: Math.max(this.selectedCount, 1), percent: 100 }
      this.maxStep = 4
      this.step = 4
      await Data().loadData()
    },
    async rollback() {
      if (!this.report?.backupPath) return
      this.rollbackError = ''
      this.rollbackLoading = true
      try {
        const msg = await rollbackXuiMigration(this.report.backupPath)
        if (!msg.success) {
          this.rollbackError = msg.msg || this.$t('migrateXui.rollbackFailedFallback')
          return
        }
        const ready = await this.waitForRollbackReady()
        if (ready) {
          location.reload()
          return
        }
        this.rollbackError = this.$t('migrateXui.rollbackHealthTimeout')
      } finally {
        this.rollbackLoading = false
      }
    },
    async waitForRollbackReady(timeoutMs = 15000, intervalMs = 500): Promise<boolean> {
      const deadline = Date.now() + timeoutMs
      while (Date.now() < deadline) {
        try {
          const response = await api.get('api/status', { params: { r: 'db' } })
          const body = response.data
          if (body?.success && body.obj && Object.prototype.hasOwnProperty.call(body.obj, 'db') && body.obj.db !== null) {
            return true
          }
        } catch {
          // Backend may be restarting after rollback; keep polling until timeout.
        }
        await new Promise(resolve => setTimeout(resolve, intervalMs))
      }
      return false
    },
    clearGeneratedAdminsTimer() {
      if (this.generatedAdminsClearTimer) {
        clearTimeout(this.generatedAdminsClearTimer)
        this.generatedAdminsClearTimer = undefined
      }
    },
    scheduleGeneratedAdminsClear() {
      this.clearGeneratedAdminsTimer()
      if (!this.hasGeneratedAdmins) return
      this.generatedAdminsClearTimer = setTimeout(() => {
        this.clearGeneratedAdmins()
      }, generatedAdminsAutoClearMs)
    },
    clearGeneratedAdmins() {
      this.clearGeneratedAdminsTimer()
      if (this.report) {
        this.report.generatedAdmins = []
      }
      this.generatedAdminsRevealed = false
    },
    setImport(item: PlanItem, enabled: boolean) {
      item.action = enabled ? (item.conflict ? this.strategy : 'create') : 'skip'
    },
    handleNativeFileChange(event: Event) {
      const input = event.target as HTMLInputElement
      this.file = input.files?.[0] ?? null
    },
    rowItem(item: any): PlanItem {
      return item?.raw ?? item
    },
    kindTitle(kind: string): string {
      return this.$t(`migrateXui.kinds.${kind}`) as string
    },
    previewText(item: PlanItem): string {
      return JSON.stringify(item.previewJson ?? null, null, 2)
    },
    downloadJSON() {
      this.download('xui-import-report.json', JSON.stringify(this.report ?? {}, null, 2), 'application/json')
    },
    downloadMarkdown() {
      this.download('xui-import-report.md', this.markdownReport(), 'text/markdown')
    },
    download(name: string, content: string, type: string) {
      const blob = new Blob([content], { type })
      const url = URL.createObjectURL(blob)
      const link = document.createElement('a')
      link.href = url
      link.download = name
      link.click()
      URL.revokeObjectURL(url)
    },
    markdownReport(): string {
      const summary = this.report?.summary ?? {}
      const lines = ['# 3x-ui import report', '', `Backup: ${this.report?.backupPath || '-'}`, '']
      for (const [key, value] of Object.entries(summary)) {
        lines.push(`## ${key}`, '```json', JSON.stringify(value, null, 2), '```', '')
      }
      if (this.report?.warnings?.length) {
        lines.push('## Warnings', ...this.report.warnings.map((warning: string) => `- ${warning}`), '')
      }
      return lines.join('\n')
    },
  },
})
