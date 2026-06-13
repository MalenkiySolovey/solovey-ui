import { ref } from 'vue'

const blockedSelector = [
  'button',
  'a',
  'span',
  'strong',
  'small',
  'code',
  'p',
  'input',
  'textarea',
  'select',
  'label',
  '[contenteditable="true"]',
  '.v-btn',
  '.v-card-title',
  '.v-card-subtitle',
  '.v-card-text',
  '.v-chip',
  '.v-icon',
  '.v-field',
  '.v-selection-control',
  '.v-overlay',
  '.v-menu',
  '.manual-drag-no-drag',
].join(',')

const eventElement = (event: Event): HTMLElement | null => {
  const target = event.target
  if (target instanceof HTMLElement) return target
  if (target instanceof Text) return target.parentElement
  return null
}

const eventDragHost = (event: Event): HTMLElement | null => {
  const target = event.currentTarget
  return target instanceof HTMLElement ? target : null
}

export const canStartManualDrag = (event: Event): boolean => {
  const target = eventElement(event)
  if (!target) return false
  return target.closest(blockedSelector) == null
}

export const prepareManualDrag = (event: Event, disabled = false): boolean => {
  const host = eventDragHost(event)
  if (!host) return false

  const allowed = !disabled && canStartManualDrag(event)
  host.draggable = allowed
  return allowed
}

export const resetManualDrag = (event?: Event) => {
  if (!event) return
  const host = eventDragHost(event)
  if (host) host.draggable = false
}

export const useManualDrag = <T>() => {
  const dragged = ref<T | null>(null)

  const prepare = (event: Event, disabled = false): boolean => prepareManualDrag(event, disabled)

  const start = (event: DragEvent, item: T, disabled = false): boolean => {
    if (disabled || !canStartManualDrag(event)) {
      resetManualDrag(event)
      event.preventDefault()
      return false
    }

    dragged.value = item as any
    if (event.dataTransfer) {
      event.dataTransfer.effectAllowed = 'move'
      event.dataTransfer.setData('text/plain', 'manual-order')
    }
    return true
  }

  const over = (event: DragEvent, disabled = false) => {
    if (disabled) return
    event.preventDefault()
    if (event.dataTransfer) event.dataTransfer.dropEffect = 'move'
  }

  const drop = (event: DragEvent, target: T, onDrop: (dragged: T, target: T) => void | Promise<void>, disabled = false) => {
    if (disabled) return
    event.preventDefault()
    if (dragged.value == null) return

    const source = dragged.value as T
    dragged.value = null
    if (source === target) return
    return onDrop(source, target)
  }

  const clear = (event?: Event) => {
    dragged.value = null
    resetManualDrag(event)
  }

  return { dragged, prepare, start, over, drop, clear }
}

export const moveArrayItem = <T>(items: T[], from: number, to: number) => {
  if (from < 0 || to < 0 || from >= items.length || to >= items.length || from === to) return false
  const [item] = items.splice(from, 1)
  items.splice(to, 0, item)
  return true
}
