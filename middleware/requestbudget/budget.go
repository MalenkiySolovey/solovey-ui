// Package requestbudget owns the mechanical per-route request and admission
// inventory for the admin API.
package requestbudget

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
)

const (
	MaxHeaderBytes        = 32 * 1024
	AuthTinyBytes         = 16 * 1024
	JSONBytes             = 1 * 1024 * 1024
	ConfigBytes           = 8 * 1024 * 1024
	ComponentBytes        = 64 * 1024 * 1024
	DatabaseBytes         = 512 * 1024 * 1024
	MultipartMemoryBytes  = 1 * 1024 * 1024
	MaxPageSize           = 200
	MaxJSONNestingDepth   = 64
	sessionAdmissionLanes = 64
)

type BodyClass string

const (
	BodyNone      BodyClass = "NONE"
	BodyAuthTiny  BodyClass = "AUTH_TINY"
	BodyJSON      BodyClass = "JSON_STANDARD"
	BodyConfig    BodyClass = "CONFIG_LARGE"
	BodyComponent BodyClass = "COMPONENT_PACKAGE"
	BodyDatabase  BodyClass = "DATABASE_TRANSFER"
)

type Policy struct {
	Method           string    `json:"method"`
	Route            string    `json:"route"`
	Authentication   string    `json:"authentication"`
	ActionScope      string    `json:"actionScope"`
	BodyClass        BodyClass `json:"bodyClass"`
	MaxBodyBytes     int64     `json:"maxBodyBytes"`
	ConcurrencyClass string    `json:"concurrencyClass"`
	PressureClass    string    `json:"pressureClass"`
	AuditPolicy      string    `json:"auditPolicy"`
	ResponseClass    string    `json:"responseClass"`
	StepUpOperation  string    `json:"stepUpOperation,omitempty"`
}

type Registry struct {
	basePath string
	mu       sync.RWMutex
	policies map[string]Policy
	pressure PressureGate
}

// RejectionHook receives bounded reason classes after the response has been
// rejected. It must not inspect or persist the request body.
type RejectionHook func(c *gin.Context, policy Policy, reason string)

type PressureDecision struct {
	Allowed    bool
	Reason     string
	RetryAfter int
}

type PressureGate func(Policy) PressureDecision

func NewRegistry(basePath string) *Registry {
	return &Registry{
		basePath: strings.TrimRight(basePath, "/"),
		policies: map[string]Policy{},
	}
}

func (r *Registry) SetPressureGate(gate PressureGate) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pressure = gate
}

func (r *Registry) pressureDecision(policy Policy) PressureDecision {
	r.mu.RLock()
	gate := r.pressure
	r.mu.RUnlock()
	if gate == nil {
		return PressureDecision{Allowed: true}
	}
	return gate(policy)
}

// DeclareGinRoutes creates an entry for every registered admin API route.
// Classification is deterministic, so inventory coverage cannot silently lag
// behind route additions.
func (r *Registry) DeclareGinRoutes(routes gin.RoutesInfo) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, route := range routes {
		if !r.protected(route.Path) {
			continue
		}
		policy := classify(route.Method, route.Path)
		r.policies[policyKey(route.Method, route.Path)] = policy
	}
}

func (r *Registry) Policies() []Policy {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]Policy, 0, len(r.policies))
	for _, policy := range r.policies {
		result = append(result, policy)
	}
	return result
}

func (r *Registry) Lookup(method, route string) (Policy, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	policy, ok := r.policies[policyKey(method, route)]
	return policy, ok
}

func (r *Registry) protected(path string) bool {
	apiBase := r.basePath + "/api"
	apiV2Base := r.basePath + "/apiv2"
	if r.basePath == "" {
		apiBase = "/api"
		apiV2Base = "/apiv2"
	}
	return path == apiBase || strings.HasPrefix(path, apiBase+"/") ||
		path == apiV2Base || strings.HasPrefix(path, apiV2Base+"/")
}

type admission struct {
	global map[string]chan struct{}
	lanes  [sessionAdmissionLanes]chan struct{}
}

