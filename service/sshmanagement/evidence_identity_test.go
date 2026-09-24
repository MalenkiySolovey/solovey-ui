package sshmanagement

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	clientidentity "github.com/MalenkiySolovey/solovey-ui/internal/httpsecurity/clientidentity"
	domain "github.com/MalenkiySolovey/solovey-ui/internal/sshmanagement"
)

func TestPanelRecoveryBindsDirectAndTrustedProxyIdentity(t *testing.T) {
	for _, test := range []struct {
		name    string
		trusted bool
	}{{"direct", false}, {"trusted-xff", true}} {
		t.Run(test.name, func(t *testing.T) {
			var identity clientidentity.V1
			if test.trusted {
				t.Setenv("SUI_TRUSTED_PROXIES", "10.0.0.0/8")
				identity = recoveryTrustedProxyIdentity(t)
			} else {
				t.Setenv("SUI_TRUSTED_PROXIES", "")
				identity = recoveryDirectIdentity("198.51.100.7", "panel.example")
			}
			manager, _, now := workflowFixture(t)
			if err := manager.HandlePanelAuthenticationEvent("login_success", "administrator", domain.Revision("session-one"), identity); err != nil {
				t.Fatal(err)
			}
			paths, err := manager.RecoveryPaths(context.Background(), *now)
			if err != nil || len(paths) != 1 {
				t.Fatalf("paths=%#v err=%v disposition=%#v", paths, err, manager.RecoveryDisposition())
			}
			path := paths[0]
			if path.VerificationMethod != "fresh_panel_login" || path.ClientIdentityBindingRevision != clientidentity.BindingRevision(identity) ||
				path.ClientIdentityConfigRevision != identity.ConfigRevision || path.ClientIdentityProvenance != identity.Provenance {
				t.Fatalf("recovery lost client authority: %#v", path)
			}
		})
	}
}

func TestCredentialTransitionRequiresLaterRealAuthenticationToReissueRecovery(t *testing.T) {
	t.Setenv("SUI_TRUSTED_PROXIES", "")
	manager, _, now := workflowFixture(t)
	identity := recoveryDirectIdentity("198.51.100.7", "panel.example")
	if err := manager.HandlePanelAuthenticationEvent("login_success", "administrator", domain.Revision("session-before-change"), identity); err != nil {
		t.Fatal(err)
	}
	if paths, err := manager.RecoveryPaths(context.Background(), *now); err != nil || len(paths) != 1 {
		t.Fatalf("initial recovery paths=%#v err=%v", paths, err)
	}
	if err := manager.HandlePanelEvent("admin_credentials_changed", map[string]string{"user": "administrator"}); err != nil {
		t.Fatal(err)
	}
	if err := manager.HandlePanelEvent("session_reestablished_after_credential_change", map[string]string{"user": "administrator"}); err != nil {
		t.Fatal(err)
	}
	if paths, err := manager.RecoveryPaths(context.Background(), *now); err != nil || len(paths) != 0 {
		t.Fatalf("same-request session continuity reissued recovery: %#v err=%v", paths, err)
	}
	if disposition := manager.RecoveryDisposition(); disposition == nil || disposition.Code != domain.ReasonRecoveryReauthRequired {
		t.Fatalf("missing reauthentication disposition: %#v", disposition)
	}
	*now = now.Add(time.Minute)
	if err := manager.HandlePanelAuthenticationEvent("login_success", "administrator", domain.Revision("session-after-change"), identity); err != nil {
		t.Fatal(err)
	}
	if paths, err := manager.RecoveryPaths(context.Background(), *now); err != nil || len(paths) != 1 || paths[0].VerifiedAt != now.Unix() {
		t.Fatalf("later real authentication did not reissue recovery: %#v err=%v", paths, err)
	}
}

func TestPanelRecoveryRejectsUnknownAndInvalidIdentityWithBoundedDisposition(t *testing.T) {
	t.Setenv("SUI_TRUSTED_PROXIES", "")
	manager, _, now := workflowFixture(t)
	unknownRequest := httptest.NewRequest("GET", "http://panel.example/api", nil)
	unknownRequest.RemoteAddr = "10.0.0.2:443"
	unknown := clientidentity.Resolve(unknownRequest, clientidentity.ParseConfig("10.0.0.0/8"))
	if err := manager.HandlePanelAuthenticationEvent("login_success", "administrator", domain.Revision("session-unknown"), unknown); err != nil {
		t.Fatal(err)
	}
	if disposition := manager.RecoveryDisposition(); disposition == nil || disposition.Code != domain.ReasonRecoveryIdentityUnknown {
		t.Fatalf("unknown identity disposition=%#v", disposition)
	}
	invalid := recoveryDirectIdentity("127.0.0.1", "panel.example")
	if err := manager.HandlePanelAuthenticationEvent("login_success", "administrator", domain.Revision("session-loopback"), invalid); err != nil {
		t.Fatal(err)
	}
	if paths, err := manager.RecoveryPaths(context.Background(), *now); err != nil || len(paths) != 0 {
		t.Fatalf("rejected identity created recovery: %#v err=%v", paths, err)
	}
	if disposition := manager.RecoveryDisposition(); disposition == nil || disposition.Code != domain.ReasonRecoveryIdentityInvalid {
		t.Fatalf("invalid identity disposition=%#v", disposition)
	}
}

func TestPanelRecoveryBindingDriftRemovesPreviouslyValidEvidence(t *testing.T) {
	manager, _, now := workflowFixture(t)
	t.Setenv("SUI_TRUSTED_PROXIES", "")
	identity := recoveryDirectIdentity("198.51.100.7", "panel.example")
	if err := manager.HandlePanelAuthenticationEvent("login_success", "administrator", domain.Revision("session-drift"), identity); err != nil {
		t.Fatal(err)
	}
	if paths, err := manager.RecoveryPaths(context.Background(), *now); err != nil || len(paths) != 1 {
		t.Fatalf("initial paths=%#v err=%v", paths, err)
	}
	t.Setenv("SUI_TRUSTED_PROXIES", "10.0.0.0/8")
	if paths, err := manager.RecoveryPaths(context.Background(), *now); err != nil || len(paths) != 0 {
		t.Fatalf("stale binding remained authoritative: %#v err=%v", paths, err)
	}
	if disposition := manager.RecoveryDisposition(); disposition == nil || disposition.Code != domain.ReasonRecoveryBindingStale {
		t.Fatalf("binding drift disposition=%#v", disposition)
	}
}

func recoveryDirectIdentity(client, host string) clientidentity.V1 {
	request := httptest.NewRequest("GET", "https://"+host+"/api", nil)
	request.RemoteAddr = client + ":443"
	return clientidentity.Resolve(request, clientidentity.ConfigFromEnvironment())
}

func recoveryTrustedProxyIdentity(t *testing.T) clientidentity.V1 {
	t.Helper()
	config := clientidentity.ConfigFromEnvironment()
	request := httptest.NewRequest("GET", "https://panel.example/api", nil)
	request.RemoteAddr = "10.0.0.2:443"
	request.Header.Set("X-Forwarded-For", "198.51.100.8")
	request.Header.Set("X-Forwarded-Proto", "https")
	identity := clientidentity.Resolve(request, config)
	if identity.Provenance != clientidentity.ProvenanceTrustedXFF {
		t.Fatalf("trusted proxy fixture=%#v config=%#v", identity, config)
	}
	return identity
}
