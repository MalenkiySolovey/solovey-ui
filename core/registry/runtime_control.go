package registry

// PrivateRuntimeAPICompiled describes the attached control seam included in
// every current profile. It does not authorize a raw user-configured api service.
func PrivateRuntimeAPICompiled() bool { return true }
