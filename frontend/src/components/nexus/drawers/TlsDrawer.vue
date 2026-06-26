<template>
  <entity-drawer
    :dirty="dirty"
    :loading="loading"
    :model-value="visible"
    :saving="loading"
    :title="$t('actions.' + title) + ' ' + $t('objects.tls')"
    :width="720"
    @close="closeModal"
    @save="saveChanges"
  >
    <v-card class="rounded-lg">
      <v-row>
        <v-col cols="12" sm="6" md="4">
          <v-text-field :label="$t('client.name')" hide-details v-model="tls.name"></v-text-field>
        </v-col>
        <v-col align="end">
          <v-btn-toggle v-model="tlsType"
          class="rounded-xl"
          density="compact"
          variant="outlined"
          @update:model-value="changeTlsType"
          shaped
          mandatory>
            <v-btn>TLS</v-btn>
            <v-btn>Reality</v-btn>
          </v-btn-toggle>
        </v-col>
      </v-row>
      <v-row>
        <v-col cols="12" sm="6" md="4" v-if="inTls.server_name != undefined">
          <v-text-field label="SNI" hide-details v-model="inTls.server_name"></v-text-field>
        </v-col>
        <template v-if="tlsType == 0">
          <v-col cols="12" sm="6" md="4" v-if="inTls.min_version">
            <v-select hide-details :label="$t('tls.minVer')" :items="tlsVersions" v-model="inTls.min_version"></v-select>
          </v-col>
          <v-col cols="12" sm="6" md="4" v-if="inTls.max_version">
            <v-select hide-details :label="$t('tls.maxVer')" :items="tlsVersions" v-model="inTls.max_version"></v-select>
          </v-col>
          <v-col cols="12" sm="6" md="4" v-if="inTls.alpn">
            <v-select hide-details label="ALPN" multiple :items="alpn" v-model="inTls.alpn"></v-select>
          </v-col>
          <v-col cols="12" md="8" v-if="inTls.cipher_suites != undefined">
            <v-select hide-details :label="$t('tls.cs')" multiple :items="cipher_suites" v-model="inTls.cipher_suites"></v-select>
          </v-col>
          <v-col cols="12" md="8" v-if="inTls.curve_preferences != undefined">
            <v-select hide-details :label="$t('tls.curves')" multiple chips :items="curvePreferences" v-model="inTls.curve_preferences"></v-select>
          </v-col>
        </template>
      </v-row>
      <template v-if="tlsType == 0">
        <v-row>
          <v-col>
            <v-btn-toggle v-model="usePath"
            class="rounded-xl"
            density="compact"
            variant="outlined"
            shaped
            mandatory>
              <v-btn @click="inTls.key=undefined; inTls.certificate=undefined">{{ $t('tls.usePath') }}</v-btn>
              <v-btn @click="inTls.key_path=undefined; inTls.certificate_path=undefined">{{ $t('tls.useText') }}</v-btn>
            </v-btn-toggle>
          </v-col>
          <v-spacer></v-spacer>
          <v-col cols="auto">
            <v-btn variant="tonal" density="compact" icon="mdi-key-star" :aria-label="$t('actions.generate')" :title="$t('actions.generate')" @click="genSelfSigned" :loading="loading" />
          </v-col>
        </v-row>
        <v-row v-if="usePath == 0">
          <v-col cols="12" sm="6">
            <v-text-field :label="$t('tls.certPath')" hide-details v-model="inTls.certificate_path"></v-text-field>
          </v-col>
          <v-col cols="12" sm="6">
            <v-text-field :label="$t('tls.keyPath')" hide-details v-model="inTls.key_path"></v-text-field>
          </v-col>
        </v-row>
        <v-row v-else>
          <v-col cols="12">
            <v-textarea :label="$t('tls.cert')" hide-details v-model="certText"></v-textarea>
          </v-col>
          <v-col cols="12">
            <v-textarea :label="$t('tls.key')" hide-details v-model="keyText"></v-textarea>
          </v-col>
        </v-row>
        <v-row>
          <v-col cols="12" sm="6" md="4">
            <v-switch color="primary" :label="$t('tls.disableSni')" v-model="disableSni" hide-details></v-switch>
          </v-col>
          <v-col cols="12" sm="6" md="4">
            <v-switch color="primary" :label="$t('tls.insecure')" v-model="insecure" hide-details></v-switch>
          </v-col>
        </v-row>
        <template v-if="optionClientAuth">
          <v-row>
            <v-col cols="12" sm="6" md="4">
              <v-select hide-details :label="$t('tls.clientAuthentication')" :items="clientAuthTypes" v-model="inTls.client_authentication"></v-select>
            </v-col>
            <v-col cols="12" sm="6">
              <v-textarea :label="$t('tls.clientCertPubKeySha256')" rows="2" no-resize hide-details v-model="clientCertificatePublicKeySha256"></v-textarea>
            </v-col>
            <v-col cols="12" sm="6">
              <v-text-field :label="$t('tls.clientCertPath')" hide-details v-model="clientCertificatePath"></v-text-field>
            </v-col>
            <v-col cols="12">
              <v-textarea :label="$t('tls.clientCert')" rows="3" no-resize hide-details v-model="clientCertificateText"></v-textarea>
            </v-col>
          </v-row>
        </template>
      </template>
      <template v-if="outTls.reality && inTls.reality">
        <v-row>
          <v-col cols="12" sm="6" md="4">
            <v-text-field :label="$t('types.shdwTls.hs')" hide-details v-model="inTls.reality.handshake.server"></v-text-field>
          </v-col>
          <v-col cols="12" sm="6" md="4">
            <v-text-field :label="$t('out.port')" type="number" min="0" hide-details v-model="server_port"></v-text-field>
          </v-col>
          <v-spacer></v-spacer>
          <v-col cols="auto">
            <v-btn variant="tonal" density="compact" icon="mdi-key-star" :aria-label="$t('actions.generate')" :title="$t('actions.generate')" @click="genRealityKey" :loading="loading" />
          </v-col>
        </v-row>
        <v-row>
          <v-col cols="12">
            <v-text-field :label="$t('tls.privKey')" hide-details v-model="inTls.reality.private_key"></v-text-field>
          </v-col>
          <v-col cols="12">
            <v-text-field :label="$t('tls.pubKey')" hide-details v-model="outTls.reality.public_key"></v-text-field>
          </v-col>
          <v-col cols="12">
            <v-text-field label="Short IDs" hide-details append-icon="mdi-refresh" @click:append="randomSID" v-model="short_id"></v-text-field>
          </v-col>
          <v-col cols="12" sm="6" md="4" v-if="optionTime">
            <v-text-field label="Max Time Diference" type="number" min="1" :suffix="$t('date.m')" hide-details v-model="max_time"></v-text-field>
          </v-col>
        </v-row>
      </template>
      <v-row v-if="optionStore || optionKtls">
        <v-col cols="12" sm="6" md="4" v-if="optionStore">
          <v-select hide-details :label="$t('tls.store')" :items="storeItems" v-model="inTls.store"></v-select>
        </v-col>
        <template v-if="optionKtls">
          <v-col cols="12" sm="6" md="4">
            <v-switch color="primary" :label="$t('tls.kernelTx')" v-model="inTls.kernel_tx" hide-details></v-switch>
          </v-col>
          <v-col cols="12" sm="6" md="4">
            <v-switch color="primary" :label="$t('tls.kernelRx')" v-model="inTls.kernel_rx" hide-details></v-switch>
          </v-col>
        </template>
      </v-row>
      <v-row v-if="outTls.utls != undefined">
        <v-col cols="12" sm="6" md="4">
          <v-select hide-details label="Fingerprint" :items="fingerprints" v-model="outTls.utls.fingerprint"></v-select>
        </v-col>
      </v-row>
      <v-card-actions>
        <v-spacer></v-spacer>
        <v-menu v-model="menu" :close-on-content-click="false" location="start">
          <template v-slot:activator="{ props }">
            <v-btn v-bind="props" variant="tonal">{{ $t('tls.options') }}</v-btn>
          </template>
          <v-card>
            <v-list>
              <template v-if="tlsType == 0">
                <v-list-item><v-switch v-model="optionSNI" color="primary" label="SNI" hide-details></v-switch></v-list-item>
                <v-list-item><v-switch v-model="optionALPN" color="primary" label="ALPN" hide-details></v-switch></v-list-item>
                <v-list-item><v-switch v-model="optionMinV" color="primary" :label="$t('tls.minVer')" hide-details></v-switch></v-list-item>
                <v-list-item><v-switch v-model="optionMaxV" color="primary" :label="$t('tls.maxVer')" hide-details></v-switch></v-list-item>
                <v-list-item><v-switch v-model="optionCS" color="primary" :label="$t('tls.cs')" hide-details></v-switch></v-list-item>
                <v-list-item><v-switch v-model="optionCurve" color="primary" :label="$t('tls.curves')" hide-details></v-switch></v-list-item>
                <v-list-item><v-switch v-model="optionClientAuth" color="primary" :label="$t('tls.clientAuthentication')" hide-details></v-switch></v-list-item>
                <v-list-item><v-switch v-model="optionFP" color="primary" label="UTLS" hide-details></v-switch></v-list-item>
                <v-list-item><v-switch v-model="optionStore" color="primary" :label="$t('tls.store')" hide-details></v-switch></v-list-item>
                <v-list-item><v-switch v-model="optionKtls" color="primary" :label="$t('tls.ktls')" hide-details></v-switch></v-list-item>
              </template>
              <template v-else>
                <v-list-item><v-switch v-model="optionTime" color="primary" label="Max Time Difference" hide-details></v-switch></v-list-item>
              </template>
            </v-list>
          </v-card>
        </v-menu>
      </v-card-actions>
    </v-card>
    <AcmeVue :tls="inTls" />
    <EchVue :iTls="inTls" :oTls="outTls" />
  </entity-drawer>
</template>

<script lang="ts" src="./TlsDrawer.logic"></script>