func newAdmission() *admission {
	a := &admission{global: map[string]chan struct{}{
		"config":               make(chan struct{}, 1),
		"backup":               make(chan struct{}, 1),
		"component":            make(chan struct{}, 2),
		"diagnostics":          make(chan struct{}, 2),
		"ssh_candidate":        make(chan struct{}, 1),
		"deployment_migration": make(chan struct{}, 1),
		"update":               make(chan struct{}, 1),
		"data_lifecycle":       make(chan struct{}, 1),
	}}
	for i := range a.lanes {
		a.lanes[i] = make(chan struct{}, 1)
	}
	return a
}

func Middleware(registry *Registry, hooks ...RejectionHook) gin.HandlerFunc {
	admission := newAdmission()
	var rejectionHook RejectionHook
	if len(hooks) > 0 {
		rejectionHook = hooks[0]
	}
	recordRejection := func(c *gin.Context, policy Policy, reason string) {
		if rejectionHook != nil {
			rejectionHook(c, policy, reason)
		}
	}
	return func(c *gin.Context) {
		route := c.FullPath()
		if route == "" || !registry.protected(route) {
			c.Next()
			return
		}
		policy, ok := registry.Lookup(c.Request.Method, route)
		if !ok {
			policy = Policy{Method: c.Request.Method, Route: route, AuditPolicy: "security"}
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
				"success": false,
				"msg":     "Request budget is not declared",
				"obj":     nil,
			})
			recordRejection(c, policy, "undeclared_route")
			return
		}
		pressure := registry.pressureDecision(policy)
		if !pressure.Allowed {
			retryAfter := pressure.RetryAfter
			if retryAfter < 1 || retryAfter > 300 {
				retryAfter = 10
			}
			c.Header("Retry-After", strconv.Itoa(retryAfter))
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
				"success": false,
				"msg":     "Request rejected by resource pressure admission",
				"obj": gin.H{"state": "PRESSURE_REJECTED", "reasonCode": pressure.Reason,
					"retryAfterSeconds": retryAfter},
			})
			recordRejection(c, policy, "pressure_rejected:"+pressure.Reason)
			return
		}
		if !enforcePageSize(c) {
			recordRejection(c, policy, "invalid_page_size")
			return
		}
		if !enforceBody(c, policy) {
			recordRejection(c, policy, "body_limit")
			return
		}
		if !enforceJSONDepth(c) {
			recordRejection(c, policy, "json_depth")
			return
		}
		release, allowed := admission.acquire(c.Request, policy)
		if !allowed {
			c.Header("Retry-After", "1")
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"success": false,
				"msg":     "Request concurrency limit reached",
				"obj":     nil,
			})
			recordRejection(c, policy, "concurrency_limit")
			return
		}
		defer release()
		c.Next()
	}
}

