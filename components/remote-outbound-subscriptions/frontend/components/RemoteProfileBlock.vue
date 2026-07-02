<template>
  <section class="remote-profile-block" :style="{ '--profile-depth': depth }">
    <button class="remote-profile-block__head" type="button" @click="expanded = !expanded">
      <v-icon :icon="expanded ? 'lucide:chevron-down' : 'lucide:chevron-right'" size="18" />
      <span class="remote-profile-block__title">{{ block.name || 'Unnamed connection' }}</span>
      <span class="remote-profile-block__type">{{ block.type || 'connection' }}</span>
      <span v-if="memberCount > 0" class="remote-profile-block__meta">{{ memberCount }} members</span>
      <span v-if="characteristicCount > 0" class="remote-profile-block__meta">{{ characteristicCount }} fields</span>
      <span class="remote-profile-block__sources">
        <v-chip
          v-for="source in sources"
          :key="source"
          density="compact"
          size="x-small"
          variant="tonal"
        >
          {{ source }}
        </v-chip>
      </span>
    </button>

    <div v-if="expanded" class="remote-profile-block__body">
      <div v-if="characteristics.length > 0" class="remote-profile-block__section">
        <h4>Characteristics</h4>
        <div class="remote-profile-block__fields">
          <div
            v-for="characteristic in characteristics"
            :key="characteristic.key || characteristic.label"
            class="remote-profile-block__field"
          >
            <span class="remote-profile-block__label">{{ characteristic.label || characteristic.key }}</span>
            <span class="remote-profile-block__values">
              <span
                v-for="(value, index) in characteristic.values ?? []"
                :key="`${value.value}-${index}`"
                class="remote-profile-block__value"
              >
                <code>{{ value.value }}</code>
                <span v-if="value.sources?.length" class="remote-profile-block__value-sources">
                  [{{ value.sources.join(', ') }}]
                </span>
              </span>
            </span>
          </div>
        </div>
      </div>

      <div v-if="members.length > 0" class="remote-profile-block__section">
        <h4>Members</h4>
        <div class="remote-profile-block__members">
          <RemoteProfileBlock
            v-for="(member, index) in members"
            :key="`${member.name}-${index}`"
            :block="member"
            :depth="depth + 1"
          />
        </div>
      </div>
    </div>
  </section>
</template>

<script lang="ts" setup>
import { computed, ref } from 'vue'

defineOptions({ name: 'RemoteProfileBlock' })

interface ProfileValue {
  value: string
  sources?: string[]
}

interface ProfileCharacteristic {
  key: string
  label: string
  values?: ProfileValue[]
}

interface ProfileBlock {
  name: string
  type: string
  sources?: string[]
  characteristics?: ProfileCharacteristic[]
  connections?: ProfileBlock[]
}

const props = withDefaults(defineProps<{
  block: ProfileBlock
  depth?: number
}>(), {
  depth: 0,
})

const expanded = ref(false)
const sources = computed(() => props.block.sources ?? [])
const characteristics = computed(() => props.block.characteristics ?? [])
const members = computed(() => props.block.connections ?? [])
const characteristicCount = computed(() => characteristics.value.length)
const memberCount = computed(() => members.value.length)
</script>

<style scoped>
.remote-profile-block {
  border: 1px solid rgba(var(--v-border-color), var(--v-border-opacity));
  border-radius: 8px;
  margin-left: calc(var(--profile-depth) * 12px);
  overflow: hidden;
}

.remote-profile-block__head {
  align-items: center;
  background: rgba(var(--v-theme-on-surface), .035);
  border: 0;
  color: inherit;
  cursor: pointer;
  display: grid;
  gap: 8px;
  grid-template-columns: auto minmax(160px, 1fr) minmax(120px, auto) auto auto auto;
  min-height: 42px;
  padding: 8px 10px;
  text-align: start;
  width: 100%;
}

.remote-profile-block__head:hover {
  background: rgba(var(--v-theme-on-surface), .06);
}

.remote-profile-block__title {
  font-weight: 700;
  min-width: 0;
  overflow-wrap: anywhere;
}

.remote-profile-block__type,
.remote-profile-block__meta {
  color: rgba(var(--v-theme-on-surface), .66);
  font-size: .8rem;
  white-space: nowrap;
}

.remote-profile-block__sources {
  align-items: center;
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  justify-content: flex-end;
}

.remote-profile-block__body {
  display: grid;
  gap: 12px;
  padding: 10px;
}

.remote-profile-block__section {
  display: grid;
  gap: 8px;
}

.remote-profile-block__section h4 {
  color: rgba(var(--v-theme-on-surface), .72);
  font-size: .78rem;
  margin: 0;
  text-transform: uppercase;
}

.remote-profile-block__fields {
  display: grid;
  gap: 6px;
}

.remote-profile-block__field {
  display: grid;
  gap: 8px;
  grid-template-columns: minmax(120px, 220px) 1fr;
}

.remote-profile-block__label {
  color: rgba(var(--v-theme-on-surface), .66);
  font-size: .82rem;
}

.remote-profile-block__values {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  min-width: 0;
}

.remote-profile-block__value {
  background: rgba(var(--v-theme-on-surface), .045);
  border-radius: 6px;
  min-width: 0;
  padding: 2px 6px;
}

.remote-profile-block__value code {
  overflow-wrap: anywhere;
}

.remote-profile-block__value-sources {
  color: rgba(var(--v-theme-on-surface), .58);
  font-size: .75rem;
  margin-left: 4px;
}

.remote-profile-block__members {
  display: grid;
  gap: 8px;
}

@media (max-width: 760px) {
  .remote-profile-block__head,
  .remote-profile-block__field {
    grid-template-columns: 1fr;
  }

  .remote-profile-block__sources {
    justify-content: flex-start;
  }
}
</style>
