// Command gen-certs creates a local CA and two leaf certificates: one for the
// Go HTTPS server (api) and one for PostgreSQL (postgres). This is intended for
// local development; use real certificates in production.
package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

func main() {
	dir := "certs"
	if len(os.Args) > 1 && os.Args[1] != "" {
		dir = os.Args[1]
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fatal(err)
	}

	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		fatal(err)
	}
	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Soglia Local CA", Organization: []string{"Soglia Local"}},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		fatal(err)
	}
	caCert, err := x509.ParseCertificate(caDER)
	if err != nil {
		fatal(err)
	}
	writePEM(filepath.Join(dir, "ca.crt"), "CERTIFICATE", caDER, 0o644)
	writeKey(filepath.Join(dir, "ca.key"), caKey)

	issue := func(name string, extraDNS ...string) {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			fatal(err)
		}
		template := &x509.Certificate{
			SerialNumber: big.NewInt(time.Now().UnixNano()),
			Subject:      pkix.Name{CommonName: name, Organization: []string{"Soglia Local"}},
			NotBefore:    time.Now().Add(-time.Hour),
			NotAfter:     time.Now().AddDate(10, 0, 0),
			KeyUsage:     x509.KeyUsageDigitalSignature,
			ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
			DNSNames:     append([]string{"localhost"}, extraDNS...),
			IPAddresses:  []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
		}
		der, err := x509.CreateCertificate(rand.Reader, template, caCert, &key.PublicKey, caKey)
		if err != nil {
			fatal(err)
		}
		writePEM(filepath.Join(dir, name+".crt"), "CERTIFICATE", der, 0o644)
		writeKey(filepath.Join(dir, name+".key"), key)
	}

	issue("api")
	issue("postgres", "soglia-postgres")
	fmt.Printf("wrote CA and server certificates to %s\n", dir)
	fmt.Println("trust ca.crt in your browser/OS to avoid the local self-signed warning")
}

func writePEM(path, blockType string, der []byte, mode os.FileMode) {
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: blockType, Bytes: der}), mode); err != nil {
		fatal(err)
	}
}

func writeKey(path string, key *ecdsa.PrivateKey) {
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		fatal(err)
	}
	writePEM(path, "EC PRIVATE KEY", der, 0o600)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "gen-certs:", err)
	os.Exit(1)
}