func classify(method, route string) Policy {
	policy := Policy{
		Method:         method,
		Route:          route,
		Authentication: "browser_session",
		ActionScope:    "read",
		BodyClass:      BodyNone,
		PressureClass:  "interactive",
		AuditPolicy:    "standard",
		ResponseClass:  "bounded_safe_envelope",
	}
	isMutation := method == http.MethodPost || method == http.MethodPut ||
		method == http.MethodPatch || method == http.MethodDelete
	if isMutation {
		policy.ActionScope = "write"
		policy.BodyClass = BodyJSON
		policy.MaxBodyBytes = JSONBytes
		policy.ConcurrencyClass = "session_mutation"
		if method == http.MethodDelete {
			policy.BodyClass = BodyNone
			policy.MaxBodyBytes = 0
		}
	}
	lower := strings.ToLower(route)
	databaseImportRoute := strings.Contains(lower, "/import-")
	restoreRoute := strings.Contains(lower, "/v1/operations/data/restore")
	if strings.Contains(lower, "/apiv2/") {
		policy.Authentication = "browser_or_bearer"
	}
	if strings.HasSuffix(lower, "/login") || strings.HasSuffix(lower, "/csrf") {
		policy.Authentication = "public_preauth"
		policy.ActionScope = "authenticate"
		policy.PressureClass = "essential"
	}

	switch {
	case strings.HasSuffix(lower, "/changepass"), strings.HasSuffix(lower, "/v1/security/password/change"):
		policy.ActionScope = "credential_change"
		policy.StepUpOperation = "admin.credential"
		policy.AuditPolicy = "security"
	case strings.HasSuffix(lower, "/addadmin"):
		policy.ActionScope = "admin_create"
		policy.StepUpOperation = "admin.create"
		policy.AuditPolicy = "security"
	case strings.HasSuffix(lower, "/deleteadmin"):
		policy.ActionScope = "admin_delete"
		policy.StepUpOperation = "admin.delete"
		policy.AuditPolicy = "security"
	case strings.HasSuffix(lower, "/addtoken"):
		policy.ActionScope = "token_create"
		policy.StepUpOperation = "token.create"
		policy.AuditPolicy = "security"
	case strings.HasSuffix(lower, "/deletetoken"):
		policy.ActionScope = "token_revoke"
		policy.StepUpOperation = "token.revoke"
		policy.AuditPolicy = "security"
	case strings.HasSuffix(lower, "/settokenenabled"):
		policy.ActionScope = "token_change"
		policy.StepUpOperation = "token.change"
		policy.AuditPolicy = "security"
	case strings.HasSuffix(lower, "/importdb"):
		policy.ActionScope = "backup_restore"
		policy.StepUpOperation = "backup.restore"
		policy.AuditPolicy = "security"
	case restoreRoute && !strings.HasSuffix(lower, "/rehearsal"):
		policy.ActionScope = "backup_restore"
		policy.StepUpOperation = "backup.restore"
		policy.AuditPolicy = "security"
	case restoreRoute:
		policy.ActionScope = "backup_restore_rehearsal"
		policy.AuditPolicy = "security"
	case databaseImportRoute && strings.HasSuffix(lower, "/plan"):
		policy.ActionScope = "database_import"
		policy.AuditPolicy = "security"
	case databaseImportRoute:
		policy.ActionScope = "database_import"
		policy.StepUpOperation = "backup.restore"
		policy.AuditPolicy = "security"
	case strings.Contains(lower, "/update/components/") && strings.HasSuffix(lower, "/remove"):
		policy.ActionScope = "component_remove"
		policy.StepUpOperation = "drop_data"
		policy.AuditPolicy = "security"
	case strings.HasSuffix(lower, "/v1/security/sessions/adopt-bounded"):
		policy.ActionScope = "session_policy_adopt"
		policy.StepUpOperation = "sessions.adopt_bounded"
		policy.AuditPolicy = "security"
	case strings.HasSuffix(lower, "/v1/security/sessions/revoke-others"):
		policy.ActionScope = "session_revoke_others"
		policy.StepUpOperation = "sessions.revoke_others"
		policy.AuditPolicy = "security"
	case strings.HasSuffix(lower, "/v1/security/mfa/enroll"):
		policy.ActionScope = "mfa_enroll"
		policy.StepUpOperation = "mfa.enroll"
		policy.AuditPolicy = "security"
	case strings.HasSuffix(lower, "/v1/security/mfa/recovery/rotate"):
		policy.ActionScope = "mfa_recovery_rotate"
		policy.StepUpOperation = "mfa.recovery.rotate"
		policy.AuditPolicy = "security"
	case strings.HasSuffix(lower, "/v1/security/mfa/disable"):
		policy.ActionScope = "mfa_disable"
		policy.StepUpOperation = "mfa.disable"
		policy.AuditPolicy = "security"
	case strings.HasSuffix(lower, "/v1/operations/ssh/candidate"):
		policy.ActionScope = "ssh_candidate_apply"
		policy.StepUpOperation = "ssh.candidate.apply"
		policy.AuditPolicy = "security"
	case strings.HasSuffix(lower, "/reconnect/confirm") && strings.Contains(lower, "/v1/operations/ssh/candidate/"):
		policy.ActionScope = "ssh_candidate_confirm"
		policy.StepUpOperation = "ssh.candidate.confirm"
		policy.AuditPolicy = "security"
	case strings.HasSuffix(lower, "/rollback") && strings.Contains(lower, "/v1/operations/ssh/candidate/"):
		policy.ActionScope = "ssh_candidate_rollback"
		policy.StepUpOperation = "ssh.candidate.rollback"
		policy.AuditPolicy = "security"
	case strings.HasSuffix(lower, "/v1/operations/deployment/migration"):
		policy.ActionScope = "deployment_migrate"
		policy.StepUpOperation = "deployment.migrate"
		policy.AuditPolicy = "security"
	case strings.HasSuffix(lower, "/confirm") && strings.Contains(lower, "/v1/operations/deployment/migration/"):
		policy.ActionScope = "deployment_confirm"
		policy.StepUpOperation = "deployment.confirm"
		policy.AuditPolicy = "security"
	case strings.HasSuffix(lower, "/rollback") && strings.Contains(lower, "/v1/operations/deployment/migration/"):
		policy.ActionScope = "deployment_rollback"
		policy.StepUpOperation = "deployment.rollback"
		policy.AuditPolicy = "security"
		policy.PressureClass = "recovery_essential"
	case (strings.HasSuffix(lower, "/prepare") || strings.HasSuffix(lower, "/preflight")) && strings.Contains(lower, "/v1/operations/update/"):
		policy.ActionScope = "update_prepare"
		policy.StepUpOperation = "update.prepare"
		policy.AuditPolicy = "security"
	case strings.HasSuffix(lower, "/activate") && strings.Contains(lower, "/v1/operations/update/"):
		policy.ActionScope = "update_activate"
		policy.StepUpOperation = "update.activate"
		policy.AuditPolicy = "security"
	case strings.HasSuffix(lower, "/rollback") && strings.Contains(lower, "/v1/operations/update/"):
		policy.ActionScope = "update_rollback"
		policy.StepUpOperation = "update.rollback"
		policy.AuditPolicy = "security"
		policy.PressureClass = "recovery_essential"
	case strings.HasSuffix(lower, "/drop") && strings.Contains(lower, "/v1/operations/data/"):
		policy.ActionScope = "data_drop"
		policy.StepUpOperation = "data.drop"
		policy.AuditPolicy = "security"
	case strings.Contains(lower, "/v1/operations/update/"):
		policy.ActionScope = "update_lifecycle"
		policy.AuditPolicy = "security"
	case strings.Contains(lower, "/v1/operations/data/"):
		policy.ActionScope = "data_lifecycle"
		policy.AuditPolicy = "security"
	case strings.Contains(lower, "/v1/operations/deployment/"):
		policy.ActionScope = "deployment_management"
		policy.AuditPolicy = "security"
	case strings.Contains(lower, "/v1/operations/ssh/"):
		policy.ActionScope = "ssh_management"
		policy.AuditPolicy = "security"
	case strings.Contains(lower, "/v1/security/"):
		policy.ActionScope = "security"
		policy.AuditPolicy = "security"
	}

	// Admission class is independent from body class: downloads and reports
	// remain bodyless while still sharing the bounded expensive-work lane.
	switch {
	case method == http.MethodGet && strings.Contains(lower, "/v1/operations/update/"),
		method == http.MethodGet && strings.Contains(lower, "/v1/operations/data/"),
		strings.Contains(lower, "/v1/operations/pressure"), strings.Contains(lower, "/v1/operations/resource-pressure"),
		strings.Contains(lower, "/v1/operations/sqlite"), strings.HasSuffix(lower, "/v1/operations/status"):
		policy.PressureClass = "essential"
	case strings.HasSuffix(lower, "/v1/operations/update/check"):
		policy.PressureClass = "interactive"
	case strings.HasSuffix(lower, "/rollback") && strings.Contains(lower, "/v1/operations/"):
		policy.PressureClass = "recovery_essential"
		switch {
		case strings.Contains(lower, "/v1/operations/ssh/"):
			policy.ConcurrencyClass = "ssh_candidate"
		case strings.Contains(lower, "/v1/operations/deployment/"):
			policy.ConcurrencyClass = "deployment_migration"
		case strings.Contains(lower, "/v1/operations/update/"):
			policy.ConcurrencyClass = "update"
		default:
			policy.ConcurrencyClass = "data_lifecycle"
		}
	case strings.Contains(lower, "/v1/operations/update/"):
		policy.ConcurrencyClass = "update"
		policy.PressureClass = "heavy_mutation"
	case strings.Contains(lower, "/v1/operations/data/"):
		policy.ConcurrencyClass = "data_lifecycle"
		policy.PressureClass = "heavy_mutation"
	case strings.Contains(lower, "/importdb"), restoreRoute, databaseImportRoute,
		strings.Contains(lower, "/backup"), strings.Contains(lower, "/getdb"):
		policy.ConcurrencyClass = "backup"
		policy.PressureClass = "expensive"
	case strings.Contains(lower, "/diagnostic"), strings.Contains(lower, "/doctor"):
		policy.ConcurrencyClass = "diagnostics"
		policy.PressureClass = "optional"
	case strings.Contains(lower, "/components/"):
		policy.ConcurrencyClass = "component"
		policy.PressureClass = "bounded_component"
	case isMutation && strings.Contains(lower, "/v1/operations/ssh/"):
		policy.ConcurrencyClass = "ssh_candidate"
		policy.PressureClass = "security_critical"
	case isMutation && strings.Contains(lower, "/v1/operations/deployment/"):
		policy.ConcurrencyClass = "deployment_migration"
		policy.PressureClass = "security_critical"
	case strings.HasSuffix(lower, "/save"),
		strings.HasSuffix(lower, "/reorder"),
		strings.Contains(lower, "/inbounddrafts/"):
		policy.ConcurrencyClass = "config"
		policy.PressureClass = "configuration"
	}
	if !isMutation {
		return policy
	}

	switch {
	case strings.HasSuffix(lower, "/login"),
		strings.HasSuffix(lower, "/changepass"),
		strings.Contains(lower, "/v1/security/"),
		strings.Contains(lower, "/v1/operations/ssh/"),
		strings.Contains(lower, "/v1/operations/deployment/"),
		strings.HasSuffix(lower, "/addadmin"),
		strings.HasSuffix(lower, "/deleteadmin"),
		strings.HasSuffix(lower, "/addtoken"),
		strings.HasSuffix(lower, "/deletetoken"),
		strings.HasSuffix(lower, "/settokenenabled"):
		policy.BodyClass = BodyAuthTiny
		policy.MaxBodyBytes = AuthTinyBytes
	case strings.HasSuffix(lower, "/update/apply"),
		strings.Contains(lower, "/update/components/") && strings.HasSuffix(lower, "/remove"):
		policy.BodyClass = BodyAuthTiny
		policy.MaxBodyBytes = AuthTinyBytes
	case strings.Contains(lower, "/importdb"), restoreRoute,
		databaseImportRoute && !strings.HasSuffix(lower, "/rollback"):
		policy.BodyClass = BodyDatabase
		policy.MaxBodyBytes = DatabaseBytes
	case databaseImportRoute && strings.HasSuffix(lower, "/rollback"):
		policy.BodyClass = BodyAuthTiny
		policy.MaxBodyBytes = AuthTinyBytes
	case strings.Contains(lower, "/components/") &&
		(strings.HasSuffix(lower, "/assets") || strings.HasSuffix(lower, "/import")):
		policy.BodyClass = BodyConfig
		policy.MaxBodyBytes = ConfigBytes
	case strings.HasSuffix(lower, "/save"),
		strings.HasSuffix(lower, "/reorder"),
		strings.Contains(lower, "/inbounddrafts/"):
		policy.BodyClass = BodyConfig
		policy.MaxBodyBytes = ConfigBytes
	}
	return policy
}

