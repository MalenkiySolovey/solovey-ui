import { shallowRef } from 'vue'
import {
  clearManualGridMetricsCache,
  createManualGridMetricsCache,
  manualGridCardElement,
  manualGridDropFromEvent,
} from '@/shared/composables/dragSelection/manualGridDrop'

export type ManualDropPosition = 'before' | 'after'
export type ManualDropAxis = 'order' | 'vertical' | 'horizontal' | 'grid'
export type ManualDragKey = string | number

export interface ManualDropIndicator<T> {
  target: T
  position: ManualDropPosition
  lineStyle?: Record<string, string>
}

export type ManualDropHandler<T> = (
  dragged: T,
  target: T,
  position: ManualDropPosition | null,
) => void | Promise<void>

export const manualDropIndicatorFor = <T>(
  source: T | null,
  target: T,
  orderedKeys: readonly T[],
  selectedKeys: readonly T[] = [],
  position: ManualDropPosition | null = null,
): ManualDropIndicator<T> | null => {
  if (source == null || sameKey(source, target)) return null

  const targetIndex = orderedKeys.findIndex(key => sameKey(key, target))
  if (targetIndex < 0) return null

  const selected = selectedKeys.map(String)
  if (selected.length > 0 && selected.includes(String(target))) return null
  const multiDrag = selected.length > 1

  const firstMovingIndex = selected.length > 0
    ? orderedKeys.findIndex(key => selected.includes(String(key)))
    : orderedKeys.findIndex(key => sameKey(key, source))
  if (firstMovingIndex < 0) return null

  const resolvedPosition = position ?? (firstMovingIndex < targetIndex ? 'after' : 'before')
  if (!multiDrag && isSingleItemNoopDrop(firstMovingIndex, targetIndex, resolvedPosition)) {
    return null
  }

  return {
    target,
    position: resolvedPosition,
  }
}

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

export const manualDropPositionFromEvent = (
  event: DragEvent,
  axis: Exclude<ManualDropAxis, 'order'>,
): ManualDropPosition | null => {
  const host = eventDragHost(event)
  if (!host) return null

  const rect = axis === 'grid'
    ? manualGridCardElement(host).getBoundingClientRect()
    : host.getBoundingClientRect()
  if (axis === 'horizontal') {
    return event.clientX < rect.left + rect.width / 2 ? 'before' : 'after'
  }
  if (axis === 'grid') {
    return event.clientX < rect.left + rect.width / 2 ? 'before' : 'after'
  }

  return event.clientY < rect.top + rect.height / 2 ? 'before' : 'after'
}

export const manualGridDropLineStyle = (
  event: DragEvent,
  position: ManualDropPosition,
): Record<string, string> | null => {
  return manualGridDropFromEvent(event, position, createManualGridMetricsCache())?.lineStyle ?? null
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
  const dragged = shallowRef<T | null>(null)
  const dropIndicator = shallowRef<ManualDropIndicator<T> | null>(null)
  const gridCache = createManualGridMetricsCache()

  const prepare = (event: Event, disabled = false): boolean => prepareManualDrag(event, disabled)

  const start = (event: DragEvent, item: T, disabled = false): boolean => {
    if (disabled || !canStartManualDrag(event)) {
      resetManualDrag(event)
      event.preventDefault()
      return false
    }

    dragged.value = item as any
    clearManualGridMetricsCache(gridCache)
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

  const overTarget = (
    event: DragEvent,
    target: T,
    orderedKeys: readonly T[],
    selectedKeys: readonly T[] = [],
    disabled = false,
    axis: ManualDropAxis = 'order',
  ) => {
    over(event, disabled)
    if (disabled || dragged.value == null) {
      dropIndicator.value = null
      return
    }
    const gridDrop = axis === 'grid' ? manualGridDropFromEvent(event, gridCache) : null
    const gridTarget = gridDrop?.targetIndex != null ? orderedKeys[gridDrop.targetIndex] : undefined
    const dropTarget = gridTarget ?? target
    const position = gridDrop?.position ?? (axis === 'order' ? null : manualDropPositionFromEvent(event, axis))
    const indicator = manualDropIndicatorFor(dragged.value, dropTarget, orderedKeys, selectedKeys, position)
    if (indicator && axis === 'grid') {
      indicator.lineStyle = gridDrop?.position === indicator.position ? gridDrop.lineStyle : undefined
    }
    if (sameIndicator(dropIndicator.value, indicator)) {
      return
    }
    dropIndicator.value = indicator
  }

  const leaveTarget = (event: DragEvent, target: T) => {
    const current = event.currentTarget
    const related = event.relatedTarget
    if (current instanceof HTMLElement && related instanceof Node && current.contains(related)) return
    if (dragged.value != null) return
    if (dropIndicator.value && sameKey(dropIndicator.value.target, target)) dropIndicator.value = null
  }

  const drop = (event: DragEvent, target: T, onDrop: ManualDropHandler<T>, disabled = false) => {
    if (disabled) return
    event.preventDefault()
    if (dragged.value == null) return

    const source = dragged.value as T
    const activeDrop = dropIndicator.value
    if (!activeDrop) {
      dragged.value = null
      dropIndicator.value = null
      clearManualGridMetricsCache(gridCache)
      return
    }

    const dropTarget = activeDrop.target
    const position = activeDrop.position
    dragged.value = null
    dropIndicator.value = null
    clearManualGridMetricsCache(gridCache)
    if (sameKey(source, dropTarget)) return
    return onDrop(source, dropTarget, position)
  }

  const clear = (event?: Event) => {
    dragged.value = null
    dropIndicator.value = null
    clearManualGridMetricsCache(gridCache)
    resetManualDrag(event)
  }

  const indicatorClasses = (target: T): Record<string, boolean> => ({
    'manual-drop-before': dropIndicator.value?.position === 'before' && sameKey(dropIndicator.value.target, target),
    'manual-drop-after': dropIndicator.value?.position === 'after' && sameKey(dropIndicator.value.target, target),
  })

  const indicatorStyles = (target: T): Record<string, string> => (
    dropIndicator.value && sameKey(dropIndicator.value.target, target)
      ? dropIndicator.value.lineStyle ?? {}
      : {}
  )

  return { dragged, dropIndicator, indicatorClasses, indicatorStyles, leaveTarget, overTarget, prepare, start, over, drop, clear }
}

const sameKey = <T>(left: T, right: T): boolean => String(left) === String(right)

const isSingleItemNoopDrop = (
  sourceIndex: number,
  targetIndex: number,
  position: ManualDropPosition,
): boolean => {
  return (position === 'before' && targetIndex === sourceIndex + 1)
    || (position === 'after' && targetIndex === sourceIndex - 1)
}

const sameIndicator = <T>(
  left: ManualDropIndicator<T> | null,
  right: ManualDropIndicator<T> | null,
): boolean => {
  if (!left || !right) return left === right
  return sameKey(left.target, right.target)
    && left.position === right.position
    && sameLineStyle(left.lineStyle, right.lineStyle)
}

const sameLineStyle = (
  left?: Record<string, string>,
  right?: Record<string, string>,
): boolean => {
  const leftKeys = Object.keys(left ?? {})
  const rightKeys = Object.keys(right ?? {})
  if (leftKeys.length !== rightKeys.length) return false
  return leftKeys.every(key => left?.[key] === right?.[key])
}
