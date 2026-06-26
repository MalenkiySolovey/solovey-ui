<template>
  <entity-drawer
    :dirty="dirty"
    :loading="loading"
    :model-value="visible"
    :saving="loading"
    :title="$t('actions.' + title) + ' ' + $t('objects.service')"
    :width="720"
    @close="closeModal"
    @save="saveChanges"
  >
    <form-section icon="lucide:sliders-horizontal" :title="$t('form.sections.configuration')">
      <v-row>
        <v-col cols="12" sm="6">
          <v-select
            hide-details
            :label="$t('type')"
            :items="Object.keys(srvTypes).map((key,index) => ({title: key, value: Object.values(srvTypes)[index]}))"
            v-model="srv.type"
            @update:modelValue="changeType">
          </v-select>
        </v-col>
        <v-col cols="12" sm="6">
          <v-text-field v-model="srv.tag" :label="$t('objects.tag')" hide-details></v-text-field>
        </v-col>
      </v-row>

      <Listen v-if="!NoListen.includes(srv.type)" :data="srv" :inTags="inTags" />
      <Derp v-if="srv.type == srvTypes.DERP" :data="srv" :inTags="inTags" :tsTags="tsTags" />
      <SSMapi v-if="srv.type == srvTypes.SSMAPI" :data="srv" :ssTags="ssTags" />
      <OomKiller v-if="srv.type == srvTypes.OOMKiller" :data="srv" />
      <InTLS v-if="HasTls.includes(srv.type)"  :inbound="srv" :tlsConfigs="tlsConfigs" :tls_id="srv.tls_id" />
    </form-section>
  </entity-drawer>
</template>

<script lang="ts" src="./ServiceDrawer.logic"></script>
