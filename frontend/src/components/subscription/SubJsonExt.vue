<template>
  <Editor
    v-model="enableEditor"
    :data="settings.subJsonExt"
    :visible="enableEditor"
    :title="$t('editor') + ' - ' + $t('setting.jsonSub')"
    @close="enableEditor = false"
    @save="saveEditor"
    />
  <v-card>
    <v-row>
      <v-col cols="12" sm="6" md="3">
        <v-select
          v-model="ruleToDirect"
          :items="geoList"
          :label="$t('setting.toDirect')"
          multiple
          chips
          hide-details
        ></v-select>
      </v-col>
      <v-col cols="12" sm="6" md="3">
        <v-select
          v-model="ruleToBlock"
          :items="geoList"
          :label="$t('setting.toBlock')"
          multiple
          chips
          hide-details
        ></v-select>
      </v-col>
    </v-row>
    <v-row  v-if="enableLog">
      <v-col cols="12" sm="6" md="3" lg="2">
        <v-select
          hide-details
          :label="$t('basic.log.level')"
          :items="levels"
          v-model="subJsonExt.log.level">
        </v-select>
      </v-col>
      <v-col cols="12" sm="6" md="3" lg="2">
        <v-switch v-model="subJsonExt.log.timestamp" color="primary" :label="$t('setting.timestamp')" hide-details />
      </v-col>
    </v-row>
    <v-row v-if="enableDns">
      <v-col cols="12" sm="6" md="3" lg="2">
        <v-select
          hide-details
          :label="$t('dns.final')"
          :items="dnsTags"
          v-model="subJsonExt.dns.final">
        </v-select>
      </v-col>
      <v-col cols="12" sm="6" md="3" lg="2">
        <SimpleDNS :data="proxyDns" :label="$t('setting.globalDns')" />
      </v-col>
      <v-col cols="12" sm="6" md="3" lg="2">
        <SimpleDNS :data="directDns" :label="$t('setting.directDns')" />
      </v-col>
    </v-row>
    <v-row v-if="enableDns">
      <v-col cols="12" sm="6" md="3" lg="2">
        <v-select
          hide-details
          :label="$t('basic.routing.defaultDns')"
          :items="dnsTags"
          clearable
          @click:clear="delete subJsonExt.default_domain_resolver"
          v-model="subJsonExt.default_domain_resolver">
        </v-select>
      </v-col>
      <v-col cols="12" sm="6" md="3">
        <v-select
          v-model="dnsToDirect"
          :items="geositeList"
          :label="$t('setting.toDirectDns')"
          multiple
          chips
          hide-details
        ></v-select>
      </v-col>
    </v-row>
    <template v-if="enableInb">
      <v-row>
        <v-col cols="12" sm="6" md="3">
          <v-combobox
            v-model="inbounds[0].address"
            :items="defaultInb[0].address"
            chips
            multiple
            hide-details
            :label="$t('in.addr')"
          ></v-combobox>
        </v-col>
        <v-col cols="12" sm="6" md="3" lg="2">
          <v-text-field
            type="number"
            v-model.number="inbounds[0].mtu"
            hide-details
            label="MTU"
          ></v-text-field>
        </v-col>
      </v-row>
      <v-row>
        <v-col cols="12" sm="6" md="3">
          <v-combobox
            v-model="inbounds[0].exclude_package"
            :items="['ir.mci.ecareapp','com.myirancell']"
            chips
            multiple
            hide-details
            :label="$t('setting.excludePkg')"
          ></v-combobox>
        </v-col>
        <v-col cols="12" sm="6" md="3" lg="2">
          <v-switch
            v-model="platformProxy"
            hide-details
            color="primary"
            label="Platform HTTP proxy"
          ></v-switch>
        </v-col>
      </v-row>
    </template>
    <v-card-actions>
      <v-spacer></v-spacer>
      <v-btn @click="openEditor" variant="outlined" hide-details>{{ $t('editor') }}</v-btn>
      <v-menu v-model="menu" :close-on-content-click="false" location="start">
        <template v-slot:activator="{ props }">
          <v-btn v-bind="props" hide-details variant="tonal">{{ $t('setting.jsonSubOptions') }}</v-btn>
        </template>
        <v-card>
          <v-list>
            <v-list-item>
              <v-switch v-model="enableLog" color="primary" :label="$t('basic.log.title')" hide-details></v-switch>
            </v-list-item>
            <v-list-item>
              <v-switch v-model="enableDns" color="primary" label="DNS" hide-details></v-switch>
            </v-list-item>
            <v-list-item>
              <v-switch v-model="enableInb" color="primary" :label="$t('objects.inbound')" hide-details></v-switch>
            </v-list-item>
            <v-list-item>
              <v-switch v-model="enableExp" color="primary" label="Experimental" hide-details></v-switch>
            </v-list-item>
          </v-list>
        </v-card>
      </v-menu>
    </v-card-actions>
  </v-card>
</template>

<script lang="ts" src="./SubJsonExt.logic"></script>
