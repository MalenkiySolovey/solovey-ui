package zapretactivation

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"strings"
)

// PackedList carries data for a selected profile, not a filename. Lines
// preserves the count needed to reserve space for decoded entries.
type PackedList struct {
	Bytes int    `json:"bytes"`
	Lines int    `json:"lines"`
	Gzip  []byte `json:"gzip"`
}

// PackSelectedLists changes only the broker projection. Neither the editor,
// stored definitions, export format nor strategy digests use this codec.
func PackSelectedLists(input Validation) (Validation, error) {
	input.Definitions = maps.Clone(input.Definitions)
	for id, raw := range input.Definitions {
		var doc map[string]json.RawMessage
		if err := json.Unmarshal(raw, &doc); err != nil || doc == nil {
			return Validation{}, errors.New("invalid selected Zapret definition")
		}
		var profiles []map[string]json.RawMessage
		if err := json.Unmarshal(doc["profiles"], &profiles); err != nil {
			return Validation{}, errors.New("invalid selected Zapret profiles")
		}
		changed := false
		for _, profile := range profiles {
			if profile["packedLists"] != nil {
				return Validation{}, errors.New("stored Zapret profiles cannot contain broker list input")
			}
			lists := profile["lists"]
			if lists == nil || bytes.Equal(lists, []byte("null")) {
				continue
			}
			var values map[string][]string
			if err := json.Unmarshal(lists, &values); err != nil {
				return Validation{}, errors.New("invalid selected Zapret lists")
			}
			packed := make(map[string]PackedList, len(values))
			for kind, entries := range values {
				if len(entries) == 0 {
					continue
				}
				value, err := packList(entries)
				if err != nil {
					return Validation{}, err
				}
				packed[kind] = value
			}
			payload, err := json.Marshal(packed)
			if err != nil {
				return Validation{}, err
			}
			delete(profile, "lists")
			profile["packedLists"] = payload
			changed = true
		}
		if !changed {
			continue
		}
		encoded, err := json.Marshal(profiles)
		if err != nil {
			return Validation{}, err
		}
		doc["profiles"] = encoded
		input.Definitions[id], err = json.Marshal(doc)
		if err != nil {
			return Validation{}, err
		}
	}
	return input, nil
}

func packList(entries []string) (PackedList, error) {
	var out bytes.Buffer
	writer, err := gzip.NewWriterLevel(&out, gzip.BestSpeed)
	if err != nil {
		return PackedList{}, err
	}
	defer writer.Close()
	value := PackedList{Lines: len(entries)}
	for _, entry := range entries {
		if entry == "" || strings.ContainsAny(entry, "\x00\r\n") {
			return PackedList{}, errors.New("invalid line in a selected Zapret list")
		}
		if _, err := io.WriteString(writer, entry); err != nil {
			return PackedList{}, err
		}
		if _, err := writer.Write([]byte{'\n'}); err != nil {
			return PackedList{}, err
		}
		value.Bytes += len(entry) + 1
	}
	if err := writer.Close(); err != nil {
		return PackedList{}, err
	}
	value.Gzip = out.Bytes()
	return value, nil
}

// Expand runs only after the authenticated broker has reserved workspace.
// Refuse a shorter/longer stream, bad CRC, concatenated stream, or false count.
func (p PackedList) Expand(reserve func(bytes, lines int) error) ([]string, error) {
	if p.Bytes <= 0 || uint64(p.Bytes) >= 1<<63-1 || p.Lines <= 0 || p.Lines > p.Bytes/2 || len(p.Gzip) == 0 || reserve == nil {
		return nil, errors.New("invalid selected Zapret list envelope")
	}
	if err := reserve(p.Bytes, p.Lines); err != nil {
		return nil, err
	}
	input := bytes.NewReader(p.Gzip)
	reader, err := gzip.NewReader(input)
	if err != nil {
		return nil, fmt.Errorf("invalid selected Zapret list stream: %w", err)
	}
	defer reader.Close()
	reader.Multistream(false)
	data, err := io.ReadAll(io.LimitReader(reader, int64(p.Bytes)+1))
	if err != nil || len(data) != p.Bytes || input.Len() != 0 || data[len(data)-1] != '\n' ||
		bytes.Count(data, []byte{'\n'}) != p.Lines || bytes.ContainsAny(data, "\x00\r") {
		return nil, errors.New("selected Zapret list stream does not match its declared content")
	}
	return strings.Split(string(data[:len(data)-1]), "\n"), nil
}
