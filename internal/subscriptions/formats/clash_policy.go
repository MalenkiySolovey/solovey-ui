package formats

import "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/canonical"

type ClashUDPPolicy = canonical.ClashUDPPolicy

const (
	ClashUDPDefault  = canonical.ClashUDPDefault
	ClashUDPEnabled  = canonical.ClashUDPEnabled
	ClashUDPDisabled = canonical.ClashUDPDisabled
)

func ParseClashUDPPolicy(value string) (ClashUDPPolicy, error) {
	return canonical.ParseClashUDPPolicy(value)
}
