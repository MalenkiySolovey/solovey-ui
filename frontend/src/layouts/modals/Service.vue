<template>
  <v-dialog transition="dialog-bottom-transition" width="800">
    <v-card class="rounded-lg">
      <v-card-title>
        {{ $t('actions.' + title) + " " + $t('objects.service') }}
      </v-card-title>
      <v-divider></v-divider>
      <v-card-text style="padding: 0 16px; overflow-y: scroll;">
        <v-row>
          <v-col cols="12" sm="6" md="4">
            <v-select
            hide-details
            :label="$t('type')"
            :items="Object.keys(srvTypes).map((key,index) => ({title: key, value: Object.values(srvTypes)[index]}))"
            v-model="srv.type"
            @update:modelValue="changeType">
            </v-select>
          </v-col>
          <v-col cols="12" sm="6" md="4">
            <v-text-field v-model="srv.tag" :label="$t('objects.tag')" hide-details></v-text-field>
          </v-col>
        </v-row>

        <Listen v-if="!NoListen.includes(srv.type)" :data="srv" :inTags="inTags" />
        <Derp v-if="srv.type == srvTypes.DERP" :data="srv" :inTags="inTags" :tsTags="tsTags" />
        <SSMapi v-if="srv.type == srvTypes.SSMAPI" :data="srv" :ssTags="ssTags" />
        <OomKiller v-if="srv.type == srvTypes.OOMKiller" :data="srv" />
        <InTLS v-if="HasTls.includes(srv.type)"  :inbound="srv" :tlsConfigs="tlsConfigs" :tls_id="srv.tls_id" />
      </v-card-text>
      <v-card-actions>
        <v-spacer></v-spacer>
        <v-btn
          color="primary"
          variant="outlined"
          @click="closeModal"
        >
          {{ $t('actions.close') }}
        </v-btn>
        <v-btn
          color="primary"
          variant="tonal"
          :loading="loading"
          :disabled="loading"
          @click="saveChanges"
        >
          {{ $t('actions.save') }}
        </v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>
</template>

<script lang="ts" src="./Service.logic.ts"></script>
