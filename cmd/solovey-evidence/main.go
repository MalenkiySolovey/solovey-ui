package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/internal/evidencebundle"
	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
)

const maxOperatorInput = 64 << 10

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "solovey evidence operator:", err)
		os.Exit(1)
	}
}

func run(args []string, output io.Writer) error {
	return runWithRecentReader(args, output, broker.ReadRecentDiagnostics)
}

func runWithRecentReader(args []string, output io.Writer, readRecent func() (broker.RecentDiagnostics, error)) error {
	if len(args) == 1 && args[0] == "recent" {
		if readRecent == nil {
			return errors.New("recent diagnostic reader is unavailable")
		}
		document, err := readRecent()
		if err != nil {
			return err
		}
		return writeJSON(output, document)
	}
	if len(args) < 2 {
		return errors.New("usage: solovey-evidence recent | <arm|begin-epoch|close-epoch|activate|record|record-failure|finalize|verify> <root> [input]")
	}
	action, root := args[0], args[1]
	switch action {
	case "arm":
		if len(args) != 3 {
			return errors.New("arm requires one contract JSON file")
		}
		var contract evidencebundle.Contract
		if err := readExactJSON(args[2], &contract); err != nil {
			return err
		}
		recorder, err := evidencebundle.Arm(root, contract)
		if err != nil {
			return err
		}
		return writeJSON(output, recorder.Contract())
	case "begin-epoch":
		if len(args) != 3 {
			return errors.New("begin-epoch requires one epoch JSON file")
		}
		var input evidencebundle.EpochInput
		if err := readExactJSON(args[2], &input); err != nil {
			return err
		}
		recorder, err := evidencebundle.OpenArmed(root)
		if err != nil {
			return err
		}
		epoch, err := recorder.BeginEpoch(input)
		if err != nil {
			return err
		}
		return writeJSON(output, epoch)
	case "close-epoch":
		if len(args) != 2 {
			return errors.New("close-epoch accepts no input")
		}
		recorder, err := evidencebundle.OpenArmed(root)
		if err != nil {
			return err
		}
		seal, err := recorder.CloseEpoch()
		if err != nil {
			return err
		}
		return writeJSON(output, seal)
	case "activate":
		if len(args) != 4 {
			return errors.New("activate requires scenario and checkpoint")
		}
		recorder, err := evidencebundle.OpenArmed(root)
		if err != nil {
			return err
		}
		return recorder.ActivateCheckpoint(evidencebundle.Scenario(args[2]), args[3])
	case "record":
		if len(args) != 3 {
			return errors.New("record requires one checkpoint JSON file")
		}
		var input evidencebundle.CheckpointInput
		if err := readExactJSON(args[2], &input); err != nil {
			return err
		}
		recorder, err := evidencebundle.OpenArmed(root)
		if err != nil {
			return err
		}
		return recorder.RecordCheckpoint(input)
	case "record-failure":
		if len(args) != 3 {
			return errors.New("record-failure requires one bounded failure JSON file")
		}
		var input evidencebundle.FailureInput
		if err := readExactJSON(args[2], &input); err != nil {
			return err
		}
		recorder, err := evidencebundle.OpenArmed(root)
		if err != nil {
			return err
		}
		return recorder.RecordFailure(input)
	case "finalize":
		if len(args) != 2 {
			return errors.New("finalize accepts no input")
		}
		recorder, err := evidencebundle.OpenArmed(root)
		if err != nil {
			return err
		}
		manifest, finalizeErr := recorder.Finalize()
		if err := writeJSON(output, manifest); err != nil {
			return err
		}
		return finalizeErr
	case "verify":
		if len(args) != 3 {
			return errors.New("verify requires the expected contract JSON file")
		}
		var expected evidencebundle.Contract
		if err := readExactJSON(args[2], &expected); err != nil {
			return err
		}
		manifest, err := evidencebundle.Verify(root, expected)
		if err != nil {
			return err
		}
		return writeJSON(output, manifest)
	default:
		return fmt.Errorf("unknown evidence operator action %q", action)
	}
}

func readExactJSON(path string, value any) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > maxOperatorInput {
		return errors.New("operator input is not a bounded regular file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("operator input contains trailing JSON")
	}
	return nil
}

func writeJSON(output io.Writer, value any) error {
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}
