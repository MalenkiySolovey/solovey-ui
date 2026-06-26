package telegram

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	sublocal "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/local"
	paidcore "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/paid"
	paidstore "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/paid/store"
)

func (b *Bot) menuKeyboard(l lang) *inlineKeyboard {
	return &inlineKeyboard{InlineKeyboard: [][]inlineButton{
		{{Text: tr(l, "menu_links"), CallbackData: "links"}, {Text: tr(l, "menu_qr"), CallbackData: "qr"}},
		{{Text: tr(l, "menu_stats"), CallbackData: "stats"}, {Text: tr(l, "menu_payment"), CallbackData: "payment"}},
		{{Text: tr(l, "menu_help"), CallbackData: "help"}},
	}}
}

// paymentMenuKeyboard is the "Payment" submenu: buy/renew, my purchases, refund.
func (b *Bot) paymentMenuKeyboard(l lang) *inlineKeyboard {
	return &inlineKeyboard{InlineKeyboard: [][]inlineButton{
		{{Text: tr(l, "menu_buy"), CallbackData: "buy"}},
		{{Text: tr(l, "menu_orders"), CallbackData: "orders"}},
		{{Text: tr(l, "menu_refund"), CallbackData: "refund"}},
		{{Text: tr(l, "menu_back"), CallbackData: "menu"}},
	}}
}

// backToPaymentKeyboard is a single "Back" button returning to the payment menu.
func (b *Bot) backToPaymentKeyboard(l lang) *inlineKeyboard {
	return &inlineKeyboard{InlineKeyboard: [][]inlineButton{
		{{Text: tr(l, "menu_back"), CallbackData: "payment"}},
	}}
}

func (b *Bot) subURL(client *model.Client) (string, error) {
	if client.SubSecret == "" {
		return "", nil
	}
	host, _ := b.setting.GetWebDomain()
	base, err := b.setting.GetFinalSubURI(host)
	if err != nil {
		return "", err
	}
	if base == "" {
		return "", nil
	}
	return base + client.SubSecret, nil
}

func (b *Bot) buildLinksText(client *model.Client, l lang) string {
	var sb strings.Builder
	sb.WriteString(tr(l, "links_title") + "\n")
	if sub, err := b.subURL(client); err == nil && sub != "" {
		sb.WriteString(sub + "\n")
	}
	if len(client.Links) > 0 {
		enabled, err := b.setting.GetSubLinkEnable()
		var links []string
		if err != nil || enabled {
			links = sublocal.ResolveClientLinks(client.Links, sublocal.LinkModeAll, "")
		}
		if len(links) > 0 {
			sb.WriteString("\n")
			for _, lk := range links {
				sb.WriteString(lk + "\n")
			}
		}
	}
	out := strings.TrimSpace(sb.String())
	if out == tr(l, "links_title") || out == "" {
		return tr(l, "links_none")
	}
	return out
}

func (b *Bot) buildStatsText(client *model.Client, l lang) string {
	used := client.Up + client.Down
	var sb strings.Builder
	sb.WriteString(tr(l, "stats_title") + "\n\n")
	sb.WriteString(fmt.Sprintf("%s: %s\n", tr(l, "stats_used"), humanBytes(used)))
	if client.Volume > 0 {
		pct := int(used * 100 / client.Volume)
		if pct > 100 {
			pct = 100
		}
		sb.WriteString(fmt.Sprintf("%s: %s (%d%%)\n", tr(l, "stats_limit"), humanBytes(client.Volume), pct))
		sb.WriteString(progressBar(pct) + "\n")
	} else {
		sb.WriteString(fmt.Sprintf("%s: %s\n", tr(l, "stats_limit"), tr(l, "stats_unlim")))
	}
	if client.Expiry > 0 {
		if client.Expiry < nowUnix() {
			sb.WriteString(fmt.Sprintf("%s: %s\n", tr(l, "stats_expiry"), tr(l, "stats_expired")))
		} else {
			days := (client.Expiry - nowUnix()) / 86400
			sb.WriteString(fmt.Sprintf("%s: %d %s\n", tr(l, "stats_expiry"), days, tr(l, "stats_days")))
		}
	}
	if client.Enable {
		sb.WriteString(tr(l, "stats_enabled") + "\n")
	} else {
		sb.WriteString(tr(l, "stats_disabled") + "\n")
	}
	if b.isOnline(client.Name) {
		sb.WriteString(tr(l, "stats_online") + "\n")
	} else {
		sb.WriteString(tr(l, "stats_offline") + "\n")
	}
	return strings.TrimSpace(sb.String())
}

