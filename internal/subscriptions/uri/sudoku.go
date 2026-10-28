package uri

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/MalenkiySolovey/solovey-ui/internal/entities/jsonvalue"
	"github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/canonical"
	"github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/uri/codec"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
)

// Compact field names follow the stable S-UI-X v1.1.1 Sudoku client adapter
// (af9ae56f024828ece4463014a2f9bc698646ca2d:util/genLink.go).
type sudokuPayload struct {
	Host        string   `json:"h"`
	Port        int      `json:"p"`
	Key         string   `json:"k"`
	ASCII       string   `json:"a,omitempty"`
	AEAD        string   `json:"e,omitempty"`
	Packed      bool     `json:"x,omitempty"`
	Table       string   `json:"t,omitempty"`
	Tables      []string `json:"ts,omitempty"`
	DisableMask bool     `json:"hd,omitempty"`
	MaskMode    string   `json:"hm,omitempty"`
	MaskPath    string   `json:"hy,omitempty"`
}

func decodeSudokuPayload(link string) ([]byte, error) {
	scheme, encoded, ok := strings.Cut(link, "://")
	if !ok || !strings.EqualFold(scheme, "sudoku") || len(encoded) > 1<<20 {
		return nil, common.NewError("invalid Sudoku share link")
	}
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || !utf8.Valid(raw) || jsonvalue.StrictObject(raw) != nil {
		return nil, common.NewError("invalid Sudoku share link")
	}
	return raw, nil
}

func parseSudoku(link string, index int) (*map[string]any, string, error) {
	raw, err := decodeSudokuPayload(link)
	if err != nil {
		return nil, "", err
	}
	var payload sudokuPayload
	if json.Unmarshal(raw, &payload) != nil || payload.Key == "" || payload.Port < 1 || payload.Port > 65535 {
		return nil, "", common.NewError("invalid Sudoku share fields")
	}
	host, err := codec.NormalizeHost(payload.Host)
	if err != nil {
		return nil, "", common.NewError("invalid Sudoku endpoint")
	}
	table := payload.ASCII
	switch table {
	case "", "entropy":
		table = "prefer_entropy"
	case "ascii":
		table = "prefer_ascii"
	}
	httpMask := map[string]any{"enabled": !payload.DisableMask}
	if payload.MaskMode != "" {
		httpMask["mode"] = payload.MaskMode
	}
	if payload.MaskPath != "" {
		httpMask["path_root"] = payload.MaskPath
	}
	outbound := map[string]any{"type": "sudoku", "server": host, "server_port": payload.Port, "key": payload.Key, "table_type": table, "enable_pure_downlink": !payload.Packed, "http_mask": httpMask}
	if payload.AEAD != "" {
		outbound["aead_method"] = payload.AEAD
	}
	if payload.Table != "" {
		outbound["custom_table"] = payload.Table
	}
	if len(payload.Tables) > 0 {
		outbound["custom_tables"] = payload.Tables
	}
	tag := "sudoku-" + host
	if index > 0 {
		tag = fmt.Sprintf("%d.%s", index, tag)
	}
	outbound["tag"] = tag
	return &outbound, tag, nil
}

func renderSudoku(raw map[string]any) ([]string, error) {
	host, err := codec.NormalizeHost(mapString(raw, "server"))
	if err != nil {
		return nil, canonical.ErrUnsupportedProjection
	}
	port, err := parsePortValue(raw["server_port"])
	if err != nil {
		return nil, canonical.ErrUnsupportedProjection
	}
	key := mapString(raw, "key")
	if key == "" {
		return nil, canonical.ErrUnsupportedProjection
	}
	payload := sudokuPayload{Host: host, Port: port, Key: key, AEAD: mapString(raw, "aead_method"), Table: mapString(raw, "custom_table")}
	payload.ASCII = mapString(raw, "table_type")
	switch payload.ASCII {
	case "", "prefer_entropy":
		payload.ASCII = "entropy"
	case "prefer_ascii":
		payload.ASCII = "ascii"
	}
	if value, present := raw["enable_pure_downlink"]; present {
		pure, ok := value.(bool)
		if !ok {
			return nil, canonical.ErrUnsupportedProjection
		}
		payload.Packed = !pure
	}
	if value, present := raw["custom_tables"]; present {
		switch tables := value.(type) {
		case []string:
			payload.Tables = tables
		case []any:
			for _, value := range tables {
				table, ok := value.(string)
				if !ok {
					return nil, canonical.ErrUnsupportedProjection
				}
				payload.Tables = append(payload.Tables, table)
			}
		default:
			return nil, canonical.ErrUnsupportedProjection
		}
	}
	if value, present := raw["http_mask"]; present && value != nil {
		mask, ok := value.(map[string]any)
		if !ok {
			return nil, canonical.ErrUnsupportedProjection
		}
		enabled, _ := mask["enabled"].(bool)
		payload.DisableMask = !enabled
		payload.MaskMode = mapString(mask, "mode")
		payload.MaskPath = mapString(mask, "path_root")
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return []string{"sudoku://" + base64.RawURLEncoding.EncodeToString(encoded)}, nil
}

func classifySudokuSource(link string) []canonical.ProjectionDiagnostic {
	raw, err := decodeSudokuPayload(link)
	var fields map[string]any
	if err != nil || json.Unmarshal(raw, &fields) != nil {
		return []canonical.ProjectionDiagnostic{{State: canonical.ProjectionManual, Code: "source_fields_unmapped", Field: "endpoint"}}
	}
	known := map[string]bool{}
	for _, key := range strings.Fields("h p k a e x t ts hd hm hy") {
		known[key] = true
	}
	for key := range fields {
		if !known[key] {
			return []canonical.ProjectionDiagnostic{{State: canonical.ProjectionManual, Code: "source_fields_unmapped", Field: "endpoint"}}
		}
	}
	return nil
}
