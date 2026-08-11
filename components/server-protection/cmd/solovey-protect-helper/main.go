// solovey-protect-helper is the isolated restricted-helper binary. Its normal
// mode accepts one strict JSON request on stdin. The only CLI argument is the
// non-mutating --smoke mode; arbitrary flags and command text are rejected.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	protectionhelper "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/helper"
)

func main() {
	arguments := os.Args[1:]
	if len(arguments) == 1 && arguments[0] == "--smoke" {
		response := protectionhelper.SmokeResponse("helper-binary")
		if err := json.NewEncoder(os.Stdout).Encode(response); err != nil {
			os.Exit(protectionhelper.ExitInternal)
		}
		return
	}
	if len(arguments) != 0 {
		_, _ = fmt.Fprintln(os.Stderr, "solovey-protect-helper accepts only --smoke or a JSON request on stdin")
		os.Exit(protectionhelper.ExitInvalidRequest)
	}
	workingDirectory, err := os.Getwd()
	if err != nil {
		os.Exit(protectionhelper.ExitInternal)
	}
	root, err := protectionhelper.NewManagedRoot(workingDirectory)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "helper working directory is not the managed runtime root")
		os.Exit(protectionhelper.ExitInvalidRequest)
	}
	os.Exit(protectionhelper.ServeOnce(os.Stdin, os.Stdout, root))
}
