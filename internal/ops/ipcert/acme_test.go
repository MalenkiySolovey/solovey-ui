package ipcert

import (
	"net"
	"testing"
)

// TestBuildCSRHasEmptyCommonNameAndIPSAN locks in the badCSR fix: the CSR sent
// to Let's Encrypt for an IP identifier must carry the IP only in the
// subjectAltName, with an EMPTY Common Name.
func TestBuildCSRHasEmptyCommonNameAndIPSAN(t *testing.T) {
	const ip = "93.184.216.34"
	key, csr, err := BuildCSR(ip)
	if err != nil {
		t.Fatal(err)
	}
	if key == nil {
		t.Fatal("BuildCSR returned a nil leaf key")
	}
	if csr.Subject.CommonName != "" {
		t.Fatalf("CommonName = %q, want empty (IP in CN is rejected by Let's Encrypt)", csr.Subject.CommonName)
	}
	if len(csr.IPAddresses) != 1 || !csr.IPAddresses[0].Equal(net.ParseIP(ip)) {
		t.Fatalf("IPAddresses = %v, want [%s]", csr.IPAddresses, ip)
	}
	if len(csr.DNSNames) != 0 {
		t.Fatalf("DNSNames = %v, want none for an IP certificate", csr.DNSNames)
	}
	if err := csr.CheckSignature(); err != nil {
		t.Fatalf("CSR signature invalid: %v", err)
	}
}

func TestBuildCSRTrimsWhitespace(t *testing.T) {
	_, csr, err := BuildCSR("  93.184.216.34 ")
	if err != nil {
		t.Fatal(err)
	}
	if len(csr.IPAddresses) != 1 || !csr.IPAddresses[0].Equal(net.ParseIP("93.184.216.34")) {
		t.Fatalf("IPAddresses = %v, want trimmed [93.184.216.34]", csr.IPAddresses)
	}
}
