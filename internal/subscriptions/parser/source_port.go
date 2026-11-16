package parser

import (
	"context"
	"errors"
	"github.com/MalenkiySolovey/solovey-host/subscriptions/canonical"
	"github.com/MalenkiySolovey/solovey-host/subscriptions/parseport"
	suburi "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/uri"
)

// SourceParser adapts the existing shared parsers to bounded Component input.
// Parsing creates no publisher, source worker, registration or runtime writer.
type SourceParser struct{}

var _ parseport.Parser = SourceParser{}

func (SourceParser) Parse(ctx context.Context, content []byte, options parseport.Options) (parseport.Result, error) {
	if ctx == nil {
		return parseport.Result{}, errors.New("subscription parser context is required")
	}
	if err := ctx.Err(); err != nil {
		return parseport.Result{}, err
	}
	input := string(content)
	parseOptions := ParseOptions{GroupAdaptation: options.GroupAdaptation, ConversionPolicy: options.ConversionPolicy}
	var outbounds []map[string]any
	var err error
	switch options.Format {
	case canonical.FormatXray:
		outbounds, err = ParseXrayOutboundsWithOptions(input, parseOptions)
	case canonical.FormatClash:
		outbounds, err = ParseClashOutboundsWithOptions(input, parseOptions)
	case canonical.FormatSingBox:
		outbounds, err = ParseSingBoxOutbounds(input)
	case canonical.FormatURI:
		outbounds, err = ParseExternalLinkOutbounds(input, suburi.Parse)
	default:
		return parseport.Result{}, errors.New("unsupported subscription source format")
	}
	if err != nil {
		return parseport.Result{}, err
	}
	if err := ctx.Err(); err != nil {
		return parseport.Result{}, err
	}
	return parseport.Result{Outbounds: outbounds, Canonical: canonical.ObserveOutbounds(options.Format, outbounds)}, nil
}
