package ipcert

import (
	"context"
	"crypto"
	"crypto/x509"
	"encoding/json"
	"os"
	"strconv"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/util/common"

	"github.com/go-acme/lego/v4/certcrypto"
	"github.com/go-acme/lego/v4/certificate"
	"github.com/go-acme/lego/v4/challenge/http01"
	"github.com/go-acme/lego/v4/lego"
	"github.com/go-acme/lego/v4/registration"
)

// LEShortlivedProfile is Let's Encrypt's certificate profile that authorises
// IP-address identifiers (RFC 8738). It yields ~160h (~6.7-day) certificates.
const LEShortlivedProfile = "shortlived"

// ACMERequest is the input to an ACME issuance. AccountKeyPEM/RegistrationJSON
// carry a previously-persisted account so renewals reuse it instead of
// registering a fresh account every time.
type ACMERequest struct {
	IP               string
	Email            string
	ChallengePort    int
	AccountKeyPEM    string // empty => a new account key is generated
	RegistrationJSON string // empty => the account is (re-)registered
}

// ACMEResult is the output of a successful issuance. CertPEM is the bundled
// leaf+issuer chain. AccountKeyPEM/RegistrationJSON are echoed back so the
// caller can persist the (possibly newly created) account.
type ACMEResult struct {
	CertPEM          []byte
	KeyPEM           []byte
	AccountKeyPEM    string
	RegistrationJSON string
}

// ACMEIssuer keeps all network/Let's Encrypt code behind a small seam. Tests
// inject a fake; production uses LegoIssuer.
type ACMEIssuer interface {
	Obtain(ctx context.Context, req ACMERequest) (ACMEResult, error)
}

// ACMEDirectoryURL returns the ACME directory endpoint. It defaults to Let's
// Encrypt production; SUI_ACME_DIR_URL overrides it for staging/Pebble smoke
// tests only.
func ACMEDirectoryURL() string {
	if v := strings.TrimSpace(os.Getenv("SUI_ACME_DIR_URL")); v != "" {
		return v
	}
	return lego.LEDirectoryProduction
}

type user struct {
	email        string
	key          crypto.PrivateKey
	registration *registration.Resource
}

func (u *user) GetEmail() string                        { return u.email }
func (u *user) GetRegistration() *registration.Resource { return u.registration }
func (u *user) GetPrivateKey() crypto.PrivateKey        { return u.key }

// LegoIssuer is the real, network-backed ACME issuer.
type LegoIssuer struct{}

func (LegoIssuer) Obtain(ctx context.Context, req ACMERequest) (ACMEResult, error) {
	// lego's Certificate.Obtain has no context parameter, so an in-flight ACME
	// exchange cannot be cancelled; honour an already-cancelled context by
	// refusing to start the issuance.
	if err := ctx.Err(); err != nil {
		return ACMEResult{}, err
	}
	if err := ValidateIssuableIP(req.IP); err != nil {
		return ACMEResult{}, err
	}

	user, accountKeyPEM, err := buildUser(req)
	if err != nil {
		return ACMEResult{}, err
	}

	config := lego.NewConfig(user)
	config.CADirURL = ACMEDirectoryURL()
	config.Certificate.KeyType = certcrypto.EC256

	client, err := lego.NewClient(config)
	if err != nil {
		return ACMEResult{}, err
	}

	port := req.ChallengePort
	if port <= 0 {
		port = 80
	}
	provider := http01.NewProviderServer("", strconv.Itoa(port))
	if err = client.Challenge.SetHTTP01Provider(provider); err != nil {
		return ACMEResult{}, err
	}

	registrationJSON := req.RegistrationJSON
	if user.GetRegistration() == nil {
		reg, regErr := client.Registration.Register(registration.RegisterOptions{TermsOfServiceAgreed: true})
		if regErr != nil {
			return ACMEResult{}, regErr
		}
		user.registration = reg
		marshalled, marshalErr := json.Marshal(reg)
		if marshalErr != nil {
			return ACMEResult{}, marshalErr
		}
		registrationJSON = string(marshalled)
	}

	// Build the CSR ourselves so the IP lands in the subjectAltName with an
	// EMPTY Common Name. lego's Certificate.Obtain copies the single "domain"
	// into the CSR Subject.CommonName, which Let's Encrypt rejects for IP
	// identifiers with "badCSR :: CSR contains IP address in Common Name"
	// (RFC 8738 requires the IP only in the SAN). ObtainForCSR instead infers
	// the IP identifier from the CSR's IPAddresses SAN.
	leafKey, csr, err := BuildCSR(req.IP)
	if err != nil {
		return ACMEResult{}, err
	}

	resource, err := client.Certificate.ObtainForCSR(certificate.ObtainForCSRRequest{
		CSR:        csr,
		PrivateKey: leafKey,
		Profile:    LEShortlivedProfile,
		Bundle:     true,
	})
	if err != nil {
		return ACMEResult{}, err
	}
	if resource == nil || len(resource.Certificate) == 0 || len(resource.PrivateKey) == 0 {
		return ACMEResult{}, common.NewError("ip cert: ACME returned an empty certificate")
	}

	return ACMEResult{
		CertPEM:          resource.Certificate,
		KeyPEM:           resource.PrivateKey,
		AccountKeyPEM:    accountKeyPEM,
		RegistrationJSON: registrationJSON,
	}, nil
}

func buildUser(req ACMERequest) (*user, string, error) {
	var (
		key           crypto.PrivateKey
		accountKeyPEM string
		err           error
	)
	if strings.TrimSpace(req.AccountKeyPEM) != "" {
		key, err = certcrypto.ParsePEMPrivateKey([]byte(req.AccountKeyPEM))
		if err != nil {
			return nil, "", common.NewError("ip cert: stored ACME account key is invalid: ", err.Error())
		}
		accountKeyPEM = req.AccountKeyPEM
	} else {
		key, err = certcrypto.GeneratePrivateKey(certcrypto.EC256)
		if err != nil {
			return nil, "", err
		}
		accountKeyPEM = string(certcrypto.PEMEncode(key))
	}

	user := &user{email: req.Email, key: key}
	if strings.TrimSpace(req.RegistrationJSON) != "" {
		var reg registration.Resource
		if err = json.Unmarshal([]byte(req.RegistrationJSON), &reg); err != nil {
			return nil, "", common.NewError("ip cert: stored ACME registration is invalid: ", err.Error())
		}
		user.registration = &reg
	}
	return user, accountKeyPEM, nil
}

// BuildCSR creates an EC256 leaf key and a CSR carrying the target IP as the
// sole subjectAltName with an EMPTY Subject Common Name. The empty CN keeps
// Let's Encrypt from rejecting the order with "CSR contains IP address in Common
// Name": for IP identifiers (RFC 8738) the address must appear only in the SAN.
func BuildCSR(ip string) (crypto.PrivateKey, *x509.CertificateRequest, error) {
	leafKey, err := certcrypto.GeneratePrivateKey(certcrypto.EC256)
	if err != nil {
		return nil, nil, err
	}
	csrDER, err := certcrypto.CreateCSR(leafKey, certcrypto.CSROptions{
		Domain: "", // empty Common Name: IP must not be the CN
		SAN:    []string{strings.TrimSpace(ip)},
	})
	if err != nil {
		return nil, nil, err
	}
	csr, err := x509.ParseCertificateRequest(csrDER)
	if err != nil {
		return nil, nil, err
	}
	return leafKey, csr, nil
}
