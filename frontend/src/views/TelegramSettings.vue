<template>
  <v-card :loading="loading">
    <v-card-title>{{ $t('telegram.title') }}</v-card-title>
    <v-divider></v-divider>
    <v-card-text>
      <v-alert type="warning" variant="tonal" density="compact" class="mb-4">
        {{ $t('telegram.securityWarning') }}
      </v-alert>
      <v-row align="center">
        <v-col cols="12" sm="6" md="4">
          <v-switch color="primary" v-model="telegramEnabled" :label="$t('telegram.enabled')" hide-details />
        </v-col>
        <v-col cols="12" sm="6" md="4">
          <v-switch color="primary" v-model="telegramNotifyCpu" :label="$t('telegram.notifyCpu')" hide-details />
        </v-col>
        <v-col cols="12" sm="6" md="4">
          <v-switch color="primary" v-model="telegramReport" :label="$t('telegram.report')" hide-details />
        </v-col>
      </v-row>
      <v-row>
        <v-col cols="12" sm="6" md="4">
          <SettingsSecretField
            v-model="settings.telegramBotToken"
            :has-secret="settings.telegramBotTokenHasSecret"
            :label="$t('telegram.botToken')"
            hide-details
          />
        </v-col>
        <v-col cols="12" sm="6" md="4">
          <v-text-field class="setting-info-field" v-model="settings.telegramChatID" :label="$t('telegram.chatId')" placeholder="123456789" persistent-placeholder hide-details>
            <template #append-inner><SettingInfo :text="$t('telegram.hint.chatId')" /></template>
          </v-text-field>
        </v-col>
        <v-col cols="12" sm="6" md="4">
          <v-text-field
            v-model.number="telegramCpuThreshold"
            type="number"
            min="1"
            max="100"
            :label="$t('telegram.cpuThreshold')"
            suffix="%"
            class="setting-info-field"
            placeholder="90"
            persistent-placeholder
            hide-details
          ><template #append-inner><SettingInfo :text="$t('telegram.hint.cpuThreshold')" /></template></v-text-field>
        </v-col>
      </v-row>
      <v-row>
        <v-col cols="12" sm="6" md="4">
          <v-select
            v-model="settings.telegramTransportMode"
            :items="transportModes"
            item-title="title"
            item-value="value"
            class="setting-info-field"
            :label="$t('telegram.transport')"
            hide-details
          ><template #append><SettingInfo :text="$t('telegram.hint.transport')" /></template></v-select>
        </v-col>
        <v-col v-if="settings.telegramTransportMode === 'outbound'" cols="12" sm="6" md="8">
          <v-select
            v-model="settings.telegramOutboundTag"
            :items="outboundOptions"
            item-title="title"
            item-value="value"
            :label="$t('telegram.outboundLabel')"
            :hint="outboundOptions.length === 0 ? $t('telegram.noOutbounds') : ''"
            persistent-hint
          />
        </v-col>
      </v-row>
      <v-row v-if="settings.telegramTransportMode !== 'outbound'">
        <v-col cols="12" sm="6" md="4">
          <SettingsSecretField
            v-model="settings.telegramProxyURL"
            :has-secret="settings.telegramProxyURLHasSecret"
            :label="$t('telegram.proxyUrl')"
            hide-details
          />
        </v-col>
        <v-col cols="12" sm="6" md="4">
          <SettingsSecretField
            v-model="settings.telegramProxyUsername"
            :has-secret="settings.telegramProxyUsernameHasSecret"
            :label="$t('telegram.proxyUsername')"
            hide-details
          />
        </v-col>
        <v-col cols="12" sm="6" md="4">
          <SettingsSecretField
            v-model="settings.telegramProxyPassword"
            :has-secret="settings.telegramProxyPasswordHasSecret"
            :label="$t('telegram.proxyPassword')"
            hide-details
          />
        </v-col>
      </v-row>
      <v-row>
        <v-col cols="12" md="8">
          <v-text-field class="setting-info-field" v-model="settings.telegramReportCron" :label="$t('telegram.reportCron')" placeholder="0 9 * * *" persistent-placeholder hide-details>
            <template #append-inner><SettingInfo :text="$t('telegram.hint.reportCron')" /></template>
          </v-text-field>
        </v-col>
      </v-row>
      <v-divider class="my-4"></v-divider>
      <section :class="{ 'telegram-backup-disabled': !telegramEnabled }">
        <div class="text-subtitle-1 mb-2">{{ $t('telegram.backup.title') }}</div>
        <v-row align="center">
          <v-col cols="12" sm="6" md="4">
            <v-switch
              color="primary"
              v-model="telegramBackupEnabled"
              :label="$t('telegram.backup.enabled')"
              :disabled="!telegramEnabled"
              hide-details
            />
          </v-col>
          <v-col cols="12" sm="6" md="4">
            <v-text-field
              v-model.number="telegramBackupMaxSizeMB"
              type="number"
              min="1"
              max="50"
              :label="$t('telegram.backup.maxSize')"
              suffix="MB"
              class="setting-info-field"
              placeholder="45"
              persistent-placeholder
              :disabled="!telegramEnabled"
              hide-details
            ><template #append-inner><SettingInfo :text="$t('telegram.hint.backupMaxSize')" /></template></v-text-field>
          </v-col>
          <v-col cols="12" sm="6" md="4">
            <v-btn
              variant="outlined"
              color="primary"
              :loading="backupRunLoading"
              :disabled="!telegramEnabled"
              @click="sendTelegramBackupNow"
            >
              <v-icon icon="mdi-cloud-upload-outline" class="me-2" />
              {{ $t('telegram.backup.sendNow') }}
            </v-btn>
          </v-col>
        </v-row>
        <v-row>
          <v-col cols="12" md="6">
            <SettingsSecretField
              v-model="settings.telegramBackupPassphrase"
              :has-secret="settings.telegramBackupPassphraseHasSecret"
              :label="$t('telegram.backup.passphrase')"
              :disabled="!telegramEnabled"
              :error-messages="telegramBackupPassphraseErrors"
              :hide-details="telegramBackupPassphraseErrors.length === 0"
            />
            <div class="text-caption text-medium-emphasis mt-1">
              {{ $t('telegram.backup.passphraseHint') }}
            </div>
          </v-col>
          <v-col cols="12" md="6">
            <v-row>
              <v-col cols="12" md="6">
                <v-select
                  v-model="telegramBackupScheduleMode"
                  :items="telegramBackupScheduleOptions"
                  item-title="title"
                  item-value="value"
                  :label="$t('telegram.backup.schedule.title')"
                  :disabled="!telegramEnabled"
                  hide-details
                  @update:model-value="handleTelegramBackupScheduleModeChange"
                />
              </v-col>
              <v-col v-if="telegramBackupScheduleMode === 'custom'" cols="12" md="3">
                <v-text-field
                  v-model.number="telegramBackupCustomValue"
                  type="number"
                  min="1"
                  :max="telegramBackupCustomMax"
                  :label="$t('telegram.backup.schedule.customValue')"
                  :disabled="!telegramEnabled"
                  :error-messages="telegramBackupScheduleErrors"
                  :hide-details="telegramBackupScheduleErrors.length === 0"
                  @update:model-value="updateTelegramBackupCronFromSchedule"
                />
              </v-col>
              <v-col v-if="telegramBackupScheduleMode === 'custom'" cols="12" md="3">
                <v-select
                  v-model="telegramBackupCustomUnit"
                  :items="telegramBackupScheduleUnitOptions"
                  item-title="title"
                  item-value="value"
                  :label="$t('telegram.backup.schedule.customUnit')"
                  :disabled="!telegramEnabled"
                  hide-details
                  @update:model-value="updateTelegramBackupCronFromSchedule"
                />
              </v-col>
              <v-col v-if="telegramBackupScheduleMode === 'advanced'" cols="12">
                <v-text-field
                  v-model="telegramBackupAdvancedCron"
                  :label="$t('telegram.backup.schedule.advancedCron')"
                  :disabled="!telegramEnabled"
                  :error-messages="telegramBackupScheduleErrors"
                  :hide-details="telegramBackupScheduleErrors.length === 0"
                  @update:model-value="updateTelegramBackupCronFromSchedule"
                />
              </v-col>
            </v-row>
          </v-col>
        </v-row>
        <v-row>
          <v-col cols="12">
            <div class="text-caption text-medium-emphasis mb-1">{{ $t('telegram.backup.excludeTables') }}</div>
          </v-col>
          <v-col v-for="table in telegramBackupExcludeTableOptions" :key="table" cols="12" sm="6" md="3">
            <v-checkbox
              v-model="telegramBackupExcludeTables"
              :value="table"
              :label="$t('telegram.backup.tables.' + table)"
              :disabled="!telegramEnabled"
              hide-details
            />
          </v-col>
        </v-row>
        <v-row v-if="backupRunStatus">
          <v-col cols="12" md="6">
            <v-chip :color="backupRunStatus.success ? 'success' : 'warning'" label>
              {{ backupRunStatus.timestamp }} · {{ backupRunStatus.success ? $t('success') : backupRunStatus.errorClass }}
            </v-chip>
          </v-col>
        </v-row>
      </section>
      <v-row align="center">
        <v-col cols="auto">
          <v-btn
            color="primary"
            :loading="loading"
            :disabled="!stateChange || telegramBackupScheduleErrors.length > 0 || telegramBackupPassphraseErrors.length > 0"
            @click="save"
          >
            {{ $t('actions.save') }}
          </v-btn>
        </v-col>
        <v-col cols="auto">
          <v-btn variant="outlined" color="primary" :loading="testLoading" @click="testTelegram">
            <v-icon icon="mdi-send-check-outline" class="me-2" />
            {{ $t('actions.test') }}
          </v-btn>
        </v-col>
        <v-col cols="12" md="6" v-if="testResult">
          <v-chip :color="testResult.success ? 'success' : 'warning'" label>
            {{ testResult.success ? $t('success') : testResult.errorClass }}
          </v-chip>
        </v-col>
      </v-row>
    </v-card-text>
  </v-card>
</template>

<script lang="ts" setup>
import SettingsSecretField from '@/components/settings/SettingsSecretField.vue'
import SettingInfo from '@/components/settings/SettingInfo.vue'
import { useTelegramSettingsPage } from '@/shared/composables/pages/useTelegramSettingsPage'

const { backupRunLoading, backupRunStatus, handleTelegramBackupScheduleModeChange, loading, outboundOptions, save, sendTelegramBackupNow, settings, stateChange, telegramBackupAdvancedCron, telegramBackupCustomMax, telegramBackupCustomUnit, telegramBackupCustomValue, telegramBackupEnabled, telegramBackupExcludeTableOptions, telegramBackupExcludeTables, telegramBackupMaxSizeMB, telegramBackupPassphraseErrors, telegramBackupScheduleErrors, telegramBackupScheduleMode, telegramBackupScheduleOptions, telegramBackupScheduleUnitOptions, telegramCpuThreshold, telegramEnabled, telegramNotifyCpu, telegramReport, testLoading, testResult, testTelegram, transportModes, updateTelegramBackupCronFromSchedule } = useTelegramSettingsPage()
</script>

<style scoped lang="scss" src="./TelegramSettings.scss"></style>
