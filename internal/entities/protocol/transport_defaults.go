package protocol

// XHTTPDefaultPadding is the interoperable default used by the native editor
// and by Xray sources that omit padding. Xray b69a376aa1b61ad94c9c05b2c2cd263567b8cc23,
// transport/internet/splithttp/config.go:GetNormalizedXPaddingBytes.
const XHTTPDefaultPadding = "100-1000"
