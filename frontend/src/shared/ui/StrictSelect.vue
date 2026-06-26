<template>
  <v-combobox
    ref="selectRef"
    v-bind="$attrs"
    :chips="chips || multiple"
    :closable-chips="closableChips || multiple"
    :item-title="itemTitle"
    :item-value="itemValue"
    :items="items"
    :return-object="false"
    v-model="strictModel"
    v-model:search="search"
    :multiple="multiple"
    auto-select-first
    open-on-click
    open-on-focus
  >
    <template v-if="$slots.append" #append>
      <slot name="append" />
    </template>
    <template v-if="$slots.selection" #selection="slotProps">
      <slot name="selection" v-bind="slotProps" />
    </template>
  </v-combobox>
</template>

<script lang="ts" setup>
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'

import {
  sanitizeStrictSelectModel,
  sanitizeStrictSelectMultiple,
  sanitizeStrictSelectUpdate,
  strictSelectAllowedValues,
  strictSelectModelsEqual,
  type StrictSelectItem,
  type StrictSelectModel,
  type StrictSelectValue,
} from '@/shared/models/strictSelectModel'

const props = withDefaults(defineProps<{
  modelValue?: StrictSelectModel
  items?: StrictSelectItem[]
  multiple?: boolean
  chips?: boolean
  closableChips?: boolean
  itemTitle?: string
  itemValue?: string
}>(), {
  modelValue: undefined,
  items: () => [],
  multiple: false,
  chips: false,
  closableChips: false,
  itemTitle: 'title',
  itemValue: 'value',
})

const emit = defineEmits<{
  'update:modelValue': [value: StrictSelectModel]
}>()

const selectRef = ref()
const search = ref('')
let rootEl: HTMLElement | null = null

const itemOptions = computed(() => ({
  itemTitle: props.itemTitle,
  itemValue: props.itemValue,
}))

const updateOptions = computed(() => ({
  ...itemOptions.value,
  multiple: props.multiple,
}))

const allowedValues = computed(() => strictSelectAllowedValues(props.items, itemOptions.value))

const sanitizeMultiple = (value: unknown): StrictSelectValue[] =>
  sanitizeStrictSelectMultiple(value, allowedValues.value, itemOptions.value)

const sanitize = (value: unknown): StrictSelectModel =>
  sanitizeStrictSelectModel(value, allowedValues.value, updateOptions.value)

const strictModel = computed<StrictSelectModel>({
  get: () => sanitize(props.modelValue),
  set: (value) => {
    const next = sanitizeStrictSelectUpdate(value, props.modelValue, allowedValues.value, updateOptions.value)
    if (!strictSelectModelsEqual(next, sanitize(props.modelValue))) {
      emit('update:modelValue', next)
    }
  },
})

const closeMenu = () => {
  selectRef.value?.blur?.()
}

const onGlobalClose = () => closeMenu()

const onKeydown = (event: KeyboardEvent) => {
  const rootHasFocus = Boolean(
    rootEl?.contains(document.activeElement) ||
    rootEl?.querySelector('.v-field--focused'),
  )
  if (!rootHasFocus) return

  if (event.key === 'Escape') {
    closeMenu()
    return
  }

  if (!props.multiple || !['Backspace', 'Delete'].includes(event.key)) return
  const input = rootEl?.querySelector('input')
  const target = event.target as HTMLInputElement | null
  const inputValue = typeof input?.value === 'string'
    ? input.value
    : (typeof target?.value === 'string' ? target.value : '')
  if (inputValue.length > 0) return

  const current = sanitizeMultiple(props.modelValue)
  if (current.length === 0) return

  emit('update:modelValue', current.slice(0, -1))
  search.value = ''
  event.preventDefault()
  event.stopPropagation()
}

onMounted(() => {
  rootEl = selectRef.value?.$el as HTMLElement | null
  document.addEventListener('keydown', onKeydown, true)
  window.addEventListener('sui-close-select-menus', onGlobalClose)
})

onBeforeUnmount(() => {
  closeMenu()
  document.removeEventListener('keydown', onKeydown, true)
  rootEl = null
  window.removeEventListener('sui-close-select-menus', onGlobalClose)
})

defineExpose({
  closeMenu,
  focus: () => nextTick(() => selectRef.value?.focus?.()),
})
</script>
