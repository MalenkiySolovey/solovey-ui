<template>
  <v-card :subtitle="$t('types.failover.title')">
    <v-row>
      <v-col cols="12">
        <div class="text-caption mb-1">{{ $t('types.failover.members') }}</div>
        <v-list class="bg-transparent pa-0" density="compact">
          <v-list-item v-for="(member, index) in members" :key="index" class="px-0">
            <v-row align="center" no-gutters>
              <v-col>
                <StrictSelect
                  :items="memberItems(index)"
                  :label="index === 0 ? $t('types.failover.primary') : `${$t('types.failover.backup')} ${index}`"
                  :model-value="members[index]"
                  hide-details
                  @update:model-value="setMember(index, $event)"
                />
              </v-col>
              <v-col v-if="failoverStatus" class="failover-live" cols="auto">
                <span
                  class="failover-live__dot"
                  :class="memberHealthy(member) ? 'failover-live__dot--up' : 'failover-live__dot--down'"
                >
                  <v-tooltip activator="parent" location="top" :text="memberHealthy(member) ? $t('online') : $t('overview.status.offline')" />
                </span>
                <v-chip v-if="memberActive(member)" color="primary" density="compact" size="x-small" variant="tonal">
                  {{ $t('overview.protocols.activeShort') }}
                </v-chip>
              </v-col>
              <v-col class="d-flex" cols="auto">
                <v-btn :disabled="index === 0" icon="lucide:arrow-up" size="small" variant="text" @click="move(index, -1)" />
                <v-btn :disabled="index === members.length - 1" icon="lucide:arrow-down" size="small" variant="text" @click="move(index, 1)" />
                <v-btn color="warning" icon="lucide:trash-2" size="small" variant="text" @click="remove(index)" />
              </v-col>
            </v-row>
          </v-list-item>
        </v-list>
        <v-alert
          v-if="failoverStatus?.allDown"
          class="mt-2"
          density="compact"
          type="warning"
          variant="tonal"
        >
          {{ $t('types.failover.allDown') }}
        </v-alert>
        <v-btn prepend-icon="lucide:plus" size="small" variant="tonal" @click="add">
          {{ $t('types.failover.addMember') }}
        </v-btn>
      </v-col>
    </v-row>
    <v-row>
      <v-col cols="12">
        <v-text-field
          v-model="probeTarget"
          :label="$t('types.failover.probeTarget')"
          :placeholder="defaultTarget"
          persistent-placeholder
          hide-details
        />
      </v-col>
      <v-col cols="12" sm="6">
        <v-text-field
          v-model.number="interval"
          :label="$t('types.failover.interval')"
          :suffix="$t('date.s')"
          hide-details
          min="5"
          type="number"
        />
      </v-col>
      <v-col cols="12" sm="6">
        <v-text-field v-model.number="hysteresis" :label="$t('types.failover.hysteresis')" hide-details min="1" type="number" />
      </v-col>
      <v-col cols="12" sm="6">
        <v-switch v-model="enabled" color="primary" :label="$t('types.failover.enabled')" hide-details />
      </v-col>
      <v-col cols="12" sm="6">
        <v-switch v-model="data.interrupt_exist_connections" color="primary" :label="$t('types.lb.interruptConn')" hide-details />
      </v-col>
    </v-row>
  </v-card>
</template>

<script lang="ts">
import Data from '@/store/modules/data'
import StrictSelect from '@/shared/ui/StrictSelect.vue'

export default {
  components: { StrictSelect },
  props: ['data', 'tags'],
  created() {
    if (!Array.isArray(this.$props.data.outbounds)) this.$props.data.outbounds = []
    if (!this.$props.data.failover) {
      this.$props.data.failover = { enabled: true, probe_target: '', interval: '30s', hysteresis: 2 }
    }
  },
  computed: {
    defaultTarget(): string { return 'https://www.gstatic.com/generate_204' },
    members(): string[] { return (this.$props.data.outbounds ?? []) as string[] },
    failoverStatus(): any { return Data().failoverStatus?.[this.$props.data.tag] ?? null },
    probeTarget: {
      get(): string { return this.$props.data.failover.probe_target ?? '' },
      set(value: string) { this.$props.data.failover.probe_target = value },
    },
    interval: {
      get(): number {
        return Number.parseInt(String(this.$props.data.failover.interval || '30s').replace('s', ''), 10) || 30
      },
      set(value: number) { this.$props.data.failover.interval = `${value >= 5 ? value : 30}s` },
    },
    hysteresis: {
      get(): number { return this.$props.data.failover.hysteresis || 2 },
      set(value: number) { this.$props.data.failover.hysteresis = value >= 1 ? value : 2 },
    },
    enabled: {
      get(): boolean { return this.$props.data.failover.enabled !== false },
      set(value: boolean) { this.$props.data.failover.enabled = value },
    },
  },
  methods: {
    memberItems(index: number): string[] {
      const selectedElsewhere = this.$props.data.outbounds.filter((_: string, itemIndex: number) => itemIndex !== index)
      return this.$props.tags.filter((tag: string) => tag !== this.$props.data.tag && (tag === this.$props.data.outbounds[index] || !selectedElsewhere.includes(tag)))
    },
    setMember(index: number, value: unknown) {
      this.$props.data.outbounds.splice(index, 1, typeof value === 'string' ? value : '')
    },
    move(index: number, direction: number) {
      const target = index + direction
      if (target < 0 || target >= this.$props.data.outbounds.length) return
      const [member] = this.$props.data.outbounds.splice(index, 1)
      this.$props.data.outbounds.splice(target, 0, member)
    },
    remove(index: number) { this.$props.data.outbounds.splice(index, 1) },
    add() { this.$props.data.outbounds.push('') },
    memberActive(member: string): boolean {
      return Boolean(member && this.failoverStatus?.active === member)
    },
    memberHealthy(member: string): boolean {
      const status = this.failoverStatus?.members?.find((item: any) => item.tag === member)
      return Boolean(status?.healthy)
    },
  },
}
</script>

<style scoped>
.failover-live {
  align-items: center;
  display: flex;
  gap: 8px;
  justify-content: flex-end;
  min-width: 92px;
  padding-inline: 8px;
}

.failover-live__dot {
  border-radius: 999px;
  display: inline-block;
  height: 10px;
  width: 10px;
}

.failover-live__dot--up {
  background: rgb(var(--v-theme-success));
  box-shadow: 0 0 0 3px rgba(var(--v-theme-success), 0.16);
}

.failover-live__dot--down {
  background: rgb(var(--v-theme-error));
  box-shadow: 0 0 0 3px rgba(var(--v-theme-error), 0.16);
}
</style>
