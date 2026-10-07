package box

import (
	"fmt"
	"github.com/sagernet/sing-box/option"
)

// Managers support live replacement. Initial construction must not replace an
// unstarted resource, because its manager does not own Close until Initialize.
func validateConfiguredTags(options option.Options) error {
	seen := make(map[string]bool)
	check := func(kind, tag string, index int) error {
		key := kind + "/" + indexedOptionTag(index, tag)
		if seen[key] {
			return fmt.Errorf("duplicate %s tag at index %d", kind, index)
		}
		seen[key] = true
		return nil
	}
	for i, v := range options.Endpoints {
		if err := check("outbound", v.Tag, i); err != nil {
			return err
		}
	}
	for i, v := range options.Outbounds {
		if err := check("outbound", v.Tag, i); err != nil {
			return err
		}
	}
	for i, v := range options.Inbounds {
		if err := check("inbound", v.Tag, i); err != nil {
			return err
		}
	}
	for i, v := range options.Services {
		if err := check("service", v.Tag, i); err != nil {
			return err
		}
	}
	for i, v := range options.CertificateProviders {
		if err := check("certificate-provider", v.Tag, i); err != nil {
			return err
		}
	}
	if options.DNS != nil {
		for i, v := range options.DNS.Servers {
			if err := check("dns", v.Tag, i); err != nil {
				return err
			}
		}
	}
	for i, v := range options.HTTPClients {
		if err := check("http-client", v.Tag, i); err != nil {
			return err
		}
	}
	return nil
}