func enforceBody(c *gin.Context, policy Policy) bool {
	if policy.BodyClass == BodyNone {
		if c.Request.ContentLength > 0 || len(c.Request.TransferEncoding) > 0 {
			c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{
				"success": false,
				"msg":     "Request body is not allowed",
				"obj":     nil,
			})
			return false
		}
		return true
	}
	if policy.MaxBodyBytes <= 0 {
		return true
	}
	if c.Request.ContentLength > policy.MaxBodyBytes {
		c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{
			"success": false,
			"msg":     "Request body is too large",
			"obj":     nil,
		})
		return false
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, policy.MaxBodyBytes)
	return true
}

func enforceJSONDepth(c *gin.Context) bool {
	if c.Request.Body == nil {
		return true
	}
	contentType, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || (contentType != "application/json" && !strings.HasSuffix(contentType, "+json")) {
		return true
	}
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{
				"success": false,
				"msg":     "Request body is too large",
				"obj":     nil,
			})
		} else {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
				"success": false,
				"msg":     "Unable to read request body",
				"obj":     nil,
			})
		}
		return false
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	if len(bytes.TrimSpace(body)) == 0 {
		return true
	}

	decoder := json.NewDecoder(bytes.NewReader(body))
	depth := 0
	for {
		token, tokenErr := decoder.Token()
		if errors.Is(tokenErr, io.EOF) {
			return true
		}
		if tokenErr != nil {
			// Route-specific decoders retain ownership of syntax errors and
			// response shapes; this layer only enforces the depth ceiling.
			return true
		}
		delimiter, ok := token.(json.Delim)
		if !ok {
			continue
		}
		switch delimiter {
		case '{', '[':
			depth++
			if depth > MaxJSONNestingDepth {
				c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
					"success": false,
					"msg":     "JSON nesting is too deep",
					"obj":     nil,
				})
				return false
			}
		case '}', ']':
			depth--
		}
	}
}

