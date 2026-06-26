<template>
  <v-container fluid class="migrate-xui">
    <v-row>
      <v-col cols="12" md="6">
        <div class="text-h5">{{ $t('migrateXui.title') }}</div>
      </v-col>
    </v-row>

    <v-row class="mb-2">
      <v-col cols="12">
        <v-card class="pa-3" rounded="lg">
          <v-row>
            <v-col
              v-for="item in stepItems"
              :key="item.value"
              cols="12"
              sm="3"
            >
              <v-btn
                block
                :color="step === item.value ? 'primary' : undefined"
                :variant="step === item.value ? 'flat' : 'tonal'"
                :disabled="item.value > maxStep"
                @click="step = item.value"
              >
                <v-icon :icon="item.icon" start></v-icon>
                {{ item.title }}
              </v-btn>
            </v-col>
          </v-row>
        </v-card>
      </v-col>
    </v-row>

    <v-window v-model="step">
      <v-window-item :value="1">
        <v-card class="pa-4" rounded="lg">
          <v-row>
            <v-col cols="12" md="5">
              <v-file-input
                v-model="file"
                accept=".db"
                data-testid="migrate-xui-db-file"
                prepend-icon="mdi-database-import"
                :label="$t('migrateXui.chooseFile')"
                :disabled="loading"
                hide-details
              ></v-file-input>
            </v-col>
            <v-col cols="12" sm="6" md="3">
              <v-select
                v-model="strategy"
                :items="strategyItems"
                :label="$t('migrateXui.strategy')"
                :disabled="loading"
                hide-details
              ></v-select>
            </v-col>
            <v-col cols="12" sm="6" md="4">
              <v-select
                v-model="adminMode"
                data-testid="migrate-xui-admin-mode"
                :items="adminModeItems"
                :label="$t('migrateXui.adminMode')"
                :disabled="loading"
                hide-details
              ></v-select>
            </v-col>
            <v-col cols="12" md="5">
              <v-checkbox
                v-model="includeSettings"
                :label="$t('migrateXui.includeSettings')"
                :disabled="loading"
                hide-details
              ></v-checkbox>
            </v-col>
            <v-col cols="12" md="4">
              <v-checkbox
                v-model="includeHistory"
                :label="$t('migrateXui.includeHistory')"
                :disabled="loading"
                hide-details
              ></v-checkbox>
            </v-col>
            <v-col cols="12" md="3">
              <v-checkbox
                v-model="includeRouting"
                :label="$t('migrateXui.includeRouting')"
                :disabled="loading"
                hide-details
              ></v-checkbox>
            </v-col>
            <v-spacer></v-spacer>
            <v-col cols="12" md="auto" align-self="center">
              <v-btn
                color="primary"
                data-testid="migrate-xui-build-plan"
                prepend-icon="mdi-clipboard-search"
                :loading="loading"
                :disabled="!selectedFile"
                @click="buildPlan"
              >
                {{ $t('migrateXui.buildPlan') }}
              </v-btn>
            </v-col>
          </v-row>
        </v-card>
      </v-window-item>

      <v-window-item :value="2">
        <v-card class="pa-4" rounded="lg">
          <v-row>
            <v-col cols="12" md="4">
              <div class="text-subtitle-1">{{ $t('migrateXui.reviewTitle') }}</div>
              <div class="text-caption text-medium-emphasis">{{ $t('migrateXui.sourceHash') }}: {{ plan?.source?.hash || '-' }}</div>
            </v-col>
            <v-col cols="12" sm="6" md="3">
              <v-select
                v-model="kindFilter"
                :items="kindFilterItems"
                :label="$t('migrateXui.filterKind')"
                hide-details
              ></v-select>
            </v-col>
            <v-col cols="12" sm="6" md="2">
              <v-text-field
                v-model.trim="search"
                prepend-inner-icon="mdi-magnify"
                :label="$t('migrateXui.search')"
                hide-details
              ></v-text-field>
            </v-col>
            <v-col cols="12" md="3" align-self="center">
              <div class="text-caption">
                {{ $t('migrateXui.selectedCount') }}: {{ selectedCount }} / {{ totalItems }}
              </div>
            </v-col>
          </v-row>

          <v-alert
            v-if="applyError"
            class="mt-3"
            type="error"
            variant="tonal"
            data-testid="migrate-xui-apply-error"
            :title="$t('migrateXui.applyFailed')"
          >
            {{ applyError }}
          </v-alert>

          <v-data-table-virtual
            class="mt-3"
            fixed-header
            show-expand
            density="compact"
            height="560"
            item-value="rowKey"
            :headers="headers"
            :items="filteredItems"
          >
            <template #item.import="{ item }">
              <v-checkbox-btn
                :model-value="rowItem(item).action !== 'skip'"
                @update:model-value="setImport(rowItem(item), Boolean($event))"
              ></v-checkbox-btn>
            </template>
            <template #item.kind="{ item }">
              <v-chip size="small" variant="tonal">{{ kindTitle(rowItem(item).kind) }}</v-chip>
            </template>
            <template #item.srcTag="{ item }">
              <span class="text-body-2">{{ rowItem(item).srcTag || rowItem(item).srcId }}</span>
            </template>
            <template #item.dstTag="{ item }">
              <v-text-field
                v-model="rowItem(item).dstTag"
                density="compact"
                hide-details
              ></v-text-field>
            </template>
            <template #item.action="{ item }">
              <v-select
                v-model="rowItem(item).action"
                :items="actionItems"
                density="compact"
                hide-details
              ></v-select>
            </template>
            <template #item.conflict="{ item }">
              <v-chip v-if="rowItem(item).conflict" size="small" color="warning" variant="tonal">
                {{ $t('migrateXui.conflict') }}
              </v-chip>
              <span v-else>-</span>
            </template>
            <template #expanded-row="{ columns, item }">
              <tr>
                <td :colspan="columns.length">
                  <v-expansion-panels class="my-2" variant="accordion">
                    <v-expansion-panel :title="$t('migrateXui.previewJson')">
                      <v-expansion-panel-text>
                        <pre class="preview-json">{{ previewText(rowItem(item)) }}</pre>
                      </v-expansion-panel-text>
                    </v-expansion-panel>
                    <v-expansion-panel v-if="rowItem(item).warnings?.length" :title="$t('migrateXui.warnings')">
                      <v-expansion-panel-text>
                        <ul>
                          <li v-for="warning in rowItem(item).warnings" :key="warning">{{ warning }}</li>
                        </ul>
                      </v-expansion-panel-text>
                    </v-expansion-panel>
                  </v-expansion-panels>
                </td>
              </tr>
            </template>
          </v-data-table-virtual>

          <v-row class="mt-4">
            <v-col cols="auto">
              <v-btn variant="tonal" prepend-icon="mdi-arrow-left" @click="step = 1">{{ $t('migrateXui.back') }}</v-btn>
            </v-col>
            <v-spacer></v-spacer>
            <v-col cols="auto">
              <v-btn color="primary" data-testid="migrate-xui-apply-plan" prepend-icon="mdi-database-check" :disabled="selectedCount === 0" @click="applyPlan">
                {{ $t('migrateXui.apply') }}
              </v-btn>
            </v-col>
          </v-row>
        </v-card>
      </v-window-item>

      <v-window-item :value="3">
        <v-card class="pa-4" rounded="lg">
          <div class="text-subtitle-1 mb-3">{{ $t('migrateXui.progressTitle') }}</div>
          <v-progress-linear
            :model-value="progressPercent"
            color="primary"
            height="12"
            rounded
          ></v-progress-linear>
          <v-row class="mt-3">
            <v-col cols="12" sm="4">{{ $t('migrateXui.current') }}: {{ activeProgress?.step || '-' }}</v-col>
            <v-col cols="12" sm="4">{{ activeProgress?.current || 0 }} / {{ activeProgress?.total || 0 }}</v-col>
            <v-col cols="12" sm="4">{{ activeProgress?.currentTag || activeProgress?.currentName || '-' }}</v-col>
          </v-row>
        </v-card>
      </v-window-item>      <MigrationResultStep
        :summary-text="summaryText"
        :report="report"
        :rollback-loading="rollbackLoading"
        :rollback-error="rollbackError"
        :has-generated-admins="hasGeneratedAdmins"
        :generated-admins-revealed="generatedAdminsRevealed"
        :generated-admins-text="generatedAdminsText"
        @download-json="downloadJSON"
        @download-markdown="downloadMarkdown"
        @rollback="rollback"
        @clear-generated-admins="clearGeneratedAdmins"
        @update:generated-admins-revealed="generatedAdminsRevealed = $event"
      />
    </v-window>
  </v-container>
</template>

<script lang="ts" src="./MigrateXui.page.ts"></script>

<style scoped lang="scss" src="./MigrateXui.scss"></style>
