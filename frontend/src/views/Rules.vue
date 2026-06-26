<template>
  <RulesDialogs :page="page" />
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
    :title="$t('pages.rules')"
    @update:search="search = $event"
  />

  <page-toolbar v-if="nexus">
    <template #secondary-actions>
      <v-btn color="primary" prepend-icon="lucide:plus" variant="flat" @click="showRuleModal(-1)">{{ $t('rule.add') }}</v-btn>
      <v-btn prepend-icon="lucide:plus" variant="text" @click="showRulesetModal(-1)">{{ $t('ruleset.add') }}</v-btn>
      <v-btn prepend-icon="mdi-routes" variant="text" @click="regionalPresetDrawer = true">{{ $t('regionalPresets.open') }}</v-btn>
      <v-menu :close-on-content-click="false" location="bottom center">
        <template v-slot:activator="{ props }">
          <v-btn v-bind="props" :aria-label="$t('rule.import.title')" icon="lucide:wrench" variant="text" />
        </template>
        <v-list density="compact" nav>
          <v-list-item link prepend-icon="lucide:list" :title="$t('rule.import.rulesTitle')" @click="showImportRule" />
          <v-list-item link prepend-icon="lucide:download" :title="$t('rule.import.title')" @click="showImportRulesets" />
        </v-list>
      </v-menu>
    </template>
    <template #primary-actions>
      <v-btn variant="tonal" color="warning" @click="saveConfig" :loading="loading" :disabled="stateChange">
        {{ $t('actions.save') }}
      </v-btn>
    </template>
  </page-toolbar>

  <v-row v-else>
    <v-col cols="12" justify="center" align="center">
      <v-btn color="primary" @click="showRuleModal(-1)" style="margin: 0 5px;">{{ $t('rule.add') }}</v-btn>
      <v-btn color="primary" @click="showRulesetModal(-1)" style="margin: 0 5px;">{{ $t('ruleset.add') }}</v-btn>
      <v-btn color="primary" prepend-icon="mdi-routes" @click="regionalPresetDrawer = true" style="margin: 0 5px;">{{ $t('regionalPresets.open') }}</v-btn>
      <v-menu v-model="actionMenu" :close-on-content-click="false" location="bottom center">
        <template v-slot:activator="{ props }">
          <v-btn v-bind="props" hide-details variant="text" icon>
            <v-icon icon="mdi-tools" color="primary" />
          </v-btn>
        </template>
        <v-list density="compact" nav>
          <v-list-item link @click="showImportRule">
            <template v-slot:prepend>
              <v-icon icon="mdi-routes"></v-icon>
            </template>
            <v-list-item-title v-text="$t('rule.import.rulesTitle')"></v-list-item-title>
          </v-list-item>
          <v-list-item link @click="showImportRulesets">
            <template v-slot:prepend>
              <v-icon icon="mdi-download-multiple"></v-icon>
            </template>
            <v-list-item-title v-text="$t('rule.import.title')"></v-list-item-title>
          </v-list-item>
        </v-list>
      </v-menu>
      <v-btn variant="outlined" color="warning" @click="saveConfig" :loading="loading" :disabled="stateChange">
        {{ $t('actions.save') }}
      </v-btn>
    </v-col>
  </v-row>
  <v-row>
    <v-col class="v-card-subtitle" cols="12">{{ $t('basic.routing.title') }}</v-col>
    <v-col cols="12">
      <v-row>
        <v-col cols="12" sm="6" md="3" lg="2">
          <v-select hide-details :label="$t('basic.routing.defaultOut')" clearable
            @click:clear="delete route.final" :items="outboundTags" v-model="route.final"></v-select>
        </v-col>
        <v-col cols="12" sm="6" md="3" lg="2">
          <v-text-field v-model="route.default_interface" hide-details clearable
            @click:clear="delete route.default_interface" :label="$t('basic.routing.defaultIf')"></v-text-field>
        </v-col>
        <v-col cols="12" sm="6" md="3" lg="2">
          <v-text-field v-model.number="routeMark" hide-details type="number" min="0" :label="$t('basic.routing.defaultRm')"></v-text-field>
        </v-col>
        <v-col cols="12" sm="6" md="3" lg="2">
          <v-switch v-model="route.auto_detect_interface" color="primary" :label="$t('basic.routing.autoBind')" hide-details></v-switch>
        </v-col>
        <v-col cols="12" sm="6" md="3" lg="2">
          <v-select v-model="routePreset" hide-details :label="$t('singbox.routePreset')" :items="routePresets"></v-select>
        </v-col>
        <v-col cols="12" sm="6" md="3" lg="2">
          <v-switch v-model="findProcess" color="primary" :label="$t('singbox.findProcess')" hide-details></v-switch>
        </v-col>
        <v-col cols="12" sm="6" md="3" lg="2">
          <v-switch v-model="overrideAndroidVpn" color="primary" :label="$t('singbox.overrideAndroidVpn')" hide-details></v-switch>
        </v-col>
        <v-col cols="12" v-if="route.override_android_vpn">
          <v-alert density="compact" type="warning" variant="tonal">
            {{ $t('singbox.overrideAndroidVpnWarning') }}
          </v-alert>
        </v-col>
        <v-col cols="12" v-if="route.default_network_strategy && !route.auto_detect_interface">
          <v-alert density="compact" type="warning" variant="tonal">
            {{ $t('singbox.defaultNetworkStrategyRequired') }}
          </v-alert>
        </v-col>
        <v-col cols="12" v-if="route.default_network_strategy && route.default_interface">
          <v-alert density="compact" type="warning" variant="tonal">
            {{ $t('singbox.defaultNetworkStrategyConflict') }}
          </v-alert>
        </v-col>
      </v-row>
      <DomainResolver :data="route" field="default_domain_resolver" :label="$t('singbox.defaultDomainResolver')" />
      <v-row v-if="route.default_network_strategy">
        <v-col cols="12" sm="6" md="3" lg="2">
          <v-select
            v-model="routeDefaultNetworkStrategy"
            hide-details
            clearable
            @click:clear="routeDefaultNetworkStrategy = undefined"
            :label="$t('singbox.defaultNetworkStrategy')"
            :items="['fallback', 'hybrid']">
          </v-select>
        </v-col>
        <v-col cols="12" sm="6" md="3" lg="2">
          <v-select
            v-model="route.default_network_type"
            hide-details multiple chips closable-chips
            :label="$t('singbox.defaultNetworkType')"
            :items="networkTypes">
          </v-select>
        </v-col>
        <v-col cols="12" sm="6" md="3" lg="2" v-if="route.default_network_strategy == 'fallback'">
          <v-select
            v-model="route.default_fallback_network_type"
            hide-details multiple chips closable-chips
            :label="$t('singbox.defaultFallbackNetworkType')"
            :items="networkTypes">
          </v-select>
        </v-col>
        <v-col cols="12" sm="6" md="3" lg="2">
          <v-text-field
            v-model="defaultFallbackDelayMs"
            hide-details
            type="number"
            min="1"
            suffix="ms"
            :label="$t('singbox.defaultFallbackDelay')">
          </v-text-field>
        </v-col>
      </v-row>
    </v-col>
  </v-row>
  <template v-if="nexus">
    <CollapsibleSectionHeader v-model="rulesetsExpanded" :title="$t('rule.ruleset')" nexus>
      <template #actions>
        <ManualSortButton
          :disabled="rulesets.length < 2"
          density="compact"
          size="small"
          @sort="sortRulesetsByName"
        />
        <BulkSelectionControls
          :active="rulesetSelectMode"
          :count="selectedRulesetIndexes.length"
          size="small"
          @delete="deleteSelectedRulesets"
          @toggle="toggleRulesetSelectMode"
        />
      </template>
    </CollapsibleSectionHeader>
    <nexus-data-table
      v-if="rulesetsExpanded"
      :columns="rulesetColumns"
      :drag-disabled="search.trim().length > 0"
      draggable-rows
      :items="rulesetRows"
      :row-key="(item) => item._index"
      :selectable="rulesetSelectMode"
      :selected="selectedRulesetIndexes"
      @update:selected="selectedRulesetIndexes = $event"
      @row-drop="(dragged, target, position) => moveRulesetTo(dragged._index, target._index, position)"
      @rows-drop="(dragged, target, position) => moveRulesetsTo(dragged.map(item => item._index), target._index, position)"
    >
      <template #col.tag="{ item }"><span class="rules-nexus__tag">{{ item.tag }}</span></template>
      <template #col.type="{ item }">{{ $t('ruleset.' + item.type) }}</template>
      <template #col.download_detour="{ item }">{{ item.download_detour ?? '-' }}</template>
      <template #col.update_interval="{ item }">{{ item.update_interval ?? '-' }}</template>
      <template #col.source="{ item }">
        <nexus-badge :label="presetSourceLabel(item)" :variant="presetSourceVariant(item)" />
      </template>
      <template #actions="{ item }">
        <row-actions :actions="rulesetActions(item)" @action="(key) => handleRulesetAction(key, item)" />
      </template>
      <template #empty><empty-state compact icon="lucide:list" :title="$t('ruleset.empty')" /></template>
    </nexus-data-table>
  </template>
  <v-row v-else>
    <v-col class="v-card-subtitle" cols="12">
      <CollapsibleSectionHeader v-model="rulesetsExpanded" :title="$t('rule.ruleset')">
        <template #actions>
          <ManualSortButton
            :disabled="rulesets.length < 2"
            style="margin: 0 5px;"
            @sort="sortRulesetsByName"
          />
          <BulkSelectionControls
            :active="rulesetSelectMode"
            :count="selectedRulesetIndexes.length"
            inactive-color="secondary"
            inactive-variant="outlined"
            @delete="deleteSelectedRulesets"
            @toggle="toggleRulesetSelectMode"
          />
        </template>
      </CollapsibleSectionHeader>
    </v-col>
    <v-col
      v-if="rulesetsExpanded"
      cols="12"
      sm="4"
      md="3"
      lg="2"
      v-for="(item, index) in <any[]>rulesets"
      :key="item.tag"
      class="manual-drop-grid-cell"
      :class="rulesetDrag.indicatorClasses(index)"
      :style="rulesetDrag.indicatorStyles(index)"
      :draggable="false"
      @pointerdown="rulesetDrag.prepare($event)"
      @dragstart="rulesetDrag.start($event, index)"
      @dragover="rulesetDrag.overTarget($event, index, indexKeys(rulesets), rulesetSelectMode ? selectedRulesetIndexes.map(Number) : [], false, 'grid')"
      @dragleave="rulesetDrag.leaveTarget($event, index)"
      @drop="onRulesetDrop($event, index)"
      @dragend="rulesetDrag.clear($event)"
    >
      <ClassicConfigCard
        :delete-open="delRulesetOverlay[index] ?? false"
        :rows="[
          { label: $t('ruleset.format'), value: item.format },
          { label: $t('objects.outbound'), value: item.download_detour ?? '-' },
          { label: $t('actions.update'), value: item.update_interval ?? '-' },
          { label: $t('presets.source'), value: presetSourceLabel(item) },
        ]"
        :selected="isRulesetSelected(index)"
        :select-mode="rulesetSelectMode"
        :subtitle="$t('ruleset.' + item.type)"
        :title="item.tag"
        @delete="delRuleset(index)"
        @edit="showRulesetModal(index)"
        @update:delete-open="delRulesetOverlay[index] = $event"
        @update:selected="toggleRulesetSelection(index, Boolean($event))"
      />
    </v-col>
  </v-row>
  <template v-if="nexus">
    <div class="rules-nexus__section-row">
      <div class="rules-nexus__section">{{ $t('pages.rules') }}</div>
      <BulkSelectionControls
        :active="ruleSelectMode"
        :count="selectedRuleIndexes.length"
        size="small"
        @delete="deleteSelectedRules"
        @toggle="toggleRuleSelectMode"
      />
    </div>
    <nexus-data-table
      :columns="ruleColumns"
      :drag-disabled="search.trim().length > 0"
      draggable-rows
      :items="ruleRows"
      :row-key="(item) => item._index"
      :paginated="false"
      :selectable="ruleSelectMode"
      :selected="selectedRuleIndexes"
      @update:selected="selectedRuleIndexes = $event"
      @row-drop="(dragged, target, position) => moveRuleTo(dragged._index, target._index, position)"
      @rows-drop="(dragged, target, position) => moveRulesTo(dragged.map(item => item._index), target._index, position)"
    >
      <template #col._index="{ item }">{{ item._index + 1 }}</template>
      <template #col.type="{ item }">{{ item.type != undefined ? $t('rule.logical') + ' (' + item.mode + ')' : $t('rule.simple') }}</template>
      <template #col.outbound="{ item }">{{ item.outbound ?? '-' }}</template>
      <template #col.invert="{ item }">{{ $t((item.invert ?? false) ? 'yes' : 'no') }}</template>
      <template #col.source="{ item }">
        <nexus-badge :label="presetSourceLabel(item)" :variant="presetSourceVariant(item)" />
      </template>
      <template #actions="{ item }">
        <row-actions :actions="ruleActions(item)" @action="(key) => handleRuleAction(key, item)" />
      </template>
      <template #empty><empty-state compact icon="lucide:list" :title="$t('rule.empty')" /></template>
    </nexus-data-table>
  </template>
  <v-row v-else>
    <v-col class="v-card-subtitle" cols="12">
      <div class="rules__section-actions">
        <span>{{ $t('pages.rules') }}</span>
        <BulkSelectionControls
          :active="ruleSelectMode"
          :count="selectedRuleIndexes.length"
          inactive-color="secondary"
          inactive-variant="outlined"
          @delete="deleteSelectedRules"
          @toggle="toggleRuleSelectMode"
        />
      </div>
    </v-col>
    <v-col
      cols="12"
      sm="4"
      md="3"
      lg="2"
      v-for="(item, index) in <any[]>rules"
      :key="item.id"
      class="manual-drop-grid-cell"
      :class="ruleDrag.indicatorClasses(index)"
      :style="ruleDrag.indicatorStyles(index)"
      :draggable="false"
      @pointerdown="ruleDrag.prepare($event)"
      @dragstart="ruleDrag.start($event, index)"
      @dragover="ruleDrag.overTarget($event, index, indexKeys(rules), ruleSelectMode ? selectedRuleIndexes.map(Number) : [], false, 'grid')"
      @dragleave="ruleDrag.leaveTarget($event, index)"
      @drop="onRuleDrop($event, index)"
      @dragend="ruleDrag.clear($event)"
    >
      <ClassicConfigCard
        :delete-open="delRuleOverlay[index] ?? false"
        :rows="[
          { label: $t('admin.action'), value: item.action },
          { label: $t('objects.outbound'), value: item.outbound ?? '-' },
          { label: $t('pages.rules'), value: item.rules ? item.rules.length : Object.keys(item).filter(r => !actionKeys.includes(r)).length },
          { label: $t('rule.invert'), value: $t((item.invert ?? false) ? 'yes' : 'no') },
          { label: $t('presets.source'), value: presetSourceLabel(item) },
        ]"
        :selected="isRuleSelected(index)"
        :select-mode="ruleSelectMode"
        :subtitle="item.type != undefined ? $t('rule.logical') + ' (' + item.mode + ')' : $t('rule.simple')"
        :title="index + 1"
        @delete="delRule(index)"
        @edit="showRuleModal(index)"
        @update:delete-open="delRuleOverlay[index] = $event"
        @update:selected="toggleRuleSelection(index, Boolean($event))"
      />
    </v-col>
  </v-row>
