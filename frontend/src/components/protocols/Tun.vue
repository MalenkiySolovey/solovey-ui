<template>
  <v-card subtitle="Tun">
    <v-row>
      <v-col cols="12" sm="8">
        <v-text-field v-model="addrs" :label="$t('types.tun.addr') + ' ' + $t('commaSeparated')" placeholder="172.18.0.1/30" hide-details></v-text-field>
      </v-col>
    </v-row>
    <v-row>
      <v-col cols="12" sm="6" md="4">
        <v-text-field v-model="data.interface_name" :label="$t('types.tun.ifName')" placeholder="tun0" hide-details clearable @click:clear="delete data.interface_name"></v-text-field>
      </v-col>
      <v-col cols="12" sm="6" md="4">
        <v-text-field type="number" v-model.number="data.mtu" label="MTU" hide-details></v-text-field>
      </v-col>
    </v-row>
    <v-row>
      <v-col cols="12" sm="6" md="4">
        <v-text-field
          type="number"
          v-model.number="udpTimeout"
          label="UDP timeout"
          min="1"
          :suffix="$t('date.m')"
          hide-details>
        </v-text-field>
      </v-col>
      <v-col cols="12" sm="6" md="4">
        <v-select
          v-model="data.stack"
          label="Stack"
          :items="['system','gvisor','mixed']"
          hide-details
        ></v-select>
      </v-col>
      <v-col cols="12" sm="6" md="4" v-if="data.endpoint_independent_nat != undefined">
        <v-switch v-model="optionEndpointIndependentNat" color="primary" :label="$t('singbox.independentNatCompatibility')" hide-details></v-switch>
      </v-col>
    </v-row>
    <v-row>
      <v-col cols="12" sm="6" md="4">
        <v-switch v-model="autoRoute" color="primary" label="Auto Route" hide-details></v-switch>
      </v-col>
      <v-col cols="12" sm="6" md="4" v-if="autoRoute">
        <v-switch v-model="autoRedirect" color="primary" label="Auto Redirect" hide-details></v-switch>
      </v-col>
      <v-col cols="12" sm="6" md="4" v-if="autoRoute">
        <v-switch v-model="strictRoute" color="primary" label="Strict Route" hide-details></v-switch>
      </v-col>
      <v-col cols="12" sm="6" md="4" v-if="autoRoute && data.auto_redirect">
        <v-switch v-model="excludeMptcp" color="primary" :label="$t('types.tun.excludeMptcp')" hide-details></v-switch>
      </v-col>
      <v-col cols="12" sm="6" md="4" v-if="autoRoute && data.auto_redirect">
        <v-text-field
          type="number"
          v-model.number="fallbackRuleIndex"
          :label="$t('types.tun.fallbackRuleIndex')"
          min="0"
          hide-details>
        </v-text-field>
      </v-col>
      <v-col cols="12" sm="6" md="4" v-if="autoRoute && data.auto_redirect">
        <v-text-field
          v-model="autoRedirectResetMark"
          :label="$t('types.tun.resetMark')"
          placeholder="0x2024"
          hide-details>
        </v-text-field>
      </v-col>
      <v-col cols="12" sm="6" md="4" v-if="autoRoute && data.auto_redirect">
        <v-text-field v-model="autoRedirectInputMark" :label="$t('singbox.inputMark')" placeholder="0x2023" hide-details></v-text-field>
      </v-col>
      <v-col cols="12" sm="6" md="4" v-if="autoRoute && data.auto_redirect">
        <v-text-field v-model="autoRedirectOutputMark" :label="$t('singbox.outputMark')" placeholder="0x2024" hide-details></v-text-field>
      </v-col>
      <v-col cols="12" sm="6" md="4" v-if="autoRoute && data.auto_redirect">
        <v-text-field
          type="number"
          v-model.number="nfqueue"
          :label="$t('types.tun.nfqueue')"
          min="0"
          hide-details>
        </v-text-field>
      </v-col>
    </v-row>
    <template v-if="autoRoute">
      <v-row>
        <v-col cols="12" class="v-card-subtitle">{{ $t('singbox.splitRouting') }}</v-col>
        <v-col cols="12" sm="6" md="4">
          <v-btn variant="tonal" @click="applyLanDirect">{{ $t('singbox.keepLanDirect') }}</v-btn>
        </v-col>
        <v-col cols="12" sm="6" md="4">
          <v-select
            v-model="routeAddressSet"
            :items="ruleSetTags"
            :label="$t('singbox.ruleSetTunnel')"
            multiple chips closable-chips
            hide-details>
          </v-select>
        </v-col>
        <v-col cols="12" sm="6" md="4" v-if="data.auto_redirect && data.route_address_set?.length > 0">
          <v-alert density="compact" type="warning" variant="tonal">
            {{ $t('singbox.autoRedirectRuleSetWarning') }}
          </v-alert>
        </v-col>
        <v-col cols="12" sm="6">
          <v-textarea v-model="routeAddressText" rows="2" auto-grow hide-details :label="$t('singbox.routeAddresses')"></v-textarea>
        </v-col>
        <v-col cols="12" sm="6">
          <v-textarea v-model="routeExcludeAddressText" rows="2" auto-grow hide-details :label="$t('singbox.routeExcludeAddresses')"></v-textarea>
        </v-col>
      </v-row>
      <v-row>
        <v-col cols="12" class="v-card-subtitle">{{ $t('singbox.appsUsers') }}</v-col>
        <v-col cols="12" sm="6">
          <v-textarea v-model="includePackageText" rows="2" auto-grow hide-details :label="$t('singbox.includePackages')"></v-textarea>
        </v-col>
        <v-col cols="12" sm="6">
          <v-textarea v-model="excludePackageText" rows="2" auto-grow hide-details :label="$t('singbox.excludePackages')"></v-textarea>
        </v-col>
        <v-col cols="12" v-if="emptyAppPreset">
          <v-alert density="compact" type="warning" variant="tonal">
            {{ $t('singbox.appPackageWarning') }}
          </v-alert>
        </v-col>
        <v-col cols="12" sm="6">
          <v-textarea v-model="includeUidText" rows="2" auto-grow hide-details :label="$t('singbox.includeUids')"></v-textarea>
        </v-col>
        <v-col cols="12" sm="6">
          <v-textarea v-model="excludeUidText" rows="2" auto-grow hide-details :label="$t('singbox.excludeUids')"></v-textarea>
        </v-col>
        <v-col cols="12" sm="6">
          <v-textarea v-model="includeUidRangeText" rows="2" auto-grow hide-details :label="$t('singbox.includeUidRanges')"></v-textarea>
        </v-col>
        <v-col cols="12" sm="6">
          <v-textarea v-model="excludeUidRangeText" rows="2" auto-grow hide-details :label="$t('singbox.excludeUidRanges')"></v-textarea>
        </v-col>
      </v-row>
      <v-row>
        <v-col cols="12" class="v-card-subtitle">{{ $t('singbox.advanced') }}</v-col>
        <v-col cols="12" sm="6">
          <v-textarea v-model="includeInterfaceText" rows="2" auto-grow hide-details :label="$t('singbox.includeInterfaces')"></v-textarea>
        </v-col>
        <v-col cols="12" sm="6">
          <v-textarea v-model="excludeInterfaceText" rows="2" auto-grow hide-details :label="$t('singbox.excludeInterfaces')"></v-textarea>
        </v-col>
        <v-col cols="12" sm="6" md="4">
          <v-text-field v-model.number="data.iproute2_table_index" type="number" min="0" hide-details :label="$t('singbox.iproute2TableIndex')"></v-text-field>
        </v-col>
        <v-col cols="12" sm="6" md="4">
          <v-text-field v-model.number="data.iproute2_rule_index" type="number" min="0" hide-details :label="$t('singbox.iproute2RuleIndex')"></v-text-field>
        </v-col>
        <v-col cols="12" sm="6">
          <v-textarea v-model="loopbackAddressText" rows="2" auto-grow hide-details :label="$t('singbox.loopbackAddresses')"></v-textarea>
        </v-col>
      </v-row>
      <v-row>
        <v-col cols="12" class="v-card-subtitle">{{ $t('singbox.httpProxy') }}</v-col>
        <v-col cols="12" sm="6" md="4">
          <v-switch v-model="httpProxyEnabled" color="primary" :label="$t('singbox.enablePlatformHttpProxy')" hide-details></v-switch>
        </v-col>
        <template v-if="data.platform?.http_proxy">
          <v-col cols="12" sm="6" md="4">
            <v-text-field v-model="data.platform.http_proxy.server" hide-details :label="$t('out.addr')"></v-text-field>
          </v-col>
          <v-col cols="12" sm="6" md="4">
            <v-text-field v-model.number="data.platform.http_proxy.server_port" type="number" min="0" max="65535" hide-details :label="$t('out.port')"></v-text-field>
          </v-col>
          <v-col cols="12" sm="6">
            <v-textarea v-model="httpProxyBypassText" rows="2" auto-grow hide-details :label="$t('singbox.bypassDomains')"></v-textarea>
          </v-col>
          <v-col cols="12" sm="6">
            <v-textarea v-model="httpProxyMatchText" rows="2" auto-grow hide-details :label="$t('singbox.matchDomains')"></v-textarea>
          </v-col>
        </template>
      </v-row>
    </template>
  </v-card>
</template>

<script lang="ts" src="./Tun.logic"></script>
