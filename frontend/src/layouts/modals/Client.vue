<template>
  <form-shell
    :dirty="dirty"
    :loading="loading"
    :title="$t('actions.' + title) + ' ' + $t('objects.client')"
    @close="closeModal"
    @save="saveChanges"
  >
        <v-container style="padding: 0;" :hidden="loading">
          <v-tabs
            v-model="tab"
            align-tabs="center"
          >
            <v-tab value="t1">{{ $t('client.basics') }}</v-tab>
            <v-tab value="t2">{{ $t('client.config') }}</v-tab>
            <v-tab value="t3">{{ $t('client.links') }}</v-tab>
          </v-tabs>
          <v-window v-model="tab">
            <v-window-item value="t1">
              <v-row>
                <v-col cols="12" sm="6" md="4">
                  <v-switch color="primary" v-model="client.enable" :label="$t('enable')" hide-details></v-switch>
                </v-col>
                <v-col cols="12" sm="6" md="4">
                  <v-combobox v-model="client.group" :items="groups" :label="$t('client.group')" hide-details></v-combobox>
                </v-col>
              </v-row>
              <v-row>
                <v-col cols="12" sm="6" md="4">
                  <v-text-field v-model="client.name" :label="$t('client.name')" hide-details></v-text-field>
                </v-col>
                <v-col cols="12" sm="6" md="4">
                  <v-text-field v-model="client.desc" :label="$t('client.desc')" hide-details></v-text-field>
                </v-col>
              </v-row>
              <v-row>
                <v-col cols="12" sm="6" md="4">
                  <v-text-field v-model.number="Volume" type="number" min="0" :label="$t('stats.volume')" suffix="GiB" hide-details></v-text-field>
                </v-col>
                <v-col cols="12" sm="6" md="4" v-if="!(client.delayStart && !client.autoReset)">
                  <DatePick :expiry="expDate" @submit="setDate" />
                </v-col>
                <v-col cols="12" sm="6" md="4" v-if="client.autoReset || client.delayStart">
                  <v-text-field v-model.number="resetDays" type="number" min="1" :label="$t('client.resetDays')" hide-details></v-text-field>
                </v-col>
              </v-row>
              <v-row>
                <v-col cols="12" sm="6" md="4">
                  <v-switch color="primary"
                    :disabled="client.up+client.down>0"
                    v-model="delayStart"
                    :label="$t('client.delayStart')" hide-details>
                  </v-switch>
                </v-col>
                <v-col cols="12" sm="6" md="4">
                  <v-switch color="primary" v-model="autoReset" :label="$t('client.autoReset')" hide-details></v-switch>
                </v-col>
              </v-row>
              <v-row v-if="id > 0">
                <v-col cols="12" sm="6" md="4" class="d-flex flex-column">
                  <div class="d-flex justify-space-between align-center">
                    <div>
                      {{ $t('stats.usage') }}: {{ total }}<sup dir="ltr" v-if="percent>0">({{ percent }}%)</sup>
                    </div>
                    <v-btn density="compact" variant="text" icon="mdi-restore" @click="resetUsage">
                      <v-tooltip activator="parent" location="top">
                        {{ $t('reset') }}
                      </v-tooltip>
                      <v-icon />
                    </v-btn>
                  </div>
                  <v-progress-linear
                    v-model="percent"
                    :color="percentColor"
                    v-if="client.volume>0"
                    bottom
                  >
                  </v-progress-linear>
                </v-col>
                <v-col cols="12" sm="6" md="4">
                  <v-icon icon="mdi-upload" color="orange" /><span class="text-orange">{{ up }}</span>
                  / 
                  <v-icon icon="mdi-download" color="success" /><span class="text-success">{{ down }}</span>
                </v-col>
              </v-row>
              <v-row v-if="id >0 && client.autoReset">
                <v-col cols="12" sm="6" md="4">
                  <div class="text-medium-emphasis">{{ $t('client.nextReset') }}</div>
                  <div dir="ltr">{{ nextResetFormatted }}</div>
                </v-col>
                <v-col cols="12" sm="6" md="4">
                  <div class="text-medium-emphasis">{{ $t('main.stats.totalUsage') }}</div>
                  <div>
                    <v-icon icon="mdi-upload" color="orange" /><span class="text-orange">{{ totalUp }}</span>
                    /
                    <v-icon icon="mdi-download" color="success" /><span class="text-success">{{ totalDown }}</span>
                  </div>
                </v-col>
              </v-row>
              <v-row>
                <v-col cols="12" sm="6" md="4">
                  <v-text-field
                    v-model.number="client.limitIp"
                    type="number"
                    min="0"
                    :label="$t('client.limitIp')"
                    hide-details
                  ></v-text-field>
                </v-col>
                <v-col cols="12" sm="6" md="4">
                  <v-select
                    v-model="client.ipLimitMode"
                    :items="ipLimitModes"
                    :label="$t('client.ipLimitMode')"
                    hide-details
                  ></v-select>
                </v-col>
                <v-col cols="12" sm="6" md="4" v-if="client.ipLimitMode === 'enforce'">
                  <v-alert density="compact" type="warning" variant="tonal">
                    {{ $t('client.ipLimitWarn') }}
                  </v-alert>
                </v-col>
              </v-row>
              <v-row>
                <v-col>
                  <StrictSelect
                    v-model="clientInbounds"
                    :items="inboundTags"
                    :label="$t('client.inboundTags')"
                    clearable
                    multiple
                    chips
                    hide-details>
                    <template v-slot:append>
                      <v-icon @click="setAllInbounds" icon="mdi-set-all" v-tooltip:top="$t('all')" />
                    </template>
                  </StrictSelect>
                </v-col>
              </v-row>
            </v-window-item>
            <v-window-item value="t2">
              <v-row>
                <v-col cols="12" sm="6" md="4">
                  <v-btn variant="tonal" @click="shuffle()">{{ $t('reset') + ' - ' + $t('all') }}<v-icon icon="mdi-refresh" /></v-btn>
                </v-col>
              </v-row>
              <v-row v-for="key in Object.keys(clientConfig)">
                <v-col cols="12" md="3" align="end" align-self="center">
                    {{ key }}
                    <v-icon @click="shuffle(key)" icon="mdi-refresh" v-tooltip:top="$t('reset')" />
                </v-col>
                <v-col>
                  <v-text-field
                    v-if="clientConfig[key].password != undefined"
                    label="Password"
                    v-model="clientConfig[key].password"
                    hide-details>
                  </v-text-field>
                  <v-text-field
                    v-if="clientConfig[key].uuid != undefined"
                    label="UUID"
                    v-model="clientConfig[key].uuid"
                    hide-details>
                  </v-text-field>
                  <v-text-field
                    v-if="key == 'vless'"
                    label="Flow"
                    v-model="clientConfig[key].flow"
                    hide-details>
                  </v-text-field>
                  <v-text-field
                    v-if="key == 'hysteria'"
                    label="Auth"
                    v-model="clientConfig[key].auth_str"
                    hide-details>
                  </v-text-field>
                </v-col>
              </v-row>
            </v-window-item>
            <v-window-item value="t3">
              <v-row v-for="(lnk, index) in links">
                <v-col cols="auto">{{ index + 1 }}</v-col>
                <v-col style="direction: ltr; overflow-y: hidden;">{{ lnk.uri }}</v-col>
              </v-row>
              <v-row>
                <v-col>
                  <v-btn color="primary" @click="extLinks.push({ type: 'external', uri: ''})">{{ $t('actions.add') }} {{ $t('client.external') }}</v-btn>
                </v-col>
              </v-row>
              <v-row v-for="(lnk, index) in extLinks">
                <v-col>
                  <v-text-field
                  dir="ltr"
                  :label="$t('client.external') + ' ' + (index+1)"
                  append-icon="mdi-delete"
                  @click:append="extLinks.splice(index,1)"
                  placeholder="<protocol>://<data>"
                  v-model="lnk.uri" />
                </v-col>
              </v-row>
              <v-row>
                <v-col>
                  <StrictSelect
                    v-model="remoteGroupIds"
                    :items="remoteGroupItems"
                    :label="$t('client.subscriptionTags')"
                    clearable
                    multiple
                    chips
                    hide-details
                  />
                </v-col>
              </v-row>
            </v-window-item>
          </v-window>
        </v-container>
  </form-shell>
</template>

<script lang="ts" src="./Client.logic.ts"></script>
