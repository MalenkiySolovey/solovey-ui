import type { Column } from '@/components/nexus/data/dataTableColumns'
import type { RowAction } from '@/components/nexus/data/rowActions'

export const rulesetColumns: Column<any>[] = [
  { key: 'tag', labelKey: 'objects.tag' },
  { key: 'type', labelKey: 'type' },
  { key: 'format', labelKey: 'ruleset.format' },
  { key: 'download_detour', labelKey: 'objects.outbound' },
  { key: 'update_interval', labelKey: 'actions.update' },
  { key: 'source', labelKey: 'presets.source' },
]

export const ruleColumns: Column<any>[] = [
  { key: '_index', labelKey: 'table.rowNumber' },
  { key: 'type', labelKey: 'type' },
  { key: 'action', labelKey: 'admin.action' },
  { key: 'outbound', labelKey: 'objects.outbound' },
  { key: '_rulesCount', labelKey: 'pages.rules' },
  { key: 'invert', labelKey: 'rule.invert' },
  { key: 'source', labelKey: 'presets.source' },
]

export const rulesetActionsFor = (
  item: any,
  search: string,
  rulesetsLength: number,
): RowAction[] => [
  { key: 'up', labelKey: 'table.moveUp', icon: 'lucide:arrow-up', inline: true, reserveSpace: true, hidden: search.trim().length > 0 || item._index === 0 },
  { key: 'down', labelKey: 'table.moveDown', icon: 'lucide:arrow-down', inline: true, reserveSpace: true, hidden: search.trim().length > 0 || item._index === rulesetsLength - 1 },
  { key: 'edit', labelKey: 'actions.edit', icon: 'lucide:pencil', inline: true },
  { key: 'del', labelKey: 'actions.del', icon: 'lucide:trash-2', tone: 'error', inline: true },
]

export const ruleActionsFor = (
  item: any,
  search: string,
  rulesLength: number,
): RowAction[] => [
  { key: 'up', labelKey: 'table.moveUp', icon: 'lucide:arrow-up', inline: true, reserveSpace: true, hidden: search.trim().length > 0 || item._index === 0 },
  { key: 'down', labelKey: 'table.moveDown', icon: 'lucide:arrow-down', inline: true, reserveSpace: true, hidden: search.trim().length > 0 || item._index === rulesLength - 1 },
  { key: 'edit', labelKey: 'actions.edit', icon: 'lucide:pencil', inline: true },
  { key: 'del', labelKey: 'actions.del', icon: 'lucide:trash-2', tone: 'error', inline: true },
]
