//go:build !minimal

package service

import _ "embed"

const decoyInteractivityAssetPath = "assets/decoy-interactivity.js"

//go:embed decoy-interactivity.js
var decoyInteractivityScript []byte
