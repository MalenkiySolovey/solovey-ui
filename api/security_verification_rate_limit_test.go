package api

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestSecurityVerificationLimitsArePerMethodAndAggregate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	securityContext := SessionSecurityContext{UserID: 1, Ref: "rate-limit-session"}
	h := &securityHTTP{}
	attempt := func(method string) bool {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest("POST", "/api/v1/security/step-up", nil)
		c.Request.RemoteAddr = "198.51.100.20:12345"
		return h.allowSecurityVerification(c, securityContext, method)
	}

	resetRateLimitState()
	for i := 0; i < securityVerificationMethodLimit; i++ {
		if !attempt("password") {
			t.Fatalf("password attempt %d was rejected before its method limit", i+1)
		}
	}
	if attempt("password") {
		t.Fatal("password method limit did not reject the next attempt")
	}

	resetRateLimitState()
	for i := 0; i < securityVerificationMethodLimit; i++ {
		if !attempt("password") {
			t.Fatalf("password attempt %d was rejected", i+1)
		}
	}
	for i := 0; i < securityVerificationMethodLimit; i++ {
		if !attempt("totp") {
			t.Fatalf("TOTP attempt %d did not receive its independent budget", i+1)
		}
	}
	if attempt("recovery") {
		t.Fatal("aggregate verification limit did not reject the next factor attempt")
	}
}