func enforcePageSize(c *gin.Context) bool {
	for _, key := range []string{"limit", "pageSize", "perPage"} {
		raw := strings.TrimSpace(c.Query(key))
		if raw == "" {
			continue
		}
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > MaxPageSize {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
				"success": false,
				"msg":     "Invalid page size",
				"obj":     nil,
			})
			return false
		}
	}
	return true
}

func (a *admission) acquire(r *http.Request, policy Policy) (func(), bool) {
	releases := make([]func(), 0, 2)
	releaseAll := func() {
		for i := len(releases) - 1; i >= 0; i-- {
			releases[i]()
		}
	}
	acquireOne := func(semaphore chan struct{}) bool {
		if semaphore == nil {
			return true
		}
		select {
		case semaphore <- struct{}{}:
			releases = append(releases, func() { <-semaphore })
			return true
		default:
			return false
		}
	}

	// An expensive/global lane supplements the per-session mutation lane; it
	// never replaces it. Bearer tokens use a transient hash of Authorization so
	// distinct API credentials do not collapse onto one peer-address lane.
	if isMutationMethod(r.Method) && policy.Authentication != "public_preauth" {
		key := ""
		if cookie, err := r.Cookie("s-ui"); err == nil {
			key = "session:" + cookie.Value
		}
		if key == "" {
			if authorization := strings.TrimSpace(r.Header.Get("Authorization")); authorization != "" {
				key = "authorization:" + authorization
			}
		}
		if key == "" {
			key = "peer:" + r.RemoteAddr
		}
		sum := sha256.Sum256([]byte(key))
		index := binary.BigEndian.Uint64(sum[:8]) % sessionAdmissionLanes
		if !acquireOne(a.lanes[index]) {
			return func() {}, false
		}
	}

	if policy.ConcurrencyClass != "" && policy.ConcurrencyClass != "session_mutation" {
		if !acquireOne(a.global[policy.ConcurrencyClass]) {
			releaseAll()
			return func() {}, false
		}
	}
	return releaseAll, true
}

func isMutationMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func policyKey(method, route string) string {
	return strings.ToUpper(method) + " " + route
}
