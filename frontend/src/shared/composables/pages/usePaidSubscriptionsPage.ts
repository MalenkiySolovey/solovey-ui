import { computed, onMounted, ref } from 'vue'
import HttpUtils from '@/plugins/httputil'
import { HumanReadable } from '@/plugins/utils'
import { push } from 'notivue'
import { i18n } from '@/locales'
import { useUiMode } from '@/uiMode/useUiMode'
import {
  normalizePaidSubCurrency,
  paidSubBindingActions,
  paidSubBindingColumns,
  paidSubBindingHeaders,
  paidSubCurrencyOptions,
  paidSubOrderActions,
  paidSubOrderColumns,
  paidSubOrderHeaders,
  paidSubOrderStatusColor,
  paidSubOrderStatusTone,
  paidSubTariffActions,
  paidSubTariffColumns,
  paidSubTariffHeaders,
} from '@/shared/composables/paidsub/tableConfig'
import { usePaidSubscriptionSettings } from '@/shared/composables/paidsub/usePaidSubscriptionSettings'

export const usePaidSubscriptionsPage = () => {
  const currencyOptions = paidSubCurrencyOptions
  const normalizeCurrency = normalizePaidSubCurrency

  const { mode } = useUiMode()

  const nexus = computed(() => mode.value === 'nexus')

  const bindingColumns = paidSubBindingColumns
  const tariffColumns = paidSubTariffColumns
  const orderColumns = paidSubOrderColumns
  const bindingActions = paidSubBindingActions
  const tariffActions = paidSubTariffActions
  const orderActions = paidSubOrderActions

  const handleBindingAction = (key: string, item: any) => {
    if (key === 'edit') openBinding(item)
    else if (key === 'unbind') openUnbindConfirm(item)
  }

  const handleTariffAction = (key: string, item: any) => {
    if (key === 'edit') openTariff(item)
    else if (key === 'del') deleteTariff(item)
  }

  const handleOrderAction = (key: string, item: any) => {
    if (key === 'refund') openRefund(item)
  }

  const orderStatusTone = paidSubOrderStatusTone

  // The shared axios instance defaults POST bodies to x-www-form-urlencoded; the
  // paidsub endpoints parse JSON, so these POSTs must opt into a JSON body.
  const jsonPost = { headers: { 'Content-Type': 'application/json' } }

  const tab = ref('bindings')

  const loading = ref(false)

  const {
    autoInbounds,
    autoRegister,
    cryptoEnabled,
    enabled,
    externalEnabled,
    inboundOptions,
    loadInbounds,
    loadOutbounds,
    loadSettings,
    loadStatus,
    outboundOptions,
    paymasterEnabled,
    saveSettings,
    secretboxKeySet,
    settings,
    starsEnabled,
    stripeEnabled,
    transportModes,
    yooEnabled,
  } = usePaidSubscriptionSettings(loading)

  // ---- bindings ----
  const bindings = ref<any[]>([])

  const bindingsLoading = ref(false)

  const bindingHeaders = paidSubBindingHeaders()

  const bindingDialog = ref(false)

  const bindingEdit = ref<{ clientId: number; name: string; tgUserId: number | string; isNew: boolean }>({ clientId: 0, name: '', tgUserId: 0, isNew: false })

  // Clients available for the "Add binding" selector (all clients from the
  // bindings list, which already enumerates every client in the panel).
  const clientOptions = computed(() => bindings.value.map((b: any) => ({
    title: b.tgUserId ? `${b.name} (bound: ${b.tgUserId})` : b.name,
    value: b.clientId,
  })))

  const loadBindings = async () => {
    bindingsLoading.value = true
    const msg = await HttpUtils.get('api/paidsub/bindings')
    if (msg.success) bindings.value = msg.obj ?? []
    bindingsLoading.value = false
  }

  const openBinding = (item: any) => {
    bindingEdit.value = { clientId: item.clientId, name: item.name, tgUserId: item.tgUserId || '', isNew: false }
    bindingDialog.value = true
  }

  const openAddBinding = () => {
    bindingEdit.value = { clientId: bindings.value[0]?.clientId ?? 0, name: '', tgUserId: '', isNew: true }
    bindingDialog.value = true
  }

  const saveBinding = async () => {
    if (!bindingEdit.value.clientId) return
    const tgUserId = Number(bindingEdit.value.tgUserId) || 0
    const msg = await HttpUtils.post('api/paidsub/bindings', { clientId: bindingEdit.value.clientId, tgUserId }, jsonPost)
    if (msg.success) { bindingDialog.value = false; await loadBindings() }
  }

  const unbindDialog = ref(false)

  const unbindBusy = ref(false)

  const unbindEdit = ref<{ clientId: number; name: string; tgUserId: number | string }>({ clientId: 0, name: '', tgUserId: 0 })

  const openUnbindConfirm = (item: any) => {
    unbindEdit.value = { clientId: item.clientId, name: item.name, tgUserId: item.tgUserId }
    unbindDialog.value = true
  }

  const doUnbind = async () => {
    unbindBusy.value = true
    const msg = await HttpUtils.post('api/paidsub/bindings', { clientId: unbindEdit.value.clientId, tgUserId: 0 }, jsonPost)
    unbindBusy.value = false
    if (msg.success) { unbindDialog.value = false; await loadBindings() }
  }

  // ---- messages: greeting + broadcast ----
  const recipientCount = computed(() => bindings.value.filter((b: any) => b.tgUserId).length)

  const broadcastText = ref('')

  const broadcastLoading = ref(false)

  const broadcastDialog = ref(false)

  const broadcastResult = ref<{ sent: number; failed: number } | null>(null)

  const sendBroadcast = async () => {
    broadcastDialog.value = false
    broadcastLoading.value = true
    broadcastResult.value = null
    const msg = await HttpUtils.post('api/paidsub/broadcast', { text: broadcastText.value }, jsonPost)
    if (msg.success) {
      broadcastResult.value = { sent: Number(msg.obj?.sent ?? 0), failed: Number(msg.obj?.failed ?? 0) }
      broadcastText.value = ''
    }
    broadcastLoading.value = false
  }

  // ---- tariffs ----
  const tariffs = ref<any[]>([])

  const tariffsLoading = ref(false)

  const tariffHeaders = paidSubTariffHeaders()

  const tariffDialog = ref(false)

  const blankTariff = () => ({ id: 0, name: '', description: '', priceMajor: 0, currency: settings.value.paidSubCurrency || 'RUB', starsAmount: 0, addDays: 30, addTrafficGB: 0, sort: 0, enabled: true })

  const tariffEdit = ref<any>(blankTariff())

  const loadTariffs = async () => {
    tariffsLoading.value = true
    const msg = await HttpUtils.get('api/paidsub/tariffs')
    if (msg.success) tariffs.value = msg.obj ?? []
    tariffsLoading.value = false
  }

  const openTariff = (item?: any) => {
    if (item) {
      tariffEdit.value = {
        id: item.id, name: item.name, description: item.description,
        priceMajor: (item.price || 0) / 100, currency: item.currency,
        starsAmount: item.starsAmount || 0, addDays: item.addDays || 0,
        addTrafficGB: (item.addTrafficBytes || 0) / (1024 * 1024 * 1024),
        sort: item.sort || 0, enabled: !!item.enabled,
      }
    } else {
      tariffEdit.value = blankTariff()
    }
    tariffDialog.value = true
  }

  const saveTariff = async () => {
    const e = tariffEdit.value
    // Clamp every numeric to >= 0 so a typo / negative spinner value never reaches
    // the backend (which now also rejects negatives — defense in depth).
    const data: any = {
      name: e.name, description: e.description,
      price: Math.max(0, Math.round(Number(e.priceMajor) * 100) || 0),
      currency: (e.currency || 'RUB').toUpperCase(),
      starsAmount: Math.max(0, Math.round(Number(e.starsAmount) || 0)),
      addDays: Math.max(0, Math.round(Number(e.addDays) || 0)),
      addTrafficBytes: Math.max(0, Math.round((Number(e.addTrafficGB) || 0) * 1024 * 1024 * 1024)),
      sort: Math.max(0, Math.round(Number(e.sort) || 0)),
      enabled: !!e.enabled,
    }
    const action = e.id ? 'edit' : 'new'
    if (e.id) data.id = e.id
    const msg = await HttpUtils.post('api/paidsub/tariffs', { action, data }, jsonPost)
    if (msg.success) { tariffDialog.value = false; await loadTariffs() }
  }

  const deleteTariff = async (item: any) => {
    const msg = await HttpUtils.post('api/paidsub/tariffs', { action: 'del', data: item.id }, jsonPost)
    if (msg.success) await loadTariffs()
  }

  // ---- orders ----
  const orders = ref<any[]>([])

  const ordersLoading = ref(false)

  const orderHeaders = paidSubOrderHeaders()

  const loadOrders = async () => {
    ordersLoading.value = true
    const msg = await HttpUtils.get('api/paidsub/orders')
    if (msg.success) orders.value = msg.obj ?? []
    ordersLoading.value = false
  }

  const orderStatusColor = paidSubOrderStatusColor

  // ---- refund (admin-initiated) ----
  const refundDialog = ref(false)

  const refundBusy = ref(false)

  const refundEdit = ref<{ id: number; provider: string; amount: number; currency: string; revoke: boolean }>({
    id: 0, provider: '', amount: 0, currency: '', revoke: true,
  })

  const openRefund = (item: any) => {
    refundEdit.value = { id: item.id, provider: item.provider, amount: item.amount, currency: item.currency, revoke: true }
    refundDialog.value = true
  }

  const doRefund = async () => {
    refundBusy.value = true
    const msg = await HttpUtils.post('api/paidsub/refund', { orderId: refundEdit.value.id, revoke: refundEdit.value.revoke }, jsonPost)
    refundBusy.value = false
    if (msg.success) {
      refundDialog.value = false
      push.success({ title: i18n.global.t('success'), message: 'Refund processed', duration: 4000 })
      await loadOrders()
    }
  }

  // Telegram Stars (XTR) are whole units; fiat amounts are stored in minor units.
  const formatMoney = (amount: number, currency: string) =>
    currency === 'XTR' ? `${Number(amount)} ${currency}` : `${(Number(amount) / 100).toFixed(2)} ${currency}`

  const remainedDays = (expiry: number) => HumanReadable.remainedDays(expiry)

  const reloadAll = async () => {
    loading.value = true
    await Promise.all([loadSettings(), loadStatus(), loadInbounds(), loadOutbounds(), loadBindings(), loadTariffs(), loadOrders()])
    loading.value = false
  }

  onMounted(reloadAll)

  return {
    autoInbounds,
    autoRegister,
    bindingActions,
    bindingColumns,
    bindingDialog,
    bindingEdit,
    bindingHeaders,
    bindings,
    bindingsLoading,
    broadcastDialog,
    broadcastLoading,
    broadcastResult,
    broadcastText,
    clientOptions,
    cryptoEnabled,
    currencyOptions,
    deleteTariff,
    doRefund,
    doUnbind,
    enabled,
    externalEnabled,
    formatMoney,
    handleBindingAction,
    handleOrderAction,
    handleTariffAction,
    inboundOptions,
    loading,
    nexus,
    normalizeCurrency,
    openAddBinding,
    openBinding,
    openRefund,
    openTariff,
    openUnbindConfirm,
    orderActions,
    orderColumns,
    orderHeaders,
    orderStatusColor,
    orderStatusTone,
    orders,
    ordersLoading,
    outboundOptions,
    paymasterEnabled,
    recipientCount,
    remainedDays,
    refundBusy,
    refundDialog,
    refundEdit,
    reloadAll,
    saveBinding,
    saveSettings,
    saveTariff,
    secretboxKeySet,
    sendBroadcast,
    settings,
    starsEnabled,
    stripeEnabled,
    tab,
    tariffActions,
    tariffColumns,
    tariffDialog,
    tariffEdit,
    tariffHeaders,
    tariffs,
    tariffsLoading,
    transportModes,
    unbindBusy,
    unbindDialog,
    unbindEdit,
    yooEnabled,
  }
}

export type PaidSubscriptionsPage = ReturnType<typeof usePaidSubscriptionsPage>
