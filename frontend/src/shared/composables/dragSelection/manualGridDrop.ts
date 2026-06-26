import type { ManualDropPosition } from '@/shared/composables/dragSelection/manualDrag'

interface ManualGridCellMetrics {
  cell: HTMLElement
  hostLeft: number
  hostRight: number
  hostTop: number
  cardLeft: number
  cardRight: number
  cardTop: number
  cardBottom: number
  cardHeight: number
  cardMidX: number
}

export interface ManualGridMetricsCache {
  parent: HTMLElement | null
  cells: HTMLElement[]
  metrics: Map<HTMLElement, ManualGridCellMetrics>
}

export const createManualGridMetricsCache = (): ManualGridMetricsCache => ({
  parent: null,
  cells: [],
  metrics: new Map(),
})

export const clearManualGridMetricsCache = (cache: ManualGridMetricsCache) => {
  cache.parent = null
  cache.cells = []
  cache.metrics.clear()
}

export const manualGridDropFromEvent = (
  event: DragEvent,
  positionOrCache: ManualDropPosition | ManualGridMetricsCache,
  maybeCache?: ManualGridMetricsCache,
): { targetIndex: number; position: ManualDropPosition; lineStyle: Record<string, string> } | null => {
  const host = event.currentTarget instanceof HTMLElement ? event.currentTarget : null
  const parent = host?.parentElement
  if (!host || !parent) return null

  const cache = typeof positionOrCache === 'string' ? maybeCache : positionOrCache
  const forcedPosition = typeof positionOrCache === 'string' ? positionOrCache : null
  if (!cache) return null

  ensureManualGridMetrics(parent, cache)

  const hostMetric = cache.metrics.get(host)
  if (!hostMetric) return null

  const rawPosition = forcedPosition ?? (event.clientX < hostMetric.cardMidX ? 'before' : 'after')
  const hostIndex = cache.cells.indexOf(host)
  if (hostIndex < 0) return null

  const nextSameRow = nextGridCellInSameRow(cache, hostMetric, hostIndex)
  const targetIndex = rawPosition === 'after' && nextSameRow
    ? cache.cells.indexOf(nextSameRow)
    : hostIndex
  const position = rawPosition === 'after' && nextSameRow ? 'before' : rawPosition
  const targetMetric = cache.metrics.get(cache.cells[targetIndex])
  if (!targetMetric) return null

  const neighbor = position === 'before'
    ? previousGridCellInSameRow(cache, targetMetric, targetIndex)
    : nextGridCellInSameRow(cache, targetMetric, targetIndex)

  const lineX = (() => {
    if (neighbor) {
      const neighborMetric = cache.metrics.get(neighbor)
      if (neighborMetric) {
        return position === 'before'
          ? (neighborMetric.cardRight + targetMetric.cardLeft) / 2
          : (targetMetric.cardRight + neighborMetric.cardLeft) / 2
      }
    }

    return position === 'before'
      ? (targetMetric.hostLeft + targetMetric.cardLeft) / 2
      : (targetMetric.cardRight + targetMetric.hostRight) / 2
  })()

  return {
    targetIndex,
    position,
    lineStyle: {
      '--manual-drop-line-left': `${lineX - targetMetric.hostLeft}px`,
      '--manual-drop-line-top': `${targetMetric.cardTop - targetMetric.hostTop}px`,
      '--manual-drop-line-height': `${targetMetric.cardHeight}px`,
    },
  }
}

export const manualGridCardElement = (cell: HTMLElement): HTMLElement => {
  const card = cell.querySelector('.v-card')
  return card instanceof HTMLElement ? card : cell
}

const ensureManualGridMetrics = (parent: HTMLElement, cache: ManualGridMetricsCache) => {
  if (cache.parent === parent && cache.metrics.size > 0) return

  cache.parent = parent
  cache.cells = Array.from(parent.children)
    .filter((element): element is HTMLElement =>
      element instanceof HTMLElement && element.classList.contains('manual-drop-grid-cell'),
    )
  cache.metrics.clear()

  for (const cell of cache.cells) {
    const hostRect = cell.getBoundingClientRect()
    const cardRect = manualGridCardElement(cell).getBoundingClientRect()
    cache.metrics.set(cell, {
      cell,
      hostLeft: hostRect.left,
      hostRight: hostRect.right,
      hostTop: hostRect.top,
      cardLeft: cardRect.left,
      cardRight: cardRect.right,
      cardTop: cardRect.top,
      cardBottom: cardRect.bottom,
      cardHeight: cardRect.height,
      cardMidX: cardRect.left + cardRect.width / 2,
    })
  }
}

const previousGridCellInSameRow = (
  cache: ManualGridMetricsCache,
  host: ManualGridCellMetrics,
  hostIndex: number,
): HTMLElement | null => {
  for (let index = hostIndex - 1; index >= 0; index--) {
    const cell = cache.cells[index]
    const metric = cache.metrics.get(cell)
    if (metric && sameVisualRowMetrics(host, metric)) return cell
  }
  return null
}

const nextGridCellInSameRow = (
  cache: ManualGridMetricsCache,
  host: ManualGridCellMetrics,
  hostIndex: number,
): HTMLElement | null => {
  for (let index = hostIndex + 1; index < cache.cells.length; index++) {
    const cell = cache.cells[index]
    const metric = cache.metrics.get(cell)
    if (metric && sameVisualRowMetrics(host, metric)) return cell
  }
  return null
}

const sameVisualRowMetrics = (left: ManualGridCellMetrics, right: ManualGridCellMetrics): boolean => {
  const overlap = Math.min(left.cardBottom, right.cardBottom) - Math.max(left.cardTop, right.cardTop)
  return overlap > Math.min(left.cardHeight, right.cardHeight) * 0.5
}
