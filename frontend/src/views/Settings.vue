<template>
  <page-header v-if="nexus" :title="$t('pages.settings')" />
  <v-card :loading="loading" :flat="nexus" :class="{ 'settings-nexus-card': nexus }">
    <v-tabs
    v-model="tab"
    color="primary"
    align-tabs="center"
    show-arrows
  >
    <v-tab value="t1">{{ $t('setting.interface') }}</v-tab>
    <v-tab value="t2">{{ $t('setting.sub') }}</v-tab>
    <v-tab value="t3">{{ $t('setting.jsonSub') }}</v-tab>
    <v-tab value="t4">{{ $t('setting.clashSub') }}</v-tab>
    <v-tab value="t5">{{ $t('setting.xraySub') }}</v-tab>
    <v-tab value="basics">Basics (Singbox)</v-tab>
    <v-tab value="t6">{{ $t('setting.maintenance') }}</v-tab>
  </v-tabs>
  <v-card-text>
    <v-row
      v-if="tab !== 't6' && tab !== 'basics'"
      align="center"
      class="settings-actions"
      :class="{ 'settings-actions--nexus': nexus }"
      justify="center"
    >
      <v-col cols="auto">
        <v-btn color="primary" @click="save" :loading="loading" :disabled="!stateChange">
          {{ $t('actions.save') }}
        </v-btn>
      </v-col>
      <v-col cols="auto">
        <v-btn variant="outlined" color="warning" @click="restartApp" :loading="loading" :disabled="stateChange">
          {{ $t('actions.restartApp') }}
        </v-btn>
      </v-col>
    </v-row>
    <v-window v-model="tab">
      <v-window-item value="t1">
        <v-row v-if="showNexusControls">
          <v-col cols="12" sm="6" md="4">
            <ui-mode-control variant="select" />
          </v-col>
        </v-row>
        <v-row>
          <v-col cols="12" sm="6" md="4">
            <v-text-field class="setting-info-field" v-model="settings.webListen" :label="$t('setting.addr')" placeholder="0.0.0.0" persistent-placeholder hide-details>
              <template #append-inner><SettingInfo :text="$t('setting.hint.webListen')" /></template>
            </v-text-field>
          </v-col>
          <v-col cols="12" sm="6" md="4">
            <v-text-field class="setting-info-field" v-model.number="webPort" min="1" type="number" :label="$t('setting.port')" placeholder="2095" persistent-placeholder hide-details>
              <template #append-inner><SettingInfo :text="$t('setting.hint.webPort')" /></template>
            </v-text-field>
          </v-col>
          <v-col cols="12" sm="6" md="4">
            <v-text-field class="setting-info-field" v-model="settings.webPath" :label="$t('setting.webPath')" placeholder="/app/" persistent-placeholder hide-details>
              <template #append-inner><SettingInfo :text="$t('setting.hint.webPath')" /></template>
            </v-text-field>
          </v-col>
          <v-col cols="12" sm="6" md="4">
            <v-text-field class="setting-info-field" v-model="settings.webDomain" :label="$t('setting.domain')" placeholder="example.com" persistent-placeholder hide-details>
              <template #append-inner><SettingInfo :text="$t('setting.hint.webDomain')" /></template>
            </v-text-field>
          </v-col>
          <v-col cols="12" sm="6" md="4">
            <v-text-field class="setting-info-field" v-model="settings.webKeyFile" :label="$t('setting.sslKey')" placeholder="/etc/solovey-ui/panel.key" persistent-placeholder hide-details>
              <template #append-inner><SettingInfo :text="$t('setting.hint.sslKey')" /></template>
            </v-text-field>
          </v-col>
          <v-col cols="12" sm="6" md="4">
            <v-text-field class="setting-info-field" v-model="settings.webCertFile" :label="$t('setting.sslCert')" placeholder="/etc/solovey-ui/panel.crt" persistent-placeholder hide-details>
              <template #append-inner><SettingInfo :text="$t('setting.hint.sslCert')" /></template>
            </v-text-field>
          </v-col>
          <v-col cols="12" sm="6" md="4">
            <v-text-field class="setting-info-field" v-model="settings.webURI" :label="$t('setting.webUri')" placeholder="https://panel.example.com/app/" persistent-placeholder hide-details>
              <template #append-inner><SettingInfo :text="$t('setting.hint.webUri')" /></template>
            </v-text-field>
          </v-col>
          <v-col cols="12" sm="6" md="4">
            <v-text-field
              type="number"
              v-model.number="sessionMaxAge"
              min="0"
              :label="$t('setting.sessionAge')"
              :suffix="$t('date.m')"
              placeholder="0"
              persistent-placeholder
              class="setting-info-field"
              hide-details
              >
              <template #append-inner><SettingInfo :text="$t('setting.hint.sessionAge')" /></template>
            </v-text-field>
          </v-col>
          <v-col cols="12" sm="6" md="4">
            <v-text-field
              type="number"
              v-model.number="trafficAge"
              min="0"
              :label="$t('setting.trafficAge')"
              :suffix="$t('date.d')"
              placeholder="30"
              persistent-placeholder
              class="setting-info-field"
              hide-details
              >
              <template #append-inner><SettingInfo :text="$t('setting.hint.trafficAge')" /></template>
            </v-text-field>
          </v-col>
          <v-col cols="12" sm="6" md="4">
            <v-autocomplete
              v-model="settings.timeLocation"
              auto-select-first
              class="setting-info-field"
              hide-details
              :items="timezones"
              :label="$t('setting.timeLoc')"
              persistent-placeholder
              placeholder="Europe/Moscow"
            >
              <template #append-inner><SettingInfo :text="$t('setting.hint.timeLoc')" /></template>
            </v-autocomplete>
          </v-col>
        </v-row>
      </v-window-item>

      <v-window-item value="t2">
        <v-row>
          <v-col cols="12" sm="6" md="4">
            <div class="d-flex align-center"><v-switch color="primary" v-model="subEncode" :label="$t('setting.subEncode')" hide-details /><SettingInfo class="ms-1" :text="$t('setting.hint.subEncode')" /></div>
          </v-col>
          <v-col cols="12" sm="6" md="4">
            <div class="d-flex align-center"><v-switch color="primary" v-model="subShowInfo" :label="$t('setting.subInfo')" hide-details /><SettingInfo class="ms-1" :text="$t('setting.hint.subInfo')" /></div>
          </v-col>
          <v-col cols="12" sm="6" md="4">
            <div class="d-flex align-center"><v-switch color="primary" v-model="subSecretRequired" :label="$t('setting.subSecretRequired')" hide-details /><SettingInfo class="ms-1" :text="$t('setting.hint.subSecretRequired')" /></div>
          </v-col>
        </v-row>
        <v-row>
          <v-col cols="12" sm="6" md="4">
            <div class="d-flex align-center"><v-switch color="primary" v-model="subLinkEnable" :label="$t('setting.subLinkEnable')" hide-details /><SettingInfo class="ms-1" :text="$t('setting.hint.subLinkEnable')" /></div>
          </v-col>
          <v-col cols="12" sm="6" md="4">
            <div class="d-flex align-center"><v-switch color="primary" v-model="subJsonEnable" :label="$t('setting.subJsonEnable')" hide-details /><SettingInfo class="ms-1" :text="$t('setting.hint.subJsonEnable')" /></div>
          </v-col>
          <v-col cols="12" sm="6" md="4">
            <div class="d-flex align-center"><v-switch color="primary" v-model="subClashEnable" :label="$t('setting.subClashEnable')" hide-details /><SettingInfo class="ms-1" :text="$t('setting.hint.subClashEnable')" /></div>
          </v-col>
          <v-col cols="12" sm="6" md="4">
            <div class="d-flex align-center"><v-switch color="primary" v-model="subXrayEnable" :label="$t('setting.subXrayEnable')" hide-details /><SettingInfo class="ms-1" :text="$t('setting.hint.subXrayEnable')" /></div>
          </v-col>
        </v-row>
        <v-row>
          <v-col cols="12" sm="6" md="4">
            <v-text-field class="setting-info-field" v-model="settings.subListen" :label="$t('setting.addr')" placeholder="0.0.0.0" persistent-placeholder hide-details><template #append-inner><SettingInfo :text="$t('setting.hint.subListen')" /></template></v-text-field>
          </v-col>
          <v-col cols="12" sm="6" md="4">
            <v-text-field
              type="number"
              v-model.number="subPort"
              min="1"
              :label="$t('setting.port')"
              placeholder="2096"
              persistent-placeholder
              class="setting-info-field"
              hide-details><template #append-inner><SettingInfo :text="$t('setting.hint.subPort')" /></template></v-text-field>
          </v-col>
        </v-row>
        <v-row>
          <v-col cols="12" sm="6" md="4">
            <v-text-field class="setting-info-field" v-model="settings.subKeyFile" :label="$t('setting.sslKey')" placeholder="/etc/solovey-ui/sub.key" persistent-placeholder hide-details><template #append-inner><SettingInfo :text="$t('setting.hint.subKeyFile')" /></template></v-text-field>
          </v-col>
          <v-col cols="12" sm="6" md="4">
            <v-text-field class="setting-info-field" v-model="settings.subCertFile" :label="$t('setting.sslCert')" placeholder="/etc/solovey-ui/sub.crt" persistent-placeholder hide-details><template #append-inner><SettingInfo :text="$t('setting.hint.subCertFile')" /></template></v-text-field>
          </v-col>
        </v-row>
        <v-row>
          <v-col cols="12" sm="6" md="4">
            <v-text-field class="setting-info-field" v-model="settings.subDomain" :label="$t('setting.domain')" placeholder="sub.example.com" persistent-placeholder hide-details><template #append-inner><SettingInfo :text="$t('setting.hint.subDomain')" /></template></v-text-field>
          </v-col>
          <v-col cols="12" sm="6" md="4">
            <v-text-field class="setting-info-field" v-model="settings.subPath" :label="$t('setting.path')" placeholder="/sub/" persistent-placeholder hide-details><template #append-inner><SettingInfo :text="$t('setting.hint.subPath')" /></template></v-text-field>
          </v-col>
        </v-row>
        <v-row>
          <v-col cols="12" sm="6" md="4">
            <v-text-field
              type="number"
              v-model.number="subUpdates"
              min="0"
              :label="$t('setting.update')"
              placeholder="12"
              persistent-placeholder
              class="setting-info-field"
              hide-details
              ><template #append-inner><SettingInfo :text="$t('setting.hint.update')" /></template></v-text-field>
          </v-col>
          <v-col cols="12" sm="6" md="4">
            <v-text-field class="setting-info-field" v-model="settings.subURI" :label="$t('setting.subUri')" placeholder="https://sub.example.com/sub/" persistent-placeholder hide-details><template #append-inner><SettingInfo :text="$t('setting.hint.subUri')" /></template></v-text-field>
          </v-col>
        </v-row>
        <v-row>
          <v-col cols="12" class="v-card-subtitle">{{ $t('setting.subAdvanced') }}</v-col>
          <v-col cols="12" sm="6" md="4">
            <v-text-field class="setting-info-field" v-model="settings.subTitle" :label="$t('setting.subTitle')" hide-details><template #append-inner><SettingInfo :text="$t('setting.hint.subTitle')" /></template></v-text-field>
          </v-col>
          <v-col cols="12" sm="6" md="4">
            <v-text-field class="setting-info-field" v-model="settings.subSupportUrl" :label="$t('setting.subSupportUrl')" hide-details><template #append-inner><SettingInfo :text="$t('setting.hint.subSupportUrl')" /></template></v-text-field>
          </v-col>
          <v-col cols="12" sm="6" md="4">
            <v-text-field class="setting-info-field" v-model="settings.subProfileUrl" :label="$t('setting.subProfileUrl')" hide-details><template #append-inner><SettingInfo :text="$t('setting.hint.subProfileUrl')" /></template></v-text-field>
          </v-col>
          <v-col cols="12" sm="6" md="4">
            <v-text-field
              v-model.number="subRateLimitPerIP"
              min="0"
              type="number"
              :label="$t('setting.subRateLimitPerIP')"
              class="setting-info-field"
              hide-details
            ><template #append-inner><SettingInfo :text="$t('setting.hint.subRateLimitPerIP')" /></template></v-text-field>
          </v-col>
          <v-col cols="12" sm="6" md="4">
            <div class="d-flex align-center"><v-switch color="primary" v-model="subNameInRemark" :label="$t('setting.subNameInRemark')" hide-details /><SettingInfo class="ms-1" :text="$t('setting.hint.subNameInRemark')" /></div>
          </v-col>
          <v-col cols="12" sm="6" md="4">
            <v-select
              v-model="settings.subRemoteGroupAdaptation"
              class="setting-info-field"
              density="compact"
              hide-details
              :items="remoteGroupAdaptationItems"
              :label="$t('setting.subRemoteGroupAdaptation')"
              variant="outlined"
            ><template #append-inner><SettingInfo :text="$t('setting.hint.subRemoteGroupAdaptation')" /></template></v-select>
          </v-col>
          <v-col cols="12">
            <v-textarea class="setting-info-field" v-model="settings.subAnnounce" :label="$t('setting.subAnnounce')" rows="2" hide-details><template #append-inner><SettingInfo :text="$t('setting.hint.subAnnounce')" /></template></v-textarea>
          </v-col>
        </v-row>
      </v-window-item>

      <v-window-item value="t3">
        <v-row>
          <v-col cols="12" sm="6" md="4">
            <v-text-field class="setting-info-field" v-model="settings.subJsonPath" :label="$t('setting.jsonPath')" placeholder="/json/" persistent-placeholder hide-details><template #append-inner><SettingInfo :text="$t('setting.hint.jsonPath')" /></template></v-text-field>
          </v-col>
          <v-col cols="12" sm="6" md="4">
            <v-text-field class="setting-info-field" v-model="settings.subJsonURI" :label="$t('setting.jsonSub') + ' URI'" hide-details><template #append-inner><SettingInfo :text="$t('setting.hint.subJsonURI')" /></template></v-text-field>
          </v-col>
        </v-row>
        <SubJsonExtVue :settings="settings" />
      </v-window-item>

      <v-window-item value="t4">
        <v-row>
          <v-col cols="12" sm="6" md="4">
            <v-text-field class="setting-info-field" v-model="settings.subClashPath" :label="$t('setting.clashPath')" placeholder="/clash/" persistent-placeholder hide-details><template #append-inner><SettingInfo :text="$t('setting.hint.clashPath')" /></template></v-text-field>
          </v-col>
          <v-col cols="12" sm="6" md="4">
            <v-text-field class="setting-info-field" v-model="settings.subClashURI" :label="$t('setting.clashSub') + ' URI'" hide-details><template #append-inner><SettingInfo :text="$t('setting.hint.subClashURI')" /></template></v-text-field>
          </v-col>
        </v-row>
        <SubClashExtVue :settings="settings" />
      </v-window-item>

      <v-window-item value="t5">
        <v-row>
          <v-col cols="12" sm="6" md="4">
            <v-text-field class="setting-info-field" v-model="settings.subXrayPath" :label="$t('setting.xrayPath')" placeholder="/xray/" persistent-placeholder hide-details><template #append-inner><SettingInfo :text="$t('setting.hint.xrayPath')" /></template></v-text-field>
          </v-col>
          <v-col cols="12" sm="6" md="4">
            <v-text-field class="setting-info-field" v-model="settings.subXrayURI" :label="$t('setting.xraySub') + ' URI'" hide-details><template #append-inner><SettingInfo :text="$t('setting.hint.subXrayURI')" /></template></v-text-field>
          </v-col>
        </v-row>
      </v-window-item>

      <v-window-item value="t6">
        <MaintenanceTab />
      </v-window-item>

      <v-window-item value="basics">
        <BasicsTab />
      </v-window-item>
    </v-window>
  </v-card-text>
</v-card>
</template>

<script lang="ts" setup>
import UiModeControl from '@/components/UiModeControl.vue'
import PageHeader from '@/components/nexus/primitives/PageHeader.vue'
import BasicsTab from '@/components/settings/BasicsTab.vue'
import SubJsonExtVue from '@/components/subscription/SubJsonExt.vue'
import SubClashExtVue from '@/components/subscription/SubClashExt.vue'
import MaintenanceTab from '@/components/settings/MaintenanceTab.vue'
import SettingInfo from '@/components/settings/SettingInfo.vue'
import { useSettingsPage } from '@/shared/composables/pages/useSettingsPage'

const remoteGroupAdaptationItems = ['urltest', 'selector', 'failover']

const { loading, nexus, restartApp, save, sessionMaxAge, settings, showNexusControls, stateChange, subClashEnable, subEncode, subJsonEnable, subLinkEnable, subNameInRemark, subPort, subRateLimitPerIP, subSecretRequired, subShowInfo, subUpdates, subXrayEnable, tab, trafficAge, timezones, webPort } = useSettingsPage()
</script>

<style scoped lang="scss" src="./Settings.scss"></style>
