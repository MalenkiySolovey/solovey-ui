<template>
  <DnsVue
    v-model="dnsModal.visible"
    :visible="dnsModal.visible"
    :index="dnsModal.index"
    :data="dnsModal.data"
    :tsTags="tsTags"
    :rslvdTags="rslvdTags"
    @close="closeDnsModal"
    @save="saveDnsModal"
  />
  <DnsRuleVue
    v-model="dnsRuleModal.visible"
    :visible="dnsRuleModal.visible"
    :index="dnsRuleModal.index"
    :data="dnsRuleModal.data"
    :clients="clients"
    :inTags="inboundTags"
    :serverTags="dnsServerTags"
    :ruleSets="ruleSets"
    @close="closeDnsRuleModal"
    @save="saveDnsRuleModal"
  />
  <RegionalPresetDrawer
    v-model="regionalPresetDrawer"
    :config="appConfig"
    :outbound-tags="outboundTags"
    @apply="applyPresetConfig"
  />
  <page-header
    v-if="nexus"
    :search="search"
    searchable
    :subtitle="subtitle"
    :title="$t('pages.dns')"
    @update:search="search = $event"
  />

  <page-toolbar v-if="nexus">
    <template #secondary-actions>
      <v-btn color="primary" prepend-icon="lucide:plus" variant="flat" @click="showDnsModal(-1)">{{ $t('dns.add') }}</v-btn>
      <v-btn prepend-icon="lucide:plus" variant="text" @click="showDnsRuleModal(-1)">{{ $t('dns.rule.add') }}</v-btn>
      <v-btn prepend-icon="mdi-routes" variant="text" @click="regionalPresetDrawer = true">{{ $t('regionalPresets.open') }}</v-btn>
    </template>
    <template #primary-actions>
      <v-btn variant="tonal" color="warning" @click="saveConfig" :loading="loading" :disabled="stateChange">
        {{ $t('actions.save') }}
      </v-btn>
    </template>
  </page-toolbar>
  <v-row v-else>
    <v-col cols="12" justify="center" align="center">
      <v-btn color="primary" @click="showDnsModal(-1)" style="margin: 0 5px;">{{ $t('dns.add') }}</v-btn>
      <v-btn color="primary" @click="showDnsRuleModal(-1)" style="margin: 0 5px;">{{ $t('dns.rule.add') }}</v-btn>
      <v-btn color="primary" prepend-icon="mdi-routes" @click="regionalPresetDrawer = true" style="margin: 0 5px;">{{ $t('regionalPresets.open') }}</v-btn>
      <v-btn variant="outlined" color="warning" @click="saveConfig" :loading="loading" :disabled="stateChange">
        {{ $t('actions.save') }}
      </v-btn>
    </v-col>
  </v-row>
  <v-row>
    <v-col class="v-card-subtitle" cols="12">{{ $t('pages.basics') }}</v-col>
    <v-col cols="12">
      <v-row>
        <v-col cols="12" sm="6" md="3" lg="2">
          <v-select
            hide-details
            :label="$t('dns.final')"
            :items="[ {title: $t('dns.firstServer'), value: ''}, ...dnsServerTags]"
            v-model="finalDns">
          </v-select>
        </v-col>
        <v-col cols="12" sm="6" md="3" lg="2">
          <v-select
            hide-details
            :label="$t('dns.domainStrategy')"
            clearable
            @click:clear="delete dns.strategy"
            :items="['prefer_ipv4','prefer_ipv6','ipv4_only','ipv6_only']"
            v-model="dns.strategy">
          </v-select>
        </v-col>
        <v-col cols="12" sm="6" md="3" lg="2">
          <v-text-field
            v-model="dns.client_subnet" hide-details
            clearable @click:clear="delete dns.client_subnet"
            :label="$t('dns.rule.action.clientSubnet')"></v-text-field>
        </v-col>
        <v-col cols="auto">
          <v-text-field
            v-model.number="dns.cache_capacity"
            type="number" min="1024" hide-details
            clearable @click:clear="delete dns.cache_capacity"
            :label="$t('dns.cacheCapacity')"></v-text-field>
        </v-col>
        <v-col cols="auto">
          <v-checkbox v-model="dns.disable_cache" hide-details :label="$t('dns.disableCache')" />
        </v-col>
        <v-col cols="auto">
          <v-checkbox v-model="dns.disable_expire" hide-details :label="$t('dns.disableExpire')" />
        </v-col>
        <v-col cols="auto">
          <v-checkbox v-model="dns.independent_cache" hide-details :label="$t('dns.independentCache')" />
        </v-col>
        <v-col cols="auto">
          <v-checkbox v-model="dns.reverse_mapping" hide-details :label="$t('dns.reverseMapping')" />
        </v-col>
      </v-row>
    </v-col>
  </v-row>
  <template v-if="nexus">
    <CollapsibleSectionHeader v-model="dnsServersExpanded" :title="$t('dns.title')" nexus>
      <template #actions>
        <ManualSortButton
          :disabled="(dns.servers?.length ?? 0) < 2"
          density="compact"
          size="small"
          @sort="sortDnsServersByName"
        />
        <BulkSelectionControls
          :active="dnsServerSelectMode"
          :count="selectedDnsServerIndexes.length"
          size="small"
          @delete="deleteSelectedDnsServers"
          @toggle="toggleDnsServerSelectMode"
        />
      </template>
    </CollapsibleSectionHeader>
    <nexus-data-table
      v-if="dnsServersExpanded"
      :columns="serverColumns"
      :drag-disabled="search.trim().length > 0"
      draggable-rows
      :items="dnsServerRows"
      :row-key="(item) => item._index"
      :selectable="dnsServerSelectMode"
      :selected="selectedDnsServerIndexes"
      @update:selected="selectedDnsServerIndexes = $event"
      @row-drop="(dragged, target, position) => moveDnsServerTo(dragged._index, target._index, position)"
      @rows-drop="(dragged, target, position) => moveDnsServersTo(dragged.map(item => item._index), target._index, position)"
    >
      <template #col.tag="{ item }"><span class="dns-nexus__tag">{{ item.tag }}</span></template>
      <template #col.server="{ item }">
        <span v-if="item.server" class="nexus-mono">{{ item.server }}</span>
        <span v-else class="dns-nexus__muted">—</span>
      </template>
      <template #col.server_port="{ item }">
        <span v-if="item.server_port" class="nexus-mono">{{ item.server_port }}</span>
        <span v-else class="dns-nexus__muted">—</span>
      </template>
      <template #col.tls="{ item }">
        <nexus-badge
          v-if="Object.hasOwn(item, 'tls')"
          :label="item.tls?.enabled ? $t('nexus.on') : $t('nexus.off')"
          :variant="item.tls?.enabled ? 'success' : 'secondary'"
        />
        <span v-else class="dns-nexus__muted">—</span>
      </template>
      <template #col.source="{ item }">
        <nexus-badge :label="presetSourceLabel(item)" :variant="presetSourceVariant(item)" />
      </template>
      <template #actions="{ item }">
        <row-actions :actions="serverActions(item)" @action="(key) => handleServerAction(key, item)" />
      </template>
      <template #empty><empty-state compact icon="lucide:network" :title="$t('dns.empty')" /></template>
    </nexus-data-table>
  </template>
  <v-row v-else>
    <v-col class="v-card-subtitle" cols="12">
      <CollapsibleSectionHeader v-model="dnsServersExpanded" :title="$t('dns.title')">
        <template #actions>
          <ManualSortButton
            :disabled="(dns.servers?.length ?? 0) < 2"
            style="margin: 0 5px;"
            @sort="sortDnsServersByName"
          />
          <BulkSelectionControls
            :active="dnsServerSelectMode"
            :count="selectedDnsServerIndexes.length"
            inactive-color="secondary"
            inactive-variant="outlined"
            @delete="deleteSelectedDnsServers"
            @toggle="toggleDnsServerSelectMode"
          />
        </template>
      </CollapsibleSectionHeader>
    </v-col>
    <v-col
      v-if="dnsServersExpanded"
      cols="12"
      sm="4"
      md="3"
      lg="2"
      v-for="(item, index) in <any[]>dns.servers"
      :key="item.id"
      class="manual-drop-grid-cell"
      :class="dnsServerDrag.indicatorClasses(index)"
      :style="dnsServerDrag.indicatorStyles(index)"
      :draggable="false"
      @pointerdown="dnsServerDrag.prepare($event)"
      @dragstart="dnsServerDrag.start($event, index)"
      @dragover="dnsServerDrag.overTarget($event, index, indexKeys(dns.servers), dnsServerSelectMode ? selectedDnsServerIndexes.map(Number) : [], false, 'grid')"
      @dragleave="dnsServerDrag.leaveTarget($event, index)"
      @drop="onDnsServerDrop($event, index)"
      @dragend="dnsServerDrag.clear($event)"
    >
      <ClassicConfigCard
        :delete-open="delDnsOverlay[index] ?? false"
        :rows="[
          { label: $t('dns.server'), value: item.server ?? '-' },
          { label: $t('in.port'), value: item.server_port ?? '-' },
          { label: $t('objects.tls'), value: Object.hasOwn(item, 'tls') ? $t(item.tls?.enabled ? 'enable' : 'disable') : '-' },
          { label: $t('presets.source'), value: presetSourceLabel(item) },
        ]"
        :selected="isDnsServerSelected(index)"
        :select-mode="dnsServerSelectMode"
        :subtitle="item.type"
        :title="item.tag"
        @delete="delDns(index)"
        @edit="showDnsModal(index)"
        @update:delete-open="delDnsOverlay[index] = $event"
        @update:selected="toggleDnsServerSelection(index, Boolean($event))"
      />
    </v-col>
  </v-row>
  <template v-if="nexus">
    <div class="dns-nexus__section-row">
      <div class="dns-nexus__section">{{ $t('dns.rule.title') }}</div>
      <BulkSelectionControls
        :active="dnsRuleSelectMode"
        :count="selectedDnsRuleIndexes.length"
        size="small"
        @delete="deleteSelectedDnsRules"
        @toggle="toggleDnsRuleSelectMode"
      />
    </div>
    <nexus-data-table
      :columns="ruleColumns"
      :drag-disabled="search.trim().length > 0"
      draggable-rows
      :items="dnsRuleRows"
      :row-key="(item) => item._index"
      :paginated="false"
      :selectable="dnsRuleSelectMode"
      :selected="selectedDnsRuleIndexes"
      @update:selected="selectedDnsRuleIndexes = $event"
      @row-drop="(dragged, target, position) => moveDnsRuleTo(dragged._index, target._index, position)"
      @rows-drop="(dragged, target, position) => moveDnsRulesTo(dragged.map(item => item._index), target._index, position)"
    >
      <template #col._index="{ item }">{{ item._index + 1 }}</template>
      <template #col.type="{ item }">{{ item.type != undefined ? $t('rule.logical') + ' (' + item.mode + ')' : $t('rule.simple') }}</template>
      <template #col.server="{ item }">{{ item.server ?? '-' }}</template>
      <template #col.invert="{ item }">{{ $t((item.invert ?? false) ? 'yes' : 'no') }}</template>
      <template #col.source="{ item }">
        <nexus-badge :label="presetSourceLabel(item)" :variant="presetSourceVariant(item)" />
      </template>
      <template #actions="{ item }">
        <row-actions :actions="ruleActions(item)" @action="(key) => handleRuleAction(key, item)" />
      </template>
      <template #empty><empty-state compact icon="lucide:filter" :title="$t('dns.rule.empty')" /></template>
    </nexus-data-table>
  </template>
  <v-row v-else>
    <v-col class="v-card-subtitle" cols="12">
      <div class="dns__section-actions">
        <span>{{ $t('dns.rule.title') }}</span>
        <BulkSelectionControls
          :active="dnsRuleSelectMode"
          :count="selectedDnsRuleIndexes.length"
          inactive-color="secondary"
          inactive-variant="outlined"
          @delete="deleteSelectedDnsRules"
          @toggle="toggleDnsRuleSelectMode"
        />
      </div>
    </v-col>
    <v-col cols="12" sm="4" md="3" lg="2" v-for="(item, index) in <any[]>dnsRules"
      :key="item.id"
      class="manual-drop-grid-cell"
      :class="dnsRuleDrag.indicatorClasses(index)"
      :style="dnsRuleDrag.indicatorStyles(index)"
      :draggable="false"
      @pointerdown="dnsRuleDrag.prepare($event)"
      @dragstart="dnsRuleDrag.start($event, index)"
      @dragover="dnsRuleDrag.overTarget($event, index, indexKeys(dnsRules), dnsRuleSelectMode ? selectedDnsRuleIndexes.map(Number) : [], false, 'grid')"
      @dragleave="dnsRuleDrag.leaveTarget($event, index)"
      @drop="onDnsRuleDrop($event, index)"
      @dragend="dnsRuleDrag.clear($event)"
      >
      <ClassicConfigCard
        :delete-open="delDnsRuleOverlay[index] ?? false"
        :rows="[
          { label: $t('admin.action'), value: item.action },
          { label: $t('dns.server'), value: item.server ?? '-' },
          { label: $t('pages.rules'), value: item.rules ? item.rules.length : Object.keys(item).filter(r => !actionDnsRuleKeys.includes(r)).length },
          { label: $t('rule.invert'), value: $t((item.invert ?? false) ? 'yes' : 'no') },
          { label: $t('presets.source'), value: presetSourceLabel(item) },
        ]"
        :selected="isDnsRuleSelected(index)"
        :select-mode="dnsRuleSelectMode"
        :subtitle="item.type != undefined ? $t('rule.logical') + ' (' + item.mode + ')' : $t('rule.simple')"
        :title="index + 1"
        @delete="delDnsRule(index)"
        @edit="showDnsRuleModal(index)"
        @update:delete-open="delDnsRuleOverlay[index] = $event"
        @update:selected="toggleDnsRuleSelection(index, Boolean($event))"
      />
    </v-col>
  </v-row>
