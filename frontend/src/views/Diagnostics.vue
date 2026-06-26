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
import PageHeader from '@/components/nexus/primitives/PageHeader.vue'
import PageToolbar from '@/components/nexus/primitives/PageToolbar.vue'
import { useDiagnosticsPage } from '@/shared/composables/pages/useDiagnosticsPage'

const { categoryCounts, categoryLabel, categoryOptions, checks, copyReport, counts, downloadBundle, error, generatedAt, healthColor, healthLabel, levelOptions, loadLogs, loading, logEntries, logFilters, logLevelColor, logsLoading, nexus, refreshAll, report, sections, sourceOptions, statusColor, statusLabel } = useDiagnosticsPage()
</script>

<style scoped lang="scss" src="./Diagnostics.scss"></style>
