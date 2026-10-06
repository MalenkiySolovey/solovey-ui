import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { createI18n } from 'vue-i18n'
import { createSSRApp } from 'vue'
import { renderToString } from '@vue/server-renderer'
import en from './en'
import ru from './ru'
import fa from './fa'
import vi from './vi'
import zhcn from './zhcn'
import zhtw from './zhtw'
import SaveGuardNotice from '@/components/fields/SaveGuardNotice.vue'

const keys = [
  "types.hy.recvWindow",
  "types.anytls.paddingScheme",
  "types.hy.disableMtuDiscovery",
  "types.hy.recvWindowConn",
  "types.hy.recvWindowClient",
  "types.hy.maxConnClient",
  "types.hy.masquerade",
  "types.hy.authFailureServer",
  "types.hy.fileRoot",
  "types.hy.httpCode",
  "types.hy.targetUrl",
  "types.hy.rewriteHost",
  "types.hy.content",
  "types.wg.deviceId",
  "types.wg.accessToken",
  "types.wg.licenseKey",
  "types.wg.reserved",
  "rule.udpTimeout",
  "types.ssmApi",
  "tls.fingerprint",
  "tls.shortId",
  "transport.maxEarlyData",
  "transport.earlyDataHeaderName",
  "dns.rule.action.predefined",
  "tls.shortIds",
  "tls.maxTimeDifference",
  "types.wg.qrCode",
  "pages.basics",
  "capability.unknown",
  "capability.contextUnsupported",
  "form.saveIdentity",
  "form.savePortRequired",
  "form.savePortInvalid",
  "form.saveTls",
  "form.savePending",
  "nexus.overview.kpi.timeZone",
  "nexus.overview.kpi.systemTimeZone",
  "inboundGuidance.title",
  "inboundGuidance.vless",
  "inboundGuidance.vmess",
  "inboundGuidance.trojan",
  "inboundGuidance.shadowsocks",
  "inboundGuidance.socks",
  "inboundGuidance.http",
  "inboundGuidance.tlsSelected",
  "inboundGuidance.tlsOptional",
  "inboundGuidance.transport"
]
const inventory = [
  {
    "file": "components/subscription/OutJson.vue",
    "label": "Recv window",
    "key": "types.hy.recvWindow"
  },
  {
    "file": "components/protocols/AnyTls.vue",
    "label": "Padding scheme",
    "key": "types.anytls.paddingScheme"
  },
  {
    "file": "components/protocols/Hysteria.vue",
    "label": "Disable MTU discovery",
    "key": "types.hy.disableMtuDiscovery"
  },
  {
    "file": "components/protocols/Hysteria.vue",
    "label": "Recv window conn",
    "key": "types.hy.recvWindowConn"
  },
  {
    "file": "components/protocols/Hysteria.vue",
    "label": "Recv window",
    "key": "types.hy.recvWindow"
  },
  {
    "file": "components/protocols/Hysteria.vue",
    "label": "Recv window client",
    "key": "types.hy.recvWindowClient"
  },
  {
    "file": "components/protocols/Hysteria.vue",
    "label": "Max conn client",
    "key": "types.hy.maxConnClient"
  },
  {
    "file": "components/protocols/Hysteria2.vue",
    "label": "Hysteria2 Masquerade",
    "key": "types.hy.masquerade"
  },
  {
    "file": "components/protocols/Hysteria2.vue",
    "label": "HTTP3 server on auth fails",
    "key": "types.hy.authFailureServer"
  },
  {
    "file": "components/protocols/Hysteria2.vue",
    "label": "File server root directory",
    "key": "types.hy.fileRoot"
  },
  {
    "file": "components/protocols/Hysteria2.vue",
    "label": "HTTP Code",
    "key": "types.hy.httpCode"
  },
  {
    "file": "components/protocols/Hysteria2.vue",
    "label": "Target URL",
    "key": "types.hy.targetUrl"
  },
  {
    "file": "components/protocols/Hysteria2.vue",
    "label": "Rewrite Host",
    "key": "types.hy.rewriteHost"
  },
  {
    "file": "components/protocols/Hysteria2.vue",
    "label": "Content",
    "key": "types.hy.content"
  },
  {
    "file": "components/protocols/Warp.vue",
    "label": "Device ID",
    "key": "types.wg.deviceId"
  },
  {
    "file": "components/protocols/Warp.vue",
    "label": "Access Token",
    "key": "types.wg.accessToken"
  },
  {
    "file": "components/protocols/Warp.vue",
    "label": "License Key",
    "key": "types.wg.licenseKey"
  },
  {
    "file": "components/protocols/Warp.vue",
    "label": "Reserved",
    "key": "types.wg.reserved"
  },
  {
    "file": "components/protocols/Warp.vue",
    "label": "UDP Timeout",
    "key": "rule.udpTimeout"
  },
  {
    "file": "components/protocols/Wireguard.vue",
    "label": "UDP Timeout",
    "key": "rule.udpTimeout"
  },
  {
    "file": "components/services/SSMAPI.vue",
    "label": "Shadowsocks API",
    "key": "types.ssmApi"
  },
  {
    "file": "components/tls/OutTLS.vue",
    "label": "Fingerprint",
    "key": "tls.fingerprint"
  },
  {
    "file": "components/tls/OutTLS.vue",
    "label": "Short ID",
    "key": "tls.shortId"
  },
  {
    "file": "components/transports/WebSocket.vue",
    "label": "Max Early Data",
    "key": "transport.maxEarlyData"
  },
  {
    "file": "components/transports/WebSocket.vue",
    "label": "Early Data Header Name",
    "key": "transport.earlyDataHeaderName"
  },
  {
    "file": "layouts/modals/Dns.vue",
    "label": "Predefined",
    "key": "dns.rule.action.predefined"
  },
  {
    "file": "layouts/modals/Tls.vue",
    "label": "Short IDs",
    "key": "tls.shortIds"
  },
  {
    "file": "layouts/modals/Tls.vue",
    "label": "Max Time Diference",
    "key": "tls.maxTimeDifference"
  },
  {
    "file": "components/tls/TlsOptionsMenu.vue",
    "label": "Max Time Difference",
    "key": "tls.maxTimeDifference"
  },
  {
    "file": "layouts/modals/Tls.vue",
    "label": "Fingerprint",
    "key": "tls.fingerprint"
  },
  {
    "file": "components/nexus/drawers/TlsDrawer.vue",
    "label": "Short IDs",
    "key": "tls.shortIds"
  },
  {
    "file": "components/nexus/drawers/TlsDrawer.vue",
    "label": "Max Time Diference",
    "key": "tls.maxTimeDifference"
  },
  {
    "file": "components/nexus/drawers/TlsDrawer.vue",
    "label": "Max Time Difference",
    "key": "tls.maxTimeDifference"
  },
  {
    "file": "components/nexus/drawers/TlsDrawer.vue",
    "label": "Fingerprint",
    "key": "tls.fingerprint"
  },
  {
    "file": "layouts/modals/WgQrCode.vue",
    "label": "Wireguard QrCode",
    "key": "types.wg.qrCode"
  },
  {
    "file": "views/Settings.vue",
    "label": "Basics (Singbox)",
    "key": "pages.basics"
  }
]
const pick = (messages: any, key: string) => key.split('.').reduce((value: any, part) => value?.[part], messages)
describe('Wave 5 core presentation localization contract', () => {
  for (const [locale, messages] of Object.entries({ en, ru })) {
    it(locale + ' structurally defines every selected technical/guard/guidance key', () => {
      for (const key of keys) expect(pick(messages, key), key).toEqual(expect.any(String))
    })
  }
  for (const [locale, messages] of Object.entries({ en, ru, fa, vi, zhcn, zhtw })) {
    it(locale + ' renders all keys through the established English fallback', () => {
      const renderer = createI18n({ legacy: false, locale, fallbackLocale: 'en', messages: { en, [locale]: messages }, missingWarn: false, fallbackWarn: false })
      for (const key of keys) { const text = renderer.global.t(key); expect(text, key).not.toBe(key); expect(text.trim().length, key).toBeGreaterThan(0) }
    })
  }
  it('every selected label consumes its locale owner and no hardcoded label remains', () => {
    for (const row of inventory) {
      const source = readFileSync(fileURLToPath(new URL('../' + row.file, import.meta.url)), 'utf8')
      expect(source, row.file).toContain("$t('" + row.key + "')")
      expect(source).not.toContain('label="' + row.label + '"')
      expect(source).not.toContain('subtitle="' + row.label + '"')
      expect(source).not.toContain('>' + row.label + '<')
    }
  })
  for (const locale of ['en', 'ru']) {
    it(locale + ' renders a localized accessible shared save reason', async () => {
      const renderer = createI18n({ legacy: false, locale, messages: { en, ru } })
      const app = createSSRApp(SaveGuardNotice, { reasons: ['form.saveIdentity', 'capability.unknown'] })
      app.use(renderer)
      const html = await renderToString(app)
      expect(html).toContain('role="status"')
      expect(html).toContain('aria-live="polite"')
      expect(html).toContain(renderer.global.t('form.saveIdentity'))
      expect(html).toContain(renderer.global.t('capability.unknown'))
      expect(html).not.toContain('form.saveIdentity')
    })
  }
})
