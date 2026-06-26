<template>
  <v-card :subtitle="$t('objects.tls')">
    <v-row v-if="tlsOptional">
      <v-col cols="12" sm="6" md="4">
        <v-switch color="primary" :label="$t('tls.enable')" v-model="tlsEnable" hide-details></v-switch>
      </v-col>
    </v-row>
    <template v-if="tls.enabled">
      <v-row>
        <v-col cols="12" sm="6" md="4">
          <v-switch color="primary" :label="$t('tls.disableSni')" v-model="disable_sni" hide-details></v-switch>
        </v-col>
        <v-col cols="12" sm="6" md="4">
          <v-switch color="primary" :label="$t('tls.insecure')" v-model="insecure" hide-details></v-switch>
        </v-col>
      </v-row>
      <template v-if="optionCert">
        <v-row>
          <v-col cols="auto">
            <v-btn-toggle v-model="usePath"
            class="rounded-xl"
            density="compact"
            variant="outlined"
            shaped
            mandatory>
              <v-btn
                @click="tls.certificate=undefined; tls.certificate_path=''"
              >{{ $t('tls.usePath') }}</v-btn>
              <v-btn
                @click="tls.certificate_path=undefined; tls.certificate=''"
              >{{ $t('tls.useText') }}</v-btn>
            </v-btn-toggle>
          </v-col>
        </v-row>
        <v-row v-if="usePath == 0">
          <v-col cols="12" sm="6">
            <v-text-field
              :label="$t('tls.certPath')"
              hide-details
              v-model="tls.certificate_path">
            </v-text-field>
          </v-col>
        </v-row>
        <v-row v-else>
          <v-col cols="12" sm="6">
            <v-textarea
              :label="$t('tls.cert')"
              hide-details
              v-model="tls.certificate">
            </v-textarea>
          </v-col>
        </v-row>
      </template>
      <v-row>
        <v-col cols="12" sm="6" md="4" v-if="tls.server_name != undefined">
          <v-text-field
            label="SNI"
            hide-details
            v-model="tls.server_name">
          </v-text-field>
        </v-col>
        <v-col cols="12" sm="6" md="4" v-if="tls.alpn">
          <v-select
            hide-details
            label="ALPN"
            multiple
            :items="alpn"
            v-model="tls.alpn">
          </v-select>
        </v-col>
      </v-row>
      <v-row>
        <v-col cols="12" sm="6" md="4" v-if="tls.min_version">
          <v-select
            hide-details
            :label="$t('tls.minVer')"
            :items="tlsVersions"
            v-model="tls.min_version">
          </v-select>
        </v-col>
        <v-col cols="12" sm="6" md="4" v-if="tls.max_version">
          <v-select
            hide-details
            :label="$t('tls.maxVer')"
            :items="tlsVersions"
            v-model="tls.max_version">
          </v-select>
        </v-col>
      </v-row>
      <v-row v-if="tls.cipher_suites != undefined">
        <v-col cols="12" md="8">
          <v-select
            hide-details
            :label="$t('tls.cs')"
            multiple
            :items="cipher_suites"
            v-model="tls.cipher_suites">
          </v-select>
        </v-col>
      </v-row>
      <v-row v-if="tls.curve_preferences != undefined">
        <v-col cols="12" md="8">
          <v-select
            hide-details
            :label="$t('tls.curves')"
            multiple
            chips
            :items="curvePreferences"
            v-model="tls.curve_preferences">
          </v-select>
        </v-col>
      </v-row>
      <v-row v-if="tls.certificate_public_key_sha256 != undefined">
        <v-col cols="12">
          <v-textarea
            :label="$t('tls.certPubKeySha256')"
            rows="2"
            no-resize
            hide-details
            v-model="certificatePublicKeySha256">
          </v-textarea>
        </v-col>
      </v-row>
      <template v-if="optionClientCert">
        <v-row>
          <v-col cols="12" sm="6">
            <v-text-field
              :label="$t('tls.clientCertPath')"
              hide-details
              v-model="tls.client_certificate_path">
            </v-text-field>
          </v-col>
          <v-col cols="12" sm="6">
            <v-text-field
              :label="$t('tls.clientKeyPath')"
              hide-details
              v-model="tls.client_key_path">
            </v-text-field>
          </v-col>
          <v-col cols="12" sm="6">
            <v-textarea
              :label="$t('tls.clientCert')"
              rows="3"
              no-resize
              hide-details
              v-model="clientCertificateText">
            </v-textarea>
          </v-col>
          <v-col cols="12" sm="6">
            <v-textarea
              :label="$t('tls.clientKey')"
              rows="3"
              no-resize
              hide-details
              v-model="clientKeyText">
            </v-textarea>
          </v-col>
        </v-row>
      </template>
      <v-row v-if="tls.utls != undefined">
        <v-col cols="12" md="6">
          <v-select
            hide-details
            label="Fingerprint"
            :items="fingerprints"
            v-model="tls.utls.fingerprint">
          </v-select>
        </v-col>
      </v-row>
      <v-row v-if="tls.reality != undefined">
        <v-col cols="12" md="6">
          <v-text-field
          :label="$t('tls.pubKey')"
            hide-details
            v-model="tls.reality.public_key">
          </v-text-field>
        </v-col>
        <v-col cols="12" md="4">
          <v-text-field
            label="Short ID"
            hide-details
            v-model="tls.reality.short_id">
          </v-text-field>
        </v-col>
      </v-row>
      <template v-if="tls.ech != undefined">
        <v-row>
          <v-col class="v-card-subtitle">ECH</v-col>
        </v-row>
        <v-row>
          <v-col cols="auto">
            <v-btn-toggle v-model="useEchPath"
            class="rounded-xl"
            density="compact"
            variant="outlined"
            shaped
            mandatory>
              <v-btn
                @click="delete tls.ech?.config"
              >{{ $t('tls.usePath') }}</v-btn>
              <v-btn
                @click="delete tls.ech?.config_path"
              >{{ $t('tls.useText') }}</v-btn>
            </v-btn-toggle>
          </v-col>
        </v-row>
        <v-row v-if="useEchPath == 0">
          <v-col cols="12" sm="6">
            <v-text-field
              :label="$t('tls.certPath')"
              hide-details
              v-model="tls.ech.config_path">
            </v-text-field>
          </v-col>
          <v-col cols="12" sm="6">
            <v-text-field
              :label="$t('tls.queryServerName')"
              hide-details
              v-model="tls.ech.query_server_name"
              placeholder="ech.example.com">
            </v-text-field>
          </v-col>
        </v-row>
        <v-row v-else>
          <v-col cols="12" sm="6">
            <v-textarea
              :label="$t('tls.cert')"
              hide-details
              v-model="echConfigText">
            </v-textarea>
          </v-col>
          <v-col cols="12" sm="6">
            <v-text-field
              :label="$t('tls.queryServerName')"
              hide-details
              v-model="tls.ech.query_server_name"
              placeholder="ech.example.com">
            </v-text-field>
          </v-col>
        </v-row>
      </template>
      <v-row v-if="tls.fragment != undefined">
        <v-col cols="12" sm="6" md="4">
          <v-switch color="primary" :label="$t('tls.fragment')" v-model="tls.fragment" hide-details></v-switch>
        </v-col>
        <v-col cols="12" sm="6" md="4" v-if="tls.fragment">
          <v-switch color="primary" :label="$t('tls.recordFragment')" v-model="tls.record_fragment" hide-details></v-switch>
        </v-col>
        <v-col cols="12" sm="6" md="4" v-if="tls.fragment">
          <v-text-field
          :label="$t('tls.fragmentDelay')"
          hide-details
          type="number"
          min=0
          :suffix="$t('date.ms')"
          v-model.number="fragmentFallbackDelay">
          </v-text-field>
        </v-col>
      </v-row>
      <v-row v-if="optionKtls">
        <v-col cols="12" sm="6" md="4">
          <v-switch color="primary" :label="$t('tls.kernelTx')" v-model="tls.kernel_tx" hide-details></v-switch>
        </v-col>
        <v-col cols="12" sm="6" md="4">
          <v-switch color="primary" :label="$t('tls.kernelRx')" v-model="tls.kernel_rx" hide-details></v-switch>
        </v-col>
      </v-row>
    </template>
    <v-card-actions v-if="tls.enabled">
      <v-spacer></v-spacer>
      <v-menu v-model="menu" :close-on-content-click="false" location="start">
          <template v-slot:activator="{ props }">
            <v-btn v-bind="props" hide-details variant="tonal">{{ $t('tls.options') }}</v-btn>
          </template>
          <v-card>
            <v-list>
              <v-list-item>
                <v-switch v-model="optionCert" color="primary" :label="$t('tls.cert')" hide-details></v-switch>
              </v-list-item>
              <v-list-item>
                <v-switch v-model="optionSNI" color="primary" label="SNI" hide-details></v-switch>
              </v-list-item>
              <v-list-item>
                <v-switch v-model="optionALPN" color="primary" label="ALPN" hide-details></v-switch>
              </v-list-item>
              <v-list-item>
                <v-switch v-model="optionMinV" color="primary" :label="$t('tls.minVer')" hide-details></v-switch>
              </v-list-item>
              <v-list-item>
                <v-switch v-model="optionMaxV" color="primary" :label="$t('tls.maxVer')" hide-details></v-switch>
              </v-list-item>
              <v-list-item>
                <v-switch v-model="optionCS" color="primary" :label="$t('tls.cs')" hide-details></v-switch>
              </v-list-item>
              <v-list-item>
                <v-switch v-model="optionCurve" color="primary" :label="$t('tls.curves')" hide-details></v-switch>
              </v-list-item>
              <v-list-item>
                <v-switch v-model="optionCertPin" color="primary" :label="$t('tls.certPubKeySha256')" hide-details></v-switch>
              </v-list-item>
              <v-list-item>
                <v-switch v-model="optionClientCert" color="primary" :label="$t('tls.clientCertificate')" hide-details></v-switch>
              </v-list-item>
              <v-list-item>
                <v-switch v-model="optionFP" color="primary" label="UTLS" hide-details></v-switch>
              </v-list-item>
              <v-list-item>
                <v-switch v-model="optionReality" color="primary" label="Reality" hide-details></v-switch>
              </v-list-item>
              <v-list-item>
                <v-switch v-model="optionEch" color="primary" label="ECH" hide-details></v-switch>
              </v-list-item>
              <v-list-item>
                <v-switch v-model="optionFragment" color="primary" :label="$t('tls.fragment')" hide-details></v-switch>
              </v-list-item>
              <v-list-item>
                <v-switch v-model="optionKtls" color="primary" :label="$t('tls.ktls')" hide-details></v-switch>
              </v-list-item>
            </v-list>
          </v-card>
        </v-menu>
    </v-card-actions>
  </v-card>
</template>

<script lang="ts" src="./OutTLS.logic"></script>