func (b *Bot) isOnline(name string) bool {
	onl, err := b.stats.GetOnlines()
	if err != nil {
		return false
	}
	for _, n := range onl.User {
		if n == name {
			return true
		}
	}
	return false
}

// tariffNameMap returns tariffId → name for labelling orders (best-effort).
func (b *Bot) tariffNameMap() map[uint]string {
	names := map[uint]string{}
	all, err := paidstore.ListTariffs(dbsqlite.DB())
	if err != nil {
		return names
	}
	for i := range all {
		names[all[i].Id] = all[i].Name
	}
	return names
}

func (b *Bot) buildOrdersText(orders []paidcore.PaymentOrder, l lang) string {
	names := b.tariffNameMap()
	var sb strings.Builder
	sb.WriteString(tr(l, "orders_title") + "\n")
	for i := range orders {
		o := orders[i]
		date := ""
		if o.CreatedAt > 0 {
			date = time.Unix(o.CreatedAt, 0).Format("2006-01-02")
		}
		sb.WriteString(fmt.Sprintf("\n• %s: %s\n  %s · %s",
			orderTariffName(&o, names),
			formatOrderAmount(o.Amount, o.Currency),
			orderStatusLabel(o.Status, l),
			date,
		))
	}
	return strings.TrimSpace(sb.String())
}

func orderTariffName(o *paidcore.PaymentOrder, names map[uint]string) string {
	if name := names[o.TariffId]; name != "" {
		return name
	}
	return "#" + strconv.FormatUint(uint64(o.Id), 10)
}

func refundOrderButtonLabel(o *paidcore.PaymentOrder, names map[uint]string) string {
	return fmt.Sprintf("%s: %s", orderTariffName(o, names), formatOrderAmount(o.Amount, o.Currency))
}

// orderStatusLabel localizes a status, falling back to the raw value.
func orderStatusLabel(status string, l lang) string {
	key := "order_status_" + status
	if v := tr(l, key); v != key {
		return v
	}
	return status
}

// ---- helpers ----

func parseCommand(text string) (cmd string, arg string) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "/") {
		return "", ""
	}
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return "", ""
	}
	cmd = fields[0]
	if i := strings.IndexByte(cmd, '@'); i >= 0 {
		cmd = cmd[:i]
	}
	if len(fields) > 1 {
		arg = fields[1]
	}
	return strings.ToLower(cmd), arg
}

func humanBytes(n int64) string {
	if n < 0 {
		n = 0
	}
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

func progressBar(pct int) string {
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	filled := pct / 10
	return "[" + strings.Repeat("\u2588", filled) + strings.Repeat("\u2591", 10-filled) + "]"
}

func chunkText(s string, max int) []string {
	if len(s) <= max {
		return []string{s}
	}
	var chunks []string
	var current strings.Builder
	flush := func() {
		if current.Len() > 0 {
			chunks = append(chunks, strings.TrimRight(current.String(), "\n"))
			current.Reset()
		}
	}
	for _, line := range strings.Split(s, "\n") {
		for len([]rune(line)) > max {
			flush()
			runes := []rune(line)
			chunks = append(chunks, string(runes[:max]))
			line = string(runes[max:])
		}
		if current.Len()+len(line)+1 > max && current.Len() > 0 {
			flush()
		}
		current.WriteString(line + "\n")
	}
	flush()
	return chunks
}