</template>

<script lang="ts" setup>
import ManualSortButton from '@/components/ManualSortButton.vue'
import BulkSelectionControls from '@/shared/ui/BulkSelectionControls.vue'
import ClassicConfigCard from '@/shared/ui/ClassicConfigCard.vue'
import CollapsibleSectionHeader from '@/shared/ui/CollapsibleSectionHeader.vue'
import RulesDialogs from '@/components/rules/RulesDialogs.vue'
import DomainResolver from '@/components/fields/DomainResolver.vue'
import RegionalPresetDrawer from '@/components/presets/RegionalPresetDrawer.vue'
import { isPresetManagedItem } from '@/components/presets/routingDnsPresets'
import NexusDataTable from '@/components/nexus/data/NexusDataTable.vue'
import RowActions from '@/components/nexus/data/RowActions.vue'
import NexusBadge from '@/components/nexus/primitives/Badge.vue'
import EmptyState from '@/components/nexus/primitives/EmptyState.vue'
import PageHeader from '@/components/nexus/primitives/PageHeader.vue'
import PageToolbar from '@/components/nexus/primitives/PageToolbar.vue'
import { useRulesPage } from '@/shared/composables/pages/useRulesPage'

const page = useRulesPage()
const { actionKeys, actionMenu, appConfig, applyPresetConfig, confirm, defaultFallbackDelayMs, deleteSelectedRules, deleteSelectedRulesets, delRule, delRuleOverlay, delRuleset, delRulesetOverlay, findProcess, handleRuleAction, handleRulesetAction, isRuleSelected, isRulesetSelected, loading, mode, moveRulesTo, moveRulesetsTo, moveRuleTo, moveRulesetTo, networkTypes, nexus, onRuleDrop, onRulesetDrop, outboundTags, overrideAndroidVpn, presetSourceLabel, regionalPresetDrawer, route, routeDefaultNetworkStrategy, routeMark, routePreset, routePresets, ruleActions, ruleColumns, ruleDrag, ruleRows, ruleSelectMode, rules, rulesetActions, rulesetColumns, rulesetDrag, rulesetRows, rulesetSelectMode, rulesets, rulesetsExpanded, saveConfig, search, selectedRuleIndexes, selectedRulesetIndexes, showImportRule, showImportRulesets, showRuleModal, showRulesetModal, sortRulesetsByName, stateChange, subtitle, toggleRuleSelectMode, toggleRuleSelection, toggleRulesetSelectMode, toggleRulesetSelection } = page
const indexKeys = (rows: unknown[]): number[] => rows.map((_, rowIndex) => rowIndex)
const presetSourceVariant = (item: any) => isPresetManagedItem(item) ? 'success' : 'secondary'
</script>

<style scoped lang="scss" src="./Rules.scss"></style>
