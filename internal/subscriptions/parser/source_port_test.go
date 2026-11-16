package parser

import (
	"context"
	"errors"
	"github.com/MalenkiySolovey/solovey-host/subscriptions/canonical"
	"github.com/MalenkiySolovey/solovey-host/subscriptions/conversion"
	"github.com/MalenkiySolovey/solovey-host/subscriptions/parseport"
	"testing"
)

func TestSharedSourceParserPortUsesExistingFormatOwners(t *testing.T) {
	var parser parseport.Parser = SourceParser{}
	for _, item := range []struct{ format, raw string }{
		{canonical.FormatSingBox, `{"outbounds":[{"type":"socks","tag":"s","server":"example.org","server_port":1080}]}`},
		{canonical.FormatXray, `{"outbounds":[{"tag":"x","protocol":"socks","settings":{"servers":[{"address":"example.org","port":1080}]}}]}`},
		{canonical.FormatClash, "proxies:\n  - name: c\n    type: socks5\n    server: example.org\n    port: 1080\n"},
		{canonical.FormatURI, "socks5://fixture:synthetic-password@example.org:1080#u"},
	} {
		t.Run(item.format, func(t *testing.T) {
			result, err := parser.Parse(context.Background(), []byte(item.raw), parseport.Options{Format: item.format, ConversionPolicy: conversion.DefaultPolicy()})
			if err != nil || len(result.Outbounds) != 1 || len(result.Canonical.Connections) != 1 || result.Canonical.Connections[0].Protocol != "socks" {
				t.Fatalf("format owner did not produce a canonical endpoint: %+v %v", result, err)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := parser.Parse(ctx, []byte("socks5://fixture:synthetic-password@example.org:1080"), parseport.Options{Format: canonical.FormatURI})
	if !errors.Is(err, context.Canceled) || len(result.Outbounds) != 0 || len(result.Canonical.Connections) != 0 {
		t.Fatal("cancelled parser returned accepted state", err)
	}
	if _, err := parser.Parse(context.Background(), []byte("socks5://fixture:synthetic-password@example.org:1080"), parseport.Options{Format: "unknown"}); err == nil {
		t.Fatal("unknown format fell back to URI")
	}
}
