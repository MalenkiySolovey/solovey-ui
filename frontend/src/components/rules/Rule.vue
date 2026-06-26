<template>
  <ExpTextarea
    v-model="expTextarea.visible"
    :visible="expTextarea.visible"
    :label="expTextarea.title"
    :content="expTextarea.content"
    @update="saveExpTextarea"
    @close="closeExpTextarea"
  />
  <v-card style="background-color: inherit;">
    <v-row>
      <v-col cols="12" v-if="optionInbound">
        <StrictSelect
          v-model="rule.inbound"
          :items="inTags"
          :label="$t('pages.inbounds')"
          multiple
          chips
          hide-details
        />
      </v-col>
      <v-col cols="12" v-if="optionClient">
        <StrictSelect
          v-model="rule.auth_user"
          :items="clients"
          :label="$t('pages.clients')"
          multiple
          chips
          hide-details
        />
      </v-col>
      <v-col cols="12" sm="6" md="4" v-if="optionIPver">
        <v-select
          hide-details
          :label="$t('rule.ipVer')"
          :items="[4,6]"
          v-model.number="rule.ip_version">
        </v-select>
      </v-col>
      <v-col cols="12" sm="6" md="4" v-if="optionNetwork">
        <v-select
          hide-details
          multiple
          chips
          :label="$t('network')"
          :items="['tcp','udp','icmp']"
          v-model="rule.network">
        </v-select>
      </v-col>
      <v-col cols="12" sm="6" v-if="optionProtocol">
        <v-select
          v-model="rule.protocol"
          :items="protocols"
          :label="$t('protocol')"
          multiple
          chips
          hide-details
        ></v-select>
      </v-col>
      <v-col cols="12" sm="6" v-if="optionSniffClient">
        <v-select
          v-model="rule.client"
          :items="sniffClients"
          label="Client fingerprint"
          multiple
          chips
          hide-details
        ></v-select>
      </v-col>
    </v-row>
    <v-row v-if="optionDomain">
      <v-col cols="12" sm="6" md="4">
        <v-select
          hide-details
          :items="domainKeys"
          @update:model-value="updateDomainOption($event)"
          v-model="domainOption">
        </v-select>
      </v-col>
      <v-col cols="12" sm="6" v-if="rule.domain != undefined">
        <v-textarea :label="$t('rule.domain')"
          hide-details
          v-model="domain"
          rows="5"
          no-resize
          density="compact"
          append-icon="mdi-arrow-expand"
          @click:append="openExpTextarea($t('rule.domain'), 'domain')"
        />
      </v-col>
      <v-col cols="12" sm="6" v-if="rule.domain_suffix != undefined">
        <v-textarea :label="$t('rule.domainSufix')"
          hide-details
          v-model="domain_suffix"
          rows="5"
          no-resize
          density="compact"
          append-icon="mdi-arrow-expand"
          @click:append="openExpTextarea($t('rule.domainSufix'), 'domain_suffix')"
        />
      </v-col>
      <v-col cols="12" sm="6" v-if="rule.domain_keyword != undefined">
        <v-textarea :label="$t('rule.domainKw')"
          hide-details
          v-model="domain_keyword"
          rows="5"
          no-resize
          density="compact"
          append-icon="mdi-arrow-expand"
          @click:append="openExpTextarea($t('rule.domainKw'), 'domain_keyword')"
        />
      </v-col>
      <v-col cols="12" sm="6" v-if="rule.domain_regex != undefined">
        <v-textarea :label="$t('rule.domainRgx')"
          hide-details
          v-model="domain_regex"
          rows="5"
          no-resize
          density="compact"
          append-icon="mdi-arrow-expand"
          @click:append="openExpTextarea($t('rule.domainRgx'), 'domain_regex')"
        />
      </v-col>
      <v-col cols="12" sm="6" v-if="rule.ip_cidr != undefined">
        <v-textarea :label="$t('rule.ip')"
          hide-details
          v-model="ip_cidr"
          rows="5"
          no-resize
          density="compact"
          append-icon="mdi-arrow-expand"
          @click:append="openExpTextarea($t('rule.ip'), 'ip_cidr')"
        />
      </v-col>
      <v-col cols="12" sm="6" v-if="rule.ip_is_private != undefined">
        <v-switch v-model="rule.ip_is_private" color="primary" :label="$t('rule.privateIp')" hide-details></v-switch>
      </v-col>
    </v-row>
    <v-row v-if="optionPort">
      <v-col cols="12" sm="6" md="4">
        <v-select
          hide-details
          :items="portKeys"
          @update:model-value="updatePortOption($event)"
          v-model="portOption">
        </v-select>
      </v-col>
      <v-col cols="12" sm="6" v-if="rule.port != undefined">
        <v-textarea :label="$t('rule.port')"
          hide-details
          v-model="port"
          rows="5"
          no-resize
          density="compact"
          append-icon="mdi-arrow-expand"
          @click:append="openExpTextarea($t('rule.port'), 'port')"
        />
      </v-col>
      <v-col cols="12" sm="6" v-if="rule.port_range != undefined">
        <v-textarea :label="$t('rule.portRange')"
          hide-details
          v-model="port_range"
          rows="5"
          no-resize
          density="compact"
          append-icon="mdi-arrow-expand"
          @click:append="openExpTextarea($t('rule.portRange'), 'port_range')"
        />
      </v-col>
    </v-row>
    <v-row v-if="optionSrcIP">
      <v-col cols="12" sm="6" md="4">
        <v-select
          hide-details
          :items="srcIPKeys"
          @update:model-value="updateSrcIPOption($event)"
          v-model="srcIPOption">
        </v-select>
      </v-col>
      <v-col cols="12" sm="6" v-if="rule.source_ip_cidr != undefined">
        <v-textarea :label="$t('rule.srcCidr')"
          hide-details
          v-model="source_ip_cidr"
          rows="5"
          no-resize
          density="compact"
          append-icon="mdi-arrow-expand"
          @click:append="openExpTextarea($t('rule.srcCidr'), 'source_ip_cidr')"
        />
      </v-col>
      <v-col cols="12" sm="6" v-if="rule.source_ip_is_private != undefined">
        <v-switch v-model="rule.source_ip_is_private" color="primary" :label="$t('rule.srcPrivateIp')" hide-details></v-switch>
      </v-col>
    </v-row>
    <v-row v-if="optionSrcPort">
      <v-col cols="12" sm="6" md="4">
        <v-select
          hide-details
          :items="srcPortKeys"
          @update:model-value="updateSrcPortOption($event)"
          v-model="srcPortOption">
        </v-select>
      </v-col>
      <v-col cols="12" sm="6" v-if="rule.source_port != undefined">
        <v-textarea :label="$t('rule.srcPort')"
          hide-details
          v-model="source_port"
          rows="5"
          no-resize
          density="compact"
          append-icon="mdi-arrow-expand"
          @click:append="openExpTextarea($t('rule.srcPort'), 'source_port')"
        />
      </v-col>
      <v-col cols="12" sm="6" v-if="rule.source_port_range != undefined">
        <v-textarea :label="$t('rule.srcPortRange')"
          hide-details
          v-model="source_port_range"
          rows="5"
          no-resize
          density="compact"
          append-icon="mdi-arrow-expand"
          @click:append="openExpTextarea($t('rule.srcPortRange'), 'source_port_range')"
        />
      </v-col>
    </v-row>
    <v-row v-if="optionPreferredBy">
      <v-col cols="12" sm="6">
        <StrictSelect
          v-model="rule.preferred_by"
          :items="outTags || inTags"
          :label="$t('rule.preferredBy')"
          multiple
          chips
          hide-details
        />
      </v-col>
    </v-row>
    <RuleNetworkState v-if="optionNetworkState" :rule="rule" />
    <RuleInterfaceAddress v-if="optionInterface" :rule="rule" />
    <v-row v-if="optionRuleSet">
      <v-col cols="12" sm="6">
        <StrictSelect
          v-model="rule.rule_set"
          :items="rsTags"
          :label="$t('rule.ruleset')"
          multiple
          chips
          hide-details
        />
      </v-col>
      <v-col cols="12" sm="6">
        <v-switch v-model="rule.rule_set_ip_cidr_match_source" color="primary" :label="$t('rule.rulesetMatchSrc')" hide-details></v-switch>
      </v-col>
    </v-row>
    <v-card-actions>
      <v-spacer></v-spacer>
      <v-menu v-model="menu" :close-on-content-click="false" location="start">
        <template v-slot:activator="{ props }">
          <v-btn v-bind="props" hide-details variant="tonal">{{ $t('rule.options') }}</v-btn>
        </template>
        <v-card>
          <v-list>
            <v-list-item>
              <v-switch v-model="optionInbound" color="primary" :label="$t('pages.inbounds')" hide-details></v-switch>
            </v-list-item>
            <v-list-item>
              <v-switch v-model="optionClient" color="primary" :label="$t('pages.clients')" hide-details></v-switch>
            </v-list-item>
            <v-list-item>
              <v-switch v-model="optionIPver" color="primary" :label="$t('rule.ipVer')" hide-details></v-switch>
            </v-list-item>
            <v-list-item>
              <v-switch v-model="optionNetwork" color="primary" :label="$t('network')" hide-details></v-switch>
            </v-list-item>
            <v-list-item>
              <v-switch v-model="optionProtocol" color="primary" :label="$t('protocol')" hide-details></v-switch>
            </v-list-item>
            <v-list-item>
              <v-switch v-model="optionSniffClient" color="primary" label="Client fingerprint" hide-details></v-switch>
            </v-list-item>
            <v-list-item>
              <v-switch v-model="optionDomain" color="primary" :label="$t('rule.domainRules')" hide-details></v-switch>
            </v-list-item>
            <v-list-item>
              <v-switch v-model="optionPort" color="primary" :label="$t('in.port')" hide-details></v-switch>
            </v-list-item>
            <v-list-item>
              <v-switch v-model="optionSrcIP" color="primary" :label="$t('rule.srcIpRules')" hide-details></v-switch>
            </v-list-item>
            <v-list-item>
              <v-switch v-model="optionSrcPort" color="primary" :label="$t('rule.srcPortRules')" hide-details></v-switch>
            </v-list-item>
            <v-list-item>
              <v-switch v-model="optionPreferredBy" color="primary" :label="$t('rule.preferredBy')" hide-details></v-switch>
            </v-list-item>
            <v-list-item>
              <v-switch v-model="optionNetworkState" color="primary" :label="$t('rule.networkState')" hide-details></v-switch>
            </v-list-item>
            <v-list-item>
              <v-switch v-model="optionInterface" color="primary" :label="$t('rule.interfaceAddr')" hide-details></v-switch>
            </v-list-item>
            <v-list-item>
              <v-switch v-model="optionRuleSet" color="primary" :label="$t('rule.ruleset')" hide-details></v-switch>
            </v-list-item>
          </v-list>
        </v-card>
      </v-menu>
    </v-card-actions>
  </v-card>
</template>

<script lang="ts" src="./Rule.logic.ts"></script>