</template>

<script lang="ts" setup>
import ManualSortButton from '@/components/ManualSortButton.vue'
import BulkSelectionControls from '@/shared/ui/BulkSelectionControls.vue'
import ClassicConfigCard from '@/shared/ui/ClassicConfigCard.vue'
import CollapsibleSectionHeader from '@/shared/ui/CollapsibleSectionHeader.vue'
import DnsVue from '@/layouts/modals/Dns.vue'
import DnsRuleVue from '@/layouts/modals/DnsRule.vue'
import RegionalPresetDrawer from '@/components/presets/RegionalPresetDrawer.vue'
import { isPresetManagedItem } from '@/components/presets/routingDnsPresets'
import NexusDataTable from '@/components/nexus/data/NexusDataTable.vue'
import RowActions from '@/components/nexus/data/RowActions.vue'
import NexusBadge from '@/components/nexus/primitives/Badge.vue'
import EmptyState from '@/components/nexus/primitives/EmptyState.vue'
import PageHeader from '@/components/nexus/primitives/PageHeader.vue'
import PageToolbar from '@/components/nexus/primitives/PageToolbar.vue'
import { useDnsPage } from '@/shared/composables/pages/useDnsPage'

const { actionDnsRuleKeys, appConfig, applyPresetConfig, clients, closeDnsModal, closeDnsRuleModal, confirm, delDns, delDnsOverlay, delDnsRule, delDnsRuleOverlay, deleteSelectedDnsRules, deleteSelectedDnsServers, dns, dnsModal, dnsRuleDrag, dnsRuleModal, dnsRuleRows, dnsRuleSelectMode, dnsRules, dnsServerDrag, dnsServerRows, dnsServerSelectMode, dnsServerTags, dnsServersExpanded, finalDns, handleRuleAction, handleServerAction, inboundTags, isDnsRuleSelected, isDnsServerSelected, loading, mode, moveDnsRulesTo, moveDnsServersTo, moveDnsRuleTo, moveDnsServerTo, nexus, onDnsRuleDrop, onDnsServerDrop, outboundTags, presetSourceLabel, regionalPresetDrawer, rslvdTags, ruleActions, ruleColumns, ruleSets, saveConfig, saveDnsModal, saveDnsRuleModal, search, selectedDnsRuleIndexes, selectedDnsServerIndexes, serverActions, serverColumns, showDnsModal, showDnsRuleModal, sortDnsServersByName, stateChange, subtitle, t, toggleDnsRuleSelectMode, toggleDnsRuleSelection, toggleDnsServerSelectMode, toggleDnsServerSelection, tsTags } = useDnsPage()
const indexKeys = (rows: unknown[]): number[] => rows.map((_, rowIndex) => rowIndex)
const presetSourceVariant = (item: any) => isPresetManagedItem(item) ? 'success' : 'secondary'
</script>

<style scoped lang="scss" src="./Dns.scss"></style>
