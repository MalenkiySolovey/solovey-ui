//go:build linux && zapretproof

package nftables

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/internal/ops/executableobject"
)

// tc is independent of the nft ruleset. The guard discards packets with the
// socket mark until the matching nft queue rule issues their pass bit.
// This adapter admits a fresh loopback interface, not a live shared qdisc.
type zapretGuard struct {
	binary     *executableobject.Object
	mark, mask uint32
}

type tcObject struct{ Kind string }

func (g zapretGuard) run(ctx context.Context, args ...string) ([]byte, error) {
	out, stderr, err := runNetworkTool(ctx, g.binary, "tc", nil, args...)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, stderr)
	}
	return out, nil
}

func tcObjects(raw []byte) ([]tcObject, error) {
	var objects []tcObject
	if err := json.Unmarshal(raw, &objects); err != nil || objects == nil {
		return nil, errors.New("invalid tc inventory")
	}
	return objects, nil
}

func (g zapretGuard) snapshot(ctx context.Context) (digest, cookie string, err error) {
	qdiscs, err := g.run(ctx, "-j", "qdisc", "show", "dev", "lo")
	if err != nil {
		return "", "", err
	}
	objects, err := tcObjects(qdiscs)
	if err != nil {
		return "", "", err
	}
	var root, clsact int
	for _, qdisc := range objects {
		switch qdisc.Kind {
		case "noqueue":
			root++
		case "clsact":
			clsact++
		}
	}
	if root != 1 || clsact > 1 || root+clsact != len(objects) {
		return "", "", errors.New("unexpected loopback qdisc inventory")
	}
	if clsact == 0 {
		return "", "", nil
	}
	ingress, err := g.run(ctx, "-j", "filter", "show", "dev", "lo", "ingress")
	if err != nil {
		return "", "", err
	}
	if objects, err := tcObjects(ingress); err != nil || len(objects) != 0 {
		return "", "", errors.New("refusing a nonempty ingress attachment")
	}
	// iproute2 6.1 emits bare handle text inside the fw filter's JSON.
	// Use the bounded, non-statistical text listing for this filter only.
	egress, err := g.run(ctx, "-d", "filter", "show", "dev", "lo", "egress")
	if err != nil {
		return "", "", err
	}
	cookie, err = g.observeFilter(string(egress))
	return sha256Hex(append(qdiscs, egress...)), cookie, err
}

func (g zapretGuard) observeFilter(output string) (string, error) {
	if strings.TrimSpace(output) == "" {
		return "", nil // an existing but empty clsact still has no ownership
	}
	const header = "filter protocol all pref 1 fw chain 0"
	expected := fmt.Sprintf("%s handle 0x%x/0x%x", header, g.mark, g.mask)
	var cookie string
	var heads, filters, actions int
	for line := range strings.SplitSeq(output, "\n") {
		line = strings.Join(strings.Fields(line), " ")
		switch {
		case line == header:
			heads++
		case line == expected:
			filters++
		case line == "action order 1: gact action drop":
			actions++
		case strings.HasPrefix(line, "cookie ") && cookie == "":
			cookie = strings.TrimPrefix(line, "cookie ")
		case line == "", line == "random type none pass val 0", strings.HasPrefix(line, "index "):
		default:
			return "", fmt.Errorf("unrecognized egress guard object: %q", line)
		}
	}
	if generation, err := hex.DecodeString(cookie); err != nil || len(generation) != 16 || heads > 1 || filters != 1 || actions != 1 {
		return "", fmt.Errorf("incomplete egress guard observation: %q", output)
	}
	return cookie, nil
}

func (g zapretGuard) install(ctx context.Context) (string, error) {
	var generation [16]byte
	if _, err := rand.Read(generation[:]); err != nil {
		return "", err
	}
	cookie := hex.EncodeToString(generation[:])
	if _, err := g.run(ctx, "qdisc", "add", "dev", "lo", "clsact"); err != nil {
		return "", err
	}
	if _, err := g.run(ctx, "filter", "add", "dev", "lo", "egress", "protocol", "all", "pref", "1",
		"handle", fmt.Sprintf("0x%x/0x%x", g.mark, g.mask), "fw", "action", "drop", "cookie", cookie); err != nil {
		return "", err
	}
	digest, current, err := g.snapshot(ctx)
	if err != nil || current != cookie || digest == "" {
		return "", errors.Join(errors.New("could not attest the created egress guard"), err)
	}
	return digest, nil
}
