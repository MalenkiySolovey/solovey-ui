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
      <v-col cols="12" sm="6" md="4" v-if="optionQueryType">
        <StrictSelect
          v-model="rule.query_type"
          :items="queryTypes"
          :label="$t('dns.rule.queryType')"
          multiple
          chips
          hide-details>
        </StrictSelect>
      </v-col>
      <v-col cols="12" sm="6" md="4" v-if="optionNetwork">
        <v-select
          hide-details
          multiple
          chips
          :label="$t('network')"
          :items="['tcp','udp']"
          v-model="rule.network">
        </v-select>
      </v-col>
      <v-col cols="12" sm="6" v-if="optionProtocol">
        <StrictSelect
          v-model="rule.protocol"
          :items="['http','tls', 'quic', 'stun', 'dns']"
          :label="$t('protocol')"
          multiple
          chips
          hide-details
        />
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
        <v-textarea
          v-model="domain"
          :label="$t('rule.domain')"
          rows="5"
          no-resize
          density="compact"
          append-icon="mdi-arrow-expand"
          hide-details
          @click:append="openExpTextarea($t('rule.domain'), 'domain')"
        />
      </v-col>
      <v-col cols="12" sm="6" v-if="rule.domain_suffix != undefined">
        <v-textarea
          v-model="domain_suffix"
          :label="$t('rule.domainSufix')"
          rows="5"
          no-resize
          density="compact"
          append-icon="mdi-arrow-expand"
          hide-details
          @click:append="openExpTextarea($t('rule.domainSufix'), 'domain_suffix')"
        />
      </v-col>
      <v-col cols="12" sm="6" v-if="rule.domain_keyword != undefined">
        <v-textarea
          v-model="domain_keyword"
          :label="$t('rule.domainKw')"
          rows="5"
          no-resize
          density="compact"
          append-icon="mdi-arrow-expand"
          hide-details
          @click:append="openExpTextarea($t('rule.domainKw'), 'domain_keyword')"
        />
      </v-col>
      <v-col cols="12" sm="6" v-if="rule.domain_regex != undefined">
        <v-textarea
          v-model="domain_regex"
          :label="$t('rule.domainRgx')"
          rows="5"
          no-resize
          density="compact"
          append-icon="mdi-arrow-expand"
          hide-details
          @click:append="openExpTextarea($t('rule.domainRgx'), 'domain_regex')"
        />
      </v-col>
      <v-col cols="12" sm="6" v-if="rule.ip_cidr != undefined">
        <v-textarea
          v-model="ip_cidr"
          :label="$t('rule.ip')"
          rows="5"
          no-resize
          density="compact"
          append-icon="mdi-arrow-expand"
          hide-details
          @click:append="openExpTextarea($t('rule.ip'), 'ip_cidr')"
        />
      </v-col>
      <v-col cols="12" sm="6" v-if="rule.ip_is_private != undefined">
        <v-switch v-model="rule.ip_is_private" color="primary" :label="$t('rule.privateIp')" hide-details></v-switch>
      </v-col>
      <v-col cols="12" sm="6" v-if="rule.ip_accept_any != undefined">
        <v-switch v-model="rule.ip_accept_any" color="primary" :label="$t('dns.rule.ipAcceptAny')" hide-details></v-switch>
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
        <v-textarea
          v-model="port"
          :label="$t('rule.port')"
          rows="5"
          no-resize
          density="compact"
          append-icon="mdi-arrow-expand"
          hide-details
          @click:append="openExpTextarea($t('rule.port'), 'port')"
        />
      </v-col>
      <v-col cols="12" sm="6" v-if="rule.port_range != undefined">
        <v-textarea
          v-model="port_range"
          :label="$t('rule.portRange')"
          rows="5"
          no-resize
          density="compact"
          append-icon="mdi-arrow-expand"
          hide-details
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
        <v-textarea
          v-model="source_ip_cidr"
          :label="$t('rule.srcCidr')"
          rows="5"
          no-resize
          density="compact"
          append-icon="mdi-arrow-expand"
          hide-details
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
        <v-textarea
          v-model="source_port"
          :label="$t('rule.srcPort')"
          rows="5"
          no-resize
          density="compact"
          append-icon="mdi-arrow-expand"
          hide-details
          @click:append="openExpTextarea($t('rule.srcPort'), 'source_port')"
        />
      </v-col>
      <v-col cols="12" sm="6" v-if="rule.source_port_range != undefined">
        <v-textarea
          v-model="source_port_range"
          :label="$t('rule.srcPortRange')"
          rows="5"
          no-resize
          density="compact"
          append-icon="mdi-arrow-expand"
          hide-details
          @click:append="openExpTextarea($t('rule.srcPortRange'), 'source_port_range')"
        />
      </v-col>
    </v-row>
    <v-row v-if="optionRuleSet">
      <v-col cols="12" sm="6">
        <StrictSelect
          v-model="rule.rule_set"
          :items="ruleSets"
          :label="$t('rule.ruleset')"
          multiple
          chips
          hide-details
        />
      </v-col>
      <v-col cols="12" sm="6">
        <v-switch v-model="rule.rule_set_ip_cidr_match_source" color="primary" :label="$t('rule.rulesetMatchSrc')" hide-details></v-switch>
      </v-col>
      <v-col cols="12" sm="6">
        <v-switch v-model="rule.rule_set_ip_cidr_accept_empty" color="primary" :label="$t('dns.rule.rulesetAcceptEmpty')" hide-details></v-switch>
      </v-col>
    </v-row>
    <RuleNetworkState v-if="optionNetworkState" :rule="rule" />
    <RuleInterfaceAddress v-if="optionInterface" :rule="rule" />
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
              <v-switch v-model="optionQueryType" color="primary" :label="$t('dns.rule.queryType')" hide-details></v-switch>
            </v-list-item>
            <v-list-item>
              <v-switch v-model="optionNetwork" color="primary" :label="$t('network')" hide-details></v-switch>
            </v-list-item>
            <v-list-item>
              <v-switch v-model="optionProtocol" color="primary" :label="$t('protocol')" hide-details></v-switch>
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

<script lang="ts" src="./DnsRule.logic.ts"></script>
