package componentdoctor

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"time"

	settingcatalog "github.com/MalenkiySolovey/solovey-ui/internal/settings/catalog"
	settingsmanager "github.com/MalenkiySolovey/solovey-ui/internal/settings/manager"
	"gorm.io/gorm"
)

func checkPanel(ctx context.Context, db *gorm.DB) error {
	defaults := settingcatalog.WebDefaults()
	settings := settingsmanager.Manager{
		DB:           func() *gorm.DB { return db },
		DefaultValue: func(key string) (string, bool) { value, ok := defaults[key]; return value, ok },
	}
	listen, err := settings.GetString(settingcatalog.WebListenKey)
	if err != nil {
		return fmt.Errorf("read configured panel listener: %w", err)
	}
	portText, err := settings.GetString(settingcatalog.WebPortKey)
	if err != nil {
		return fmt.Errorf("read configured panel port: %w", err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("configured panel port is invalid")
	}
	for _, host := range probeHosts(listen) {
		dialer := net.Dialer{Timeout: time.Second}
		connection, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
		if err == nil {
			return connection.Close()
		}
	}
	return fmt.Errorf("configured panel listener is unavailable")
}

// Wildcard binds are probed on their corresponding loopback family. Specific
// binds retain their exact address; a different local listener is not evidence.
func probeHosts(listen string) []string {
	if listen == "" {
		return []string{"127.0.0.1", "::1"}
	}
	if ip := net.ParseIP(listen); ip != nil && ip.IsUnspecified() {
		if ip.To4() != nil {
			return []string{"127.0.0.1"}
		}
		return []string{"::1"}
	}
	return []string{listen}
}
