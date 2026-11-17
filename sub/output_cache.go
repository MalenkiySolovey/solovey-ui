package sub

import (
	canonical "github.com/MalenkiySolovey/solovey-ui/backend/subscriptions"
	"github.com/MalenkiySolovey/solovey-ui/service"
)

func init()                         { service.RegisterSubscriptionOutputCacheInvalidator(ClearSubscriptionOutputCache) }
func ClearSubscriptionOutputCache() { canonical.ClearSubscriptionOutputCache() }
