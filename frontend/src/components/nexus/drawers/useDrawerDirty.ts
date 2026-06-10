import { computed, type ComputedRef, ref } from 'vue'

export interface DrawerDirty {
  dirty: ComputedRef<boolean>
  // Capture the current serialized form as the clean baseline (call on open/save).
  reset: () => void
}

// Tracks whether a drawer form has unsaved edits by deep-comparing a JSON
// snapshot taken at open time against the live serialization. `serialize`
// should read the reactive form (e.g. () => JSON.stringify(form)) so the
// computed re-evaluates on every field change; the baseline is a ref so
// reset() also re-triggers the comparison.
export const useDrawerDirty = (serialize: () => string): DrawerDirty => {
  const baseline = ref<string | null>(null)

  const reset = (): void => {
    baseline.value = serialize()
  }

  const dirty = computed(() => baseline.value !== null && serialize() !== baseline.value)

  return { dirty, reset }
}
