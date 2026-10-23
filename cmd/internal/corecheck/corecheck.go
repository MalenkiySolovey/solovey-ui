package corecheck

import (
	"encoding/json"
	"fmt"
	"io"

	coreprovider "github.com/MalenkiySolovey/solovey-ui/core/provider"
	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/validation"
)

func Run(command string, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) != 0 || command != "core-info" && command != "core-check" {
		fmt.Fprintln(stderr, "core-info and core-check accept no arguments; core-check reads JSON from stdin")
		return 2
	}
	var result any
	code := 1
	if command == "core-info" {
		identity := coreprovider.Current()
		result = identity
		if coreprovider.SourceIdentityVerified(identity) {
			code = 0
		}
	} else {
		raw, err := io.ReadAll(io.LimitReader(stdin, validation.MaxTargetConfigBytes+1))
		if err != nil {
			fmt.Fprintln(stderr, "core-check: configuration input could not be read")
			return 1
		}
		report := validation.CheckTargetConfig(raw)
		result = report
		if report.State == "VALID" {
			code = 0
		}
	}
	if err := json.NewEncoder(stdout).Encode(result); err != nil {
		fmt.Fprintln(stderr, "core-check: report could not be written")
		return 1
	}
	return code
}
