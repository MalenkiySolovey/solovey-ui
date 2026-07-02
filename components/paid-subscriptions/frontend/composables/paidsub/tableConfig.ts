import { i18n } from '@/locales'
import type { Column } from '@/components/nexus/data/dataTableColumns'
import type { RowAction } from '@/components/nexus/data/rowActions'

export const paidSubCurrencyOptions = ['RUB', 'USD', 'EUR', 'GBP', 'CNY', 'KZT', 'UAH', 'TRY', 'AED', 'XTR']

export const normalizePaidSubCurrency = (value: unknown) => String(value ?? '').trim().toUpperCase().slice(0, 3)

export const paidSubBindingColumns: Column<any>[] = [
  { key: 'name', labelKey: 'paidSub.cols.client', sortable: true },
  { key: 'clientId', labelKey: 'paidSub.cols.clientId', sortable: true },
  { key: 'desc', labelKey: 'paidSub.cols.description' },
  { key: 'tgUserId', labelKey: 'paidSub.cols.telegramId' },
  { key: 'expiry', labelKey: 'paidSub.cols.expiry', sortable: true },
  { key: 'enable', labelKey: 'paidSub.cols.status' },
]

export const paidSubTariffColumns: Column<any>[] = [
  { key: 'name', labelKey: 'paidSub.cols.name', sortable: true },
  { key: 'price', labelKey: 'paidSub.cols.price' },
  { key: 'starsAmount', labelKey: 'paidSub.cols.stars' },
  { key: 'addDays', labelKey: 'paidSub.cols.addDays' },
  { key: 'addTrafficBytes', labelKey: 'paidSub.cols.addTraffic' },
  { key: 'enabled', labelKey: 'paidSub.cols.enabled' },
]

export const paidSubOrderColumns: Column<any>[] = [
  { key: 'id', labelKey: 'paidSub.cols.id', sortable: true },
  { key: 'clientName', labelKey: 'paidSub.cols.clientName' },
  { key: 'telegramUserId', labelKey: 'paidSub.cols.telegramId' },
  { key: 'clientDesc', labelKey: 'paidSub.cols.description' },
  { key: 'provider', labelKey: 'paidSub.cols.provider' },
  { key: 'amount', labelKey: 'paidSub.cols.amount' },
  { key: 'status', labelKey: 'paidSub.cols.status', sortable: true },
  { key: 'createdAt', labelKey: 'paidSub.cols.created', sortable: true },
]

export const paidSubBindingActions = (item: any): RowAction[] => [
  { key: 'edit', labelKey: 'actions.edit', icon: 'lucide:pencil', inline: true },
  { key: 'unbind', labelKey: 'paidSub.unbind.confirm', icon: 'lucide:unlink', tone: 'error', inline: true, hidden: !item.tgUserId },
]

export const paidSubTariffActions = (): RowAction[] => [
  { key: 'edit', labelKey: 'actions.edit', icon: 'lucide:pencil', inline: true },
  { key: 'del', labelKey: 'actions.del', icon: 'lucide:trash-2', tone: 'error', inline: true },
]

export const paidSubOrderActions = (item: any): RowAction[] => [
  { key: 'refund', labelKey: 'paidSub.orders.refund', icon: 'lucide:rotate-ccw', inline: true, hidden: item.status !== 'paid' },
]

export const paidSubBindingHeaders = () => [
  { title: i18n.global.t('paidSub.cols.client'), key: 'name' },
  { title: i18n.global.t('paidSub.cols.clientId'), key: 'clientId' },
  { title: i18n.global.t('paidSub.cols.description'), key: 'desc' },
  { title: i18n.global.t('paidSub.cols.telegramId'), key: 'tgUserId' },
  { title: i18n.global.t('paidSub.cols.expiry'), key: 'expiry' },
  { title: i18n.global.t('paidSub.cols.status'), key: 'enable' },
  { title: '', key: 'actions', sortable: false, align: 'end' as const },
]

export const paidSubTariffHeaders = () => [
  { title: i18n.global.t('paidSub.cols.name'), key: 'name' },
  { title: i18n.global.t('paidSub.cols.price'), key: 'price' },
  { title: i18n.global.t('paidSub.cols.stars'), key: 'starsAmount' },
  { title: i18n.global.t('paidSub.cols.addDays'), key: 'addDays' },
  { title: i18n.global.t('paidSub.cols.addTraffic'), key: 'addTrafficBytes' },
  { title: i18n.global.t('paidSub.cols.enabled'), key: 'enabled' },
  { title: '', key: 'actions', sortable: false, align: 'end' as const },
]

export const paidSubOrderHeaders = () => [
  { title: i18n.global.t('paidSub.cols.id'), key: 'id' },
  { title: i18n.global.t('paidSub.cols.clientName'), key: 'clientName' },
  { title: i18n.global.t('paidSub.cols.telegramId'), key: 'telegramUserId' },
  { title: i18n.global.t('paidSub.cols.description'), key: 'clientDesc' },
  { title: i18n.global.t('paidSub.cols.provider'), key: 'provider' },
  { title: i18n.global.t('paidSub.cols.amount'), key: 'amount' },
  { title: i18n.global.t('paidSub.cols.status'), key: 'status' },
  { title: i18n.global.t('paidSub.cols.created'), key: 'createdAt' },
  { title: '', key: 'actions', sortable: false },
]

export const paidSubTransportModes = () => [
  { title: i18n.global.t('paidSub.transportModes.proxy'), value: 'proxy' },
  { title: i18n.global.t('paidSub.transportModes.outbound'), value: 'outbound' },
]

export const paidSubOrderStatusTone = (status: string): 'info' | 'success' | 'warning' | 'error' =>
  status === 'paid' ? 'success' : status === 'pending' ? 'warning' : status === 'failed' ? 'error' : 'info'

export const paidSubOrderStatusColor = (status: string) =>
  ({ paid: 'success', pending: 'warning', failed: 'error', expired: 'grey', canceled: 'grey', refunded: 'info' } as any)[status] || 'grey'
