//go:build linux && zapretproof

package nftables

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/MalenkiySolovey/solovey-ui/internal/ops/executableobject"
)

// tc is independent of the nft ruleset. The guard discards packets with the
// socket mark until the matching nft queue rule issues their pass bit.
// This adapter admits a fresh loopback interface, not a live shared qdisc.
type zapretGuard struct {
	binary *executableobject.Object
}

type tcObject struct {
	Kind    string
	Options struct {
		Actions []struct {
			Cookie        string
			Kind          string
			ControlAction struct{ Type string } `json:"control_action"`
		}
	}
}

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
	egress, err := g.run(ctx, "-j", "-d", "filter", "show", "dev", "lo", "egress")
	if err != nil {
		return "", "", err
	}
	objects, err = tcObjects(egress)
	if err != nil || len(objects) > 2 {
		return "", "", errors.New("unexpected egress filter inventory")
	}
	for _, filter := range objects {
		if filter.Kind != "fw" {
			return "", "", errors.New("unexpected egress filter")
		}
		if actions := filter.Options.Actions; len(actions) != 0 {
			if len(actions) != 1 || actions[0].Kind != "gact" || actions[0].ControlAction.Type != "drop" || cookie != "" {
				return "", "", errors.New("unexpected action on the guard filter")
			}
			cookie = actions[0].Cookie
		}
	}
	return sha256Hex(append(qdiscs, egress...)), cookie, nil
}

func (g zapretGuard) install(ctx context.Context, mark, mask uint32) (string, error) {
	var generation [16]byte
	if _, err := rand.Read(generation[:]); err != nil {
		return "", err
	}
	cookie := hex.EncodeToString(generation[:])
	if _, err := g.run(ctx, "qdisc", "add", "dev", "lo", "clsact"); err != nil {
		return "", err
	}
	if _, err := g.run(ctx, "filter", "add", "dev", "lo", "egress", "protocol", "all", "pref", "1",
		"handle", fmt.Sprintf("0x%x/0x%x", mark, mask), "fw", "action", "drop", "cookie", cookie); err != nil {
		return "", err
	}
	digest, current, err := g.snapshot(ctx)
	if err != nil || current != cookie || digest == "" {
		return "", errors.Join(errors.New("could not attest the created egress guard"), err)
	}
	return digest, nil
}
