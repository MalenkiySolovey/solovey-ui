//go:build !minimal

// Package telegram implements the client-facing paid-subscription bot. It
// orchestrates commands and payments through the shared internal Telegram
// transport and paid-subscriptions domain stores.
package telegram
