<template>
  <form-shell
    :dirty="dirty"
    :loading="loading"
    :title="$t('actions.' + title) + ' ' + $t('objects.rule')"
    @close="closeModal"
    @save="saveChanges"
  >
        <v-row>
          <v-col cols="12" sm="6" md="4">
            <v-switch color="primary" v-model="logical" :label="$t('rule.logical')" hide-details></v-switch>
          </v-col>
          <v-spacer></v-spacer>
          <v-col cols="auto" v-if="logical" justify="center" align="center">
            <v-btn color="primary" @click="ruleData.rules.push({})" hide-details>{{ $t('actions.add') + " " + $t('objects.rule') }}</v-btn>
          </v-col>
        </v-row>
        <v-card style="background-color: inherit; margin-bottom: 5px;" v-for="(r, index) in ruleData.rules" :key="ruleObjectKey(r)" v-if="ruleData.type == 'logical'">
          <v-card-subtitle>{{ $t('objects.rule') + ' ' + (Number(index)+1) }}
            <v-icon @click="ruleData.rules.splice(index,1)" icon="mdi-delete" v-if="ruleData.rules.length>1" />
          </v-card-subtitle>
          <v-card-text style="padding: 0;">
            <RuleOptions
              :rule="r"
              :clients="clients"
              :inTags="inTags"
              :outTags="outTags"
              :rsTags="rsTags" />
          </v-card-text>
        </v-card>
        <RuleOptions
          v-else
          :rule="ruleData.rules[0]"
          :clients="clients"
          :inTags="inTags"
          :outTags="outTags"
          :rsTags="rsTags" />
        <v-row>
          <v-col cols="12" sm="6" md="4">
            <v-select
              v-model="ruleData.action"
              :items="actions"
              :label="$t('admin.action')"
              hide-details
            ></v-select>
          </v-col>
          <v-col cols="12" sm="6" md="4" v-if="logical">
            <v-select
              v-model="ruleData.mode"
              :items="['and', 'or']"
              :label="$t('rule.mode')"
              hide-details
            ></v-select>
          </v-col>
          <v-col cols="12" sm="6" md="4">
            <v-switch color="primary" v-model="ruleData.invert" :label="$t('rule.invert')" hide-details></v-switch>
          </v-col>
        </v-row>
        <v-card :subtitle="$t(`rule.action.${ruleData.action == 'route-options' ? 'routeOption' : ruleData.action}`)" v-if="['route', 'route-options', 'bypass'].includes(ruleData.action)">
          <v-row>
            <v-col cols="12" sm="6" md="4" v-if="['route', 'bypass'].includes(ruleData.action)">
              <v-select
                v-model="ruleData.outbound"
                :items="outTags"
                :label="$t('objects.outbound')"
                :clearable="ruleData.action == 'bypass'"
                @click:clear="delete ruleData.outbound"
                hide-details
              ></v-select>
            </v-col>
            <v-col cols="12" sm="6" md="4">
              <v-text-field v-model="ruleData.override_address" :label="$t('types.direct.overrideAddr')" hide-details></v-text-field>
            </v-col>
            <v-col cols="12" sm="6" md="4">
              <v-text-field
                v-model.number="ruleData.override_port"
                type="number"
                min="0"
                max="65534"
                :label="$t('types.direct.overridePort')"
                hide-details>
              </v-text-field>
            </v-col>
            <v-col cols="12" sm="6" md="4">
              <v-switch v-model="ruleData.udp_disable_domain_unmapping" :label="$t('rule.udpDisableDomainUnmapping')" hide-details></v-switch>
            </v-col>
            <v-col cols="12" sm="6" md="4">
              <v-switch v-model="ruleData.udp_connect" :label="$t('rule.udpConnect')" hide-details></v-switch>
            </v-col>
            <v-col cols="12" sm="6" md="4">
              <v-text-field v-model="ruleData.udp_timeout" :label="$t('rule.udpTimeout')" hide-details></v-text-field>
            </v-col>
            <v-col cols="12" sm="6" md="4">
              <v-select
                v-model="ruleData.network_strategy"
                :items="networkStrategies"
                :label="$t('rule.strategy')"
                clearable
                @click:clear="delete ruleData.network_strategy"
                hide-details>
              </v-select>
            </v-col>
            <v-col cols="12" sm="6" md="4">
              <v-text-field
                v-model.number="ruleData.fallback_delay"
                :label="$t('rule.fallbackDelay')"
                type="number"
                min="0"
                :suffix="$t('date.ms')"
                hide-details>
              </v-text-field>
            </v-col>
            <v-col cols="12" sm="6" md="4">
              <v-switch v-model="tlsRecordFragment" :label="$t('singbox.tlsRecordFragment')" hide-details></v-switch>
            </v-col>
            <v-col cols="12" sm="6" md="4">
              <v-switch v-model="tlsFragment" :label="$t('singbox.tlsFragment')" hide-details></v-switch>
            </v-col>
            <v-col cols="12" sm="6" md="4" v-if="ruleData.tls_fragment">
              <v-text-field
                v-model="ruleData.tls_fragment_fallback_delay"
                :label="$t('singbox.tlsFragmentFallbackDelay')"
                placeholder="500ms"
                hide-details>
              </v-text-field>
            </v-col>
          </v-row>
        </v-card>
        <v-card :subtitle="$t('rule.action.reject')" v-if="ruleData.action == 'reject'">
          <v-row>
            <v-col cols="12" sm="6" md="4">
              <v-select
                v-model="ruleData.method"
                :items="[{ title: 'Default', value: 'default' },{ title: 'Drop', value: 'drop'}, { title: 'Reply', value: 'reply' }]"
                :label="$t('rule.method')"
                clearable
                @click:clear="delete ruleData.method"
                hide-details>
            </v-select>
            </v-col>
            <v-col cols="12" sm="6" md="4">
              <v-switch v-model="ruleData.no_drop" :label="$t('rule.noDrop')" hide-details></v-switch>
            </v-col>
          </v-row>
        </v-card>
        <v-card :subtitle="$t('rule.action.sniff')" v-if="ruleData.action == 'sniff'">
          <v-row>
            <v-col cols="12" sm="6" md="4">
              <v-select
                v-model="ruleData.sniffer"
                :items="sniffers"
                :label="$t('rule.sniffer')"
                multiple
                chips
                hide-details>
              </v-select>
            </v-col>
            <v-col cols="12" sm="6" md="4">
              <v-text-field v-model="ruleData.timeout" :label="$t('rule.timeout')" hide-details></v-text-field>
            </v-col>
          </v-row>
        </v-card>
        <v-card :subtitle="$t('rule.action.resolve')" v-if="ruleData.action == 'resolve'">
          <v-row>
            <v-col cols="12" sm="6" md="4">
              <v-select
                v-model="ruleData.strategy"
                :items="domainStrategies"
                :label="$t('rule.strategy')"
                clearable
                @click:clear="delete ruleData.strategy"
                hide-details>
              </v-select>
            </v-col>
            <v-col cols="12" sm="6" md="4">
              <v-text-field v-model="ruleData.server" :label="$t('basic.dns.server')" hide-details></v-text-field>
            </v-col>
          </v-row>
        </v-card>
  </form-shell>
</template>

<script lang="ts" src="./Rule.logic"></script>
