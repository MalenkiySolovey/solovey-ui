//go:build !minimal

// Package blockcheckfixture registers the optional owner for transport tests.
// It does not require the generated application composition or expose its own routes.
package blockcheckfixture

import _ "github.com/MalenkiySolovey/solovey-ui/components/blockcheck"
