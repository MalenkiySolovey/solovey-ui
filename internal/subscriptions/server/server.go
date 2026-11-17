// Package server composes the canonical subscription listener with Product socket adapters.
package server

import (
	owner "github.com/MalenkiySolovey/solovey-ui/backend/subscriptions/server"
	configlogging "github.com/MalenkiySolovey/solovey-ui/config/logging"
	"github.com/MalenkiySolovey/solovey-ui/logger"
	"github.com/MalenkiySolovey/solovey-ui/network/autohttps"
	"github.com/MalenkiySolovey/solovey-ui/network/bind"
	"github.com/gin-gonic/gin"
	"net"
	"time"
)

type Settings = owner.Settings
type BaseRoutes = owner.BaseRoutes
type FormatHandlersFactory = owner.FormatHandlersFactory
type RuntimeServer = owner.RuntimeServer
type FormatHandlers = owner.FormatHandlers
type RateLimiter = owner.RateLimiter
type DisplaySettingsReader = owner.DisplaySettingsReader
type DisplaySettings = owner.DisplaySettings
type Hooks = owner.Hooks

const (
	SubEnumWindow            = owner.SubEnumWindow
	SubEnumThreshold         = owner.SubEnumThreshold
	SubEnumMaxKeys           = owner.SubEnumMaxKeys
	RateLimitWindow          = owner.RateLimitWindow
	DefaultRateLimitRequests = owner.DefaultRateLimitRequests
	RateLimitSettingTTL      = owner.RateLimitSettingTTL
	RateLimitMaxKeys         = owner.RateLimitMaxKeys
	RateLimitGCEvery         = owner.RateLimitGCEvery
	DisplaySettingsTTL       = owner.DisplaySettingsTTL
)

func NewRuntimeServer(s Settings, base BaseRoutes, formats FormatHandlersFactory) *RuntimeServer {
	return owner.NewRuntimeServer(s, base, formats, owner.RuntimePorts{Debug: configlogging.IsDebug, Info: logger.Info, Warning: logger.Warning, Listen: func(address, host, port string) (owner.ListenerResult, error) {
		r, err := bind.ListenWithFallbackResult(address, host, port)
		return owner.ListenerResult{Listener: r.Listener, RequestedAddr: r.RequestedAddr, FallbackAddr: r.FallbackAddr, Fallback: r.Fallback, BindError: r.BindError}, err
	}, WrapAutoHTTPS: func(l net.Listener, authority string) net.Listener {
		return autohttps.NewAutoHttpsListener(l, authority)
	}})
}
func RegisterHooks(h Hooks) (func(), error)         { return owner.RegisterHooks(h) }
func NewRateLimiter() *RateLimiter                  { return owner.NewRateLimiter() }
func CanonicalClientIP(ip string) string            { return owner.CanonicalClientIP(ip) }
func NoteSubNotFound(ip string)                     { owner.NoteSubNotFound(ip) }
func SafeHeaders(values []string, max int) []string { return owner.SafeHeaders(values, max) }
func CachedDisplaySettings(reader DisplaySettingsReader, now time.Time) (DisplaySettings, error) {
	return owner.CachedDisplaySettings(reader, now)
}
func ResetDisplaySettingsCache() { owner.ResetDisplaySettingsCache() }
func RegisterFormatRoute(e *gin.Engine, registered map[string]string, path, format string, h FormatHandlers, r *RateLimiter) error {
	return owner.RegisterFormatRoute(e, registered, path, format, h, r)
}
func RememberPath(registered map[string]string, path, format string) error {
	return owner.RememberPath(registered, path, format)
}
func JoinPath(base, child string) string    { return owner.JoinPath(base, child) }
func NormalizeRoutePath(path string) string { return owner.NormalizeRoutePath(path) }
