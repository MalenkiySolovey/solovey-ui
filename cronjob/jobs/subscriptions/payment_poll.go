package subscriptions

import (
	"context"

	paidtelegram "github.com/MalenkiySolovey/solovey-ui/paidsub/telegram"
)

// PaymentPollJob drives the experimental Paid Subscriptions out-of-band payment
// poll (CryptoBot) and stale-order expiry. It self-gates on paidSubEnabled.
type PaymentPollJob struct{}

func NewPaymentPollJob() *PaymentPollJob {
	return &PaymentPollJob{}
}

func (j *PaymentPollJob) Run() {
	paidtelegram.PollOnce(context.Background())
}
