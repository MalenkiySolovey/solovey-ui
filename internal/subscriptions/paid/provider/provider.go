package provider

import (
	"context"
	"strconv"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	paid "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/paid"
)

// ProviderKind identifies a payment backend.
type ProviderKind string

const (
	ProviderStars     ProviderKind = "stars"
	ProviderYooKassa  ProviderKind = "yookassa"
	ProviderStripe    ProviderKind = "stripe"
	ProviderPayMaster ProviderKind = "paymaster"
	ProviderCryptoBot ProviderKind = "cryptobot"
	ProviderExternal  ProviderKind = "external"
)

// InvoiceMethod tells the bot how to deliver an invoice to the user.
type InvoiceMethod int

const (
	InvoiceTelegramNative InvoiceMethod = iota
	InvoiceURL
	InvoiceManualLink
)

type LabeledPrice struct {
	Label  string `json:"label"`
	Amount int64  `json:"amount"`
}

// Invoice is the provider-agnostic result of preparing a payment.
type Invoice struct {
	Method        InvoiceMethod
	Title         string
	Description   string
	ProviderToken string
	Currency      string
	Prices        []LabeledPrice
	Payload       string
	PayURL        string
	ProviderRef   string
}

// PollResult reports an out-of-band confirmed payment.
type PollResult struct {
	OrderID          uint
	ProviderChargeID string
	RawPayload       []byte
}

// PaymentProvider prepares invoices and declares how it confirms.
type PaymentProvider interface {
	Kind() ProviderKind
	Title(language string) string
	CreateInvoice(ctx context.Context, order *paid.PaymentOrder, tariff *paid.Tariff, client *model.Client) (*Invoice, error)
}

// PollingProvider is implemented by providers confirmed via polling.
type PollingProvider interface {
	Poll(ctx context.Context, pending []paid.PaymentOrder) ([]PollResult, error)
}

func ProviderTitle(kind ProviderKind, language string) string {
	switch kind {
	case ProviderStars:
		return "Telegram Stars"
	case ProviderYooKassa:
		return "YooKassa"
	case ProviderStripe:
		return "Stripe"
	case ProviderPayMaster:
		return "PayMaster"
	case ProviderCryptoBot:
		return "CryptoBot"
	case ProviderExternal:
		if language == "ru" {
			return "Оплата по ссылке"
		}
		return "External link"
	}
	return string(kind)
}

type telegramProvider struct {
	kind  ProviderKind
	token string
}

func NewTelegramProvider(kind ProviderKind, token string) PaymentProvider {
	return &telegramProvider{kind: kind, token: token}
}

func (p *telegramProvider) Kind() ProviderKind           { return p.kind }
func (p *telegramProvider) Title(language string) string { return ProviderTitle(p.kind, language) }

func (p *telegramProvider) CreateInvoice(ctx context.Context, order *paid.PaymentOrder, tariff *paid.Tariff, client *model.Client) (*Invoice, error) {
	desc := tariff.Description
	if strings.TrimSpace(desc) == "" {
		desc = tariff.Name
	}
	inv := &Invoice{
		Method:      InvoiceTelegramNative,
		Title:       tariff.Name,
		Description: desc,
		Payload:     order.IdempotencyKey,
	}
	if p.kind == ProviderStars {
		inv.Currency = "XTR"
		inv.ProviderToken = ""
		inv.Prices = []LabeledPrice{{Label: tariff.Name, Amount: tariff.StarsAmount}}
	} else {
		inv.Currency = order.Currency
		inv.ProviderToken = p.token
		inv.Prices = []LabeledPrice{{Label: tariff.Name, Amount: tariff.Price}}
	}
	return inv, nil
}

type externalProvider struct {
	template string
}

func NewExternalProvider(template string) PaymentProvider {
	return &externalProvider{template: template}
}

func (p *externalProvider) Kind() ProviderKind { return ProviderExternal }
func (p *externalProvider) Title(language string) string {
	return ProviderTitle(ProviderExternal, language)
}

func (p *externalProvider) CreateInvoice(ctx context.Context, order *paid.PaymentOrder, tariff *paid.Tariff, client *model.Client) (*Invoice, error) {
	return &Invoice{
		Method:  InvoiceManualLink,
		Title:   tariff.Name,
		PayURL:  RenderExternalURL(p.template, order, tariff, client),
		Payload: order.IdempotencyKey,
	}, nil
}

func RenderExternalURL(tmpl string, order *paid.PaymentOrder, tariff *paid.Tariff, client *model.Client) string {
	r := strings.NewReplacer(
		"{orderId}", strconv.FormatUint(uint64(order.Id), 10),
		"{clientId}", strconv.FormatUint(uint64(client.Id), 10),
		"{tariffId}", strconv.FormatUint(uint64(tariff.Id), 10),
		"{amount}", strconv.FormatInt(order.Amount, 10),
		"{currency}", order.Currency,
		"{key}", order.IdempotencyKey,
	)
	return r.Replace(tmpl)
}
