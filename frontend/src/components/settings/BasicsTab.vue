<template>
  <v-row v-if="showActions" style="margin-bottom: 10px;">
    <v-col cols="12" justify="center" align="center">
      <v-btn variant="outlined" color="warning" @click="saveConfig" :loading="loading" :disabled="stateChange">
        {{ $t('actions.save') }}
      </v-btn>
    </v-col>
  </v-row>
  <v-expansion-panels>
    <v-expansion-panel :title="$t('basic.log.title')">
      <v-expansion-panel-text>
        <v-row>
          <v-col cols="12" sm="6" md="3" lg="2">
            <v-switch v-model="appConfig.log.disabled" color="primary" :label="$t('disable')" hide-details></v-switch>
          </v-col>
          <v-col cols="12" sm="6" md="3" lg="2">
            <v-select
              hide-details
              :label="$t('basic.log.level')"
              :items="levels"
              clearable
              @click:clear="delete appConfig.log.level"
              v-model="appConfig.log.level">
            </v-select>
          </v-col>
          <v-col cols="12" sm="6" md="3" lg="2">
            <v-text-field
              v-model="appConfig.log.output"
              class="setting-info-field"
              hide-details
              persistent-placeholder
              placeholder="box.log"
              :label="$t('basic.log.output')"
            ><template #append-inner><SettingInfo :text="$t('basic.hint.logOutput')" /></template></v-text-field>
          </v-col>
          <v-col cols="12" sm="6" md="3" lg="2">
            <v-switch v-model="appConfig.log.timestamp" color="primary" :label="$t('basic.log.timestamp')" hide-details></v-switch>
          </v-col>
        </v-row>
      </v-expansion-panel-text>
    </v-expansion-panel>
    <v-expansion-panel title="NTP">
      <v-expansion-panel-text>
        <v-row>
          <v-col cols="12" sm="6" md="3" lg="2">
            <v-switch v-model="enableNtp" color="primary" :label="$t('enable')" hide-details></v-switch>
          </v-col>
          <v-col cols="12" sm="6" md="3" lg="2" v-if="appConfig.ntp?.enabled">
            <v-text-field
              v-model="appConfig.ntp.server"
              class="setting-info-field"
              hide-details
              persistent-placeholder
              placeholder="time.apple.com"
              :label="$t('out.addr')"
            ><template #append-inner><SettingInfo :text="$t('basic.hint.ntpServer')" /></template></v-text-field>
          </v-col>
          <v-col cols="12" sm="6" md="3" lg="2" v-if="appConfig.ntp?.enabled">
            <v-text-field
              v-model="appConfig.ntp.server_port"
              hide-details
              type="number"
              clearable
              class="setting-info-field"
              persistent-placeholder
              placeholder="123"
              @click:clear="delete appConfig.ntp?.server_port"
              :label="$t('out.port')"
            ><template #append><SettingInfo :text="$t('basic.hint.ntpPort')" /></template></v-text-field>
          </v-col>
          <v-col cols="12" sm="6" md="3" lg="2" v-if="appConfig.ntp?.enabled">
            <v-text-field
              v-model="ntpInterval"
              hide-details
              :suffix="$t('date.m')"
              min="0"
              type="number"
              class="setting-info-field"
              persistent-placeholder
              placeholder="30"
              :label="$t('ruleset.interval')"
            ><template #append-inner><SettingInfo :text="$t('basic.hint.ntpInterval')" /></template></v-text-field>
          </v-col>
          <v-col cols="12" sm="6" md="3" lg="2" v-if="appConfig.ntp?.enabled">
            <v-switch v-model="appConfig.ntp.write_to_system" color="primary" :label="$t('singbox.writeSystemClock')" hide-details></v-switch>
          </v-col>
          <v-col cols="12" v-if="appConfig.ntp?.write_to_system">
            <v-alert density="compact" type="warning" variant="tonal">
              {{ $t('singbox.writeSystemClockWarning') }}
            </v-alert>
          </v-col>
        </v-row>
        <Dial :dial="appConfig.ntp" v-if="appConfig.ntp?.enabled" />
      </v-expansion-panel-text>
    </v-expansion-panel>
    <v-expansion-panel :title="$t('singbox.certificateTrust')">
      <v-expansion-panel-text>
        <v-row>
          <v-col cols="12" sm="6" md="3" lg="2">
            <v-select v-model="certificateMode" hide-details :label="$t('singbox.preset')" :items="certificateModes"></v-select>
          </v-col>
          <v-col cols="12" sm="6" md="3" lg="2" v-if="appConfig.certificate">
            <v-select
              v-model="appConfig.certificate.store"
              hide-details
              clearable
              @click:clear="delete appConfig.certificate?.store"
              :label="$t('tls.store')"
              :items="certificateStores">
            </v-select>
          </v-col>
        </v-row>
        <v-row v-if="appConfig.certificate && (certificateMode == 'file' || certificateMode == 'custom')">
          <v-col cols="12" sm="8">
            <v-textarea v-model="certificatePathText" rows="2" auto-grow hide-details :label="$t('singbox.caFilePaths')"></v-textarea>
          </v-col>
        </v-row>
        <v-row v-if="appConfig.certificate && (certificateMode == 'directory' || certificateMode == 'custom')">
          <v-col cols="12" sm="8">
            <v-textarea v-model="certificateDirectoryText" rows="2" auto-grow hide-details :label="$t('singbox.caDirectoryPaths')"></v-textarea>
          </v-col>
        </v-row>
        <v-row v-if="appConfig.certificate && (certificateMode == 'pem' || certificateMode == 'custom')">
          <v-col cols="12">
            <v-textarea v-model="certificateText" rows="5" auto-grow hide-details :label="$t('singbox.pemCertificates')"></v-textarea>
          </v-col>
        </v-row>
      </v-expansion-panel-text>
    </v-expansion-panel>
    <v-expansion-panel title="Experimental">
      <v-expansion-panel-text>
        <v-row>
          <v-col class="v-card-subtitle">{{ $t('singbox.cacheFile') }}</v-col>
        </v-row>
        <v-row>
          <v-col cols="12" sm="6" md="3" lg="2">
            <v-switch v-model="enableCacheFile" color="primary" :label="$t('enable')" hide-details></v-switch>
          </v-col>
          <v-col cols="12" sm="6" md="3" lg="2" v-if="appConfig.experimental.cache_file">
            <v-text-field
              v-model="appConfig.experimental.cache_file.path"
              class="setting-info-field"
              hide-details
              persistent-placeholder
              placeholder="cache.db"
              :label="$t('transport.path')"
            ><template #append-inner><SettingInfo :text="$t('basic.hint.cachePath')" /></template></v-text-field>
          </v-col>
          <v-col cols="12" sm="6" md="3" lg="2" v-if="appConfig.experimental.cache_file">
            <v-text-field
              v-model="appConfig.experimental.cache_file.cache_id"
              class="setting-info-field"
              hide-details
              :label="$t('singbox.cacheId')"
            ><template #append-inner><SettingInfo :text="$t('basic.hint.cacheId')" /></template></v-text-field>
          </v-col>
          <v-col cols="12" sm="6" md="3" lg="2" v-if="appConfig.experimental.cache_file">
            <v-switch v-model="appConfig.experimental.cache_file.store_fakeip"
              color="primary"
              :label="$t('basic.exp.storeFakeIp')"
              hide-details></v-switch>
          </v-col>
          <v-col cols="12" sm="6" md="3" lg="2" v-if="appConfig.experimental.cache_file">
            <v-switch v-model="appConfig.experimental.cache_file.store_rdrc"
              color="primary"
              :label="$t('singbox.storeRdrc')"
              hide-details></v-switch>
          </v-col>
          <v-col cols="12" sm="6" md="3" lg="2" v-if="appConfig.experimental.cache_file?.store_rdrc">
            <v-text-field
              v-model="appConfig.experimental.cache_file.rdrc_timeout"
              hide-details
              placeholder="7d"
              :label="$t('singbox.rdrcTimeout')">
            </v-text-field>
          </v-col>
        </v-row>
        <v-row>
          <v-col class="v-card-subtitle">{{ $t('singbox.debug') }}</v-col>
        </v-row>
        <v-row>
          <v-col cols="12" sm="6" md="3" lg="2">
            <v-switch v-model="enableDebug" color="primary" :label="$t('enable')" hide-details></v-switch>
          </v-col>
          <template v-if="appConfig.experimental.debug">
            <v-col cols="12" sm="6" md="3" lg="2">
              <v-text-field class="setting-info-field" v-model="appConfig.experimental.debug.listen" hide-details placeholder="127.0.0.1:8080" persistent-placeholder :label="$t('objects.listen')"><template #append-inner><SettingInfo :text="$t('basic.hint.debugListen')" /></template></v-text-field>
            </v-col>
            <v-col cols="12" sm="6" md="3" lg="2">
              <v-text-field class="setting-info-field" v-model.number="appConfig.experimental.debug.gc_percent" type="number" hide-details :label="$t('singbox.gcPercent')"><template #append-inner><SettingInfo :text="$t('basic.hint.gcPercent')" /></template></v-text-field>
            </v-col>
            <v-col cols="12" sm="6" md="3" lg="2">
              <v-text-field class="setting-info-field" v-model="appConfig.experimental.debug.memory_limit" hide-details placeholder="256MiB" persistent-placeholder :label="$t('singbox.memoryLimit')"><template #append-inner><SettingInfo :text="$t('basic.hint.memoryLimit')" /></template></v-text-field>
            </v-col>
            <v-col cols="12" sm="6" md="3" lg="2">
              <v-text-field class="setting-info-field" v-model.number="appConfig.experimental.debug.max_stack" type="number" hide-details :label="$t('singbox.maxStack')"><template #append-inner><SettingInfo :text="$t('basic.hint.maxStack')" /></template></v-text-field>
            </v-col>
            <v-col cols="12" sm="6" md="3" lg="2">
              <v-text-field class="setting-info-field" v-model.number="appConfig.experimental.debug.max_threads" type="number" hide-details :label="$t('singbox.maxThreads')"><template #append-inner><SettingInfo :text="$t('basic.hint.maxThreads')" /></template></v-text-field>
            </v-col>
            <v-col cols="12" sm="6" md="3" lg="2">
              <v-switch v-model="appConfig.experimental.debug.panic_on_fault" color="primary" :label="$t('singbox.panicOnFault')" hide-details></v-switch>
            </v-col>
            <v-col cols="12" sm="6" md="3" lg="2">
              <v-select
                v-model="appConfig.experimental.debug.trace_back"
                hide-details clearable
                @click:clear="delete appConfig.experimental.debug?.trace_back"
                :label="$t('singbox.traceback')"
                :items="['none','single','all','system','crash']">
              </v-select>
            </v-col>
          </template>
        </v-row>
        <v-row>
          <v-col class="v-card-subtitle">Clash API</v-col>
        </v-row>
        <v-row>
          <v-col cols="12" sm="6" md="3" lg="2">
            <v-switch v-model="enableClashApi" color="primary" :label="$t('enable')" hide-details></v-switch>
          </v-col>
          <template v-if="appConfig.experimental.clash_api">
            <v-col cols="12" sm="6" md="3" lg="2">
              <v-text-field
              v-model="appConfig.experimental.clash_api.external_controller"
                class="setting-info-field"
                hide-details
                persistent-placeholder
                placeholder="127.0.0.1:9090"
                :label="$t('basic.exp.extController')"
              ><template #append-inner><SettingInfo :text="$t('basic.hint.clashController')" /></template></v-text-field>
            </v-col>
            <v-col cols="12" sm="6" md="3" lg="2">
              <v-text-field
                v-model="appConfig.experimental.clash_api.secret"
                class="setting-info-field"
                hide-details
                :label="$t('basic.exp.secret')"
              ><template #append-inner><SettingInfo :text="$t('basic.hint.clashSecret')" /></template></v-text-field>
            </v-col>
          </template>
        </v-row>
        <v-row v-if="appConfig.experimental.clash_api">
          <v-col cols="12" sm="6" md="3" lg="2">
            <v-text-field
              v-model="appConfig.experimental.clash_api.external_ui"
              class="setting-info-field"
              hide-details
              :label="$t('basic.exp.extUi')"
            ><template #append-inner><SettingInfo :text="$t('basic.hint.clashExtUi')" /></template></v-text-field>
          </v-col>
          <v-col cols="12" sm="8" md="4">
            <v-text-field
              v-model="appConfig.experimental.clash_api.external_ui_download_url"
              class="setting-info-field"
              hide-details
              :label="$t('basic.exp.extUiDownloadUrl')"
            ><template #append-inner><SettingInfo :text="$t('basic.hint.clashExtUiUrl')" /></template></v-text-field>
          </v-col>
          <v-col cols="12" sm="6" md="3" lg="2">
            <v-select
              v-model="appConfig.experimental.clash_api.external_ui_download_detour"
              hide-details
              :items="outboundTags"
              clearable
              @click:clear="delete appConfig.experimental.clash_api.external_ui_download_detour"
              :label="$t('basic.exp.extUiDownloadDetour')"
            ></v-select>
          </v-col>
        </v-row>
        <v-row v-if="appConfig.experimental.clash_api">
          <v-col cols="12" sm="6" md="3" lg="2">
            <v-select
              v-model="appConfig.experimental.clash_api.default_mode"
              class="setting-info-field"
              clearable
              hide-details
              :items="clashModes"
              persistent-placeholder
              placeholder="rule"
              :label="$t('basic.exp.defaultMode')"
            ><template #append><SettingInfo :text="$t('basic.hint.clashDefaultMode')" /></template></v-select>
          </v-col>
          <v-col cols="12" sm="8" md="4">
            <v-text-field
              v-model="origin"
              class="setting-info-field"
              hide-details
              :label="$t('basic.exp.allowOrigin') + ' ' + $t('commaSeparated')"
            ><template #append-inner><SettingInfo :text="$t('basic.hint.clashAllowOrigin')" /></template></v-text-field>
          </v-col>
          <v-col cols="12" sm="6" md="3" lg="2">
            <v-switch v-model="appConfig.experimental.clash_api.access_control_allow_private_network" color="primary" :label="$t('basic.exp.allowPrivate')" hide-details></v-switch>
          </v-col>
        </v-row>
        <v-row>
          <v-col class="v-card-subtitle">V2Ray API</v-col>
        </v-row>
        <v-row>
          <v-col cols="12" sm="6" md="3" lg="2">
            <v-switch v-model="enableV2rayApi" color="primary" :label="$t('enable')" hide-details></v-switch>
          </v-col>
          <template v-if="appConfig.experimental.v2ray_api">
            <v-col cols="12" sm="6" md="3" lg="2">
              <v-text-field
                v-model="appConfig.experimental.v2ray_api.listen"
                class="setting-info-field"
                hide-details
                persistent-placeholder
                placeholder="127.0.0.1:8080"
                :label="$t('objects.listen')"
              ><template #append-inner><SettingInfo :text="$t('basic.hint.v2rayListen')" /></template></v-text-field>
            </v-col>
            <v-col cols="12" sm="6" md="3" lg="2">
              <v-switch v-model="appConfig.experimental.v2ray_api.stats.enabled"
                color="primary"
                :label="$t('stats.enable')"
                hide-details></v-switch>
            </v-col>
          </template>
        </v-row>
        <v-row v-if="appConfig.experimental.v2ray_api?.stats?.enabled">
          <v-col cols="12" sm="6">
            <v-select
              hide-details
              :label="$t('pages.inbounds')"
              multiple chips closable-chips
              :items="inboundTags"
              v-model="appConfig.experimental.v2ray_api.stats.inbounds">
            </v-select>
          </v-col>
          <v-col cols="12" sm="6">
            <v-select
              hide-details
              :label="$t('pages.outbounds')"
              multiple chips closable-chips
              :items="outboundTags"
              v-model="appConfig.experimental.v2ray_api.stats.outbounds">
            </v-select>
          </v-col>
          <v-col cols="12" sm="6">
            <v-select
              hide-details
              :label="$t('pages.clients')"
              multiple chips closable-chips
              :items="clientNames"
              v-model="appConfig.experimental.v2ray_api.stats.users">
            </v-select>
          </v-col>
        </v-row>
      </v-expansion-panel-text>
    </v-expansion-panel>
  </v-expansion-panels>
</template>

<script lang="ts" setup>
import Dial from '@/components/fields/Dial.vue'
import SettingInfo from '@/components/settings/SettingInfo.vue'
import { useBasicsPage } from '@/shared/composables/pages/useBasicsPage'

withDefaults(defineProps<{
  showActions?: boolean
}>(), {
  showActions: true,
})

const { appConfig, certificateDirectoryText, certificateMode, certificateModes, certificatePathText, certificateStores, certificateText, clashModes, clientNames, enableCacheFile, enableClashApi, enableDebug, enableNtp, enableV2rayApi, inboundTags, levels, loading, ntpInterval, origin, outboundTags, saveConfig, stateChange } = useBasicsPage()
</script>
