package mqtt

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mochigome-git/msp-go/pkg/config"
)

func selfSigned(t *testing.T) (certPEM, keyPEM []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	kder, _ := x509.MarshalECPrivateKey(key)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kder})
}

func TestBuildTLSConfig_FromFiles(t *testing.T) {
	cert, key := selfSigned(t)
	dir := t.TempDir()
	write := func(name string, b []byte) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, b, 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	cfg := config.AppConfig{
		CaCertFile:     write("ca.pem", cert),
		ClientCertFile: write("cert.pem", cert),
		ClientKeyFile:  write("key.pem", key),
	}
	tc, err := buildTLSConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if tc.RootCAs == nil || len(tc.Certificates) != 1 {
		t.Fatal("CA or client certificate not loaded")
	}
}

func TestBuildTLSConfig_ContentsWinOverFiles(t *testing.T) {
	cert, key := selfSigned(t)
	cfg := config.AppConfig{
		ECScaCert: string(cert), ECSclientCert: string(cert), ECSclientKey: string(key),
		CaCertFile: "/does/not/exist",
	}
	if _, err := buildTLSConfig(cfg); err != nil {
		t.Fatal(err)
	}
}

func TestBuildTLSConfig_Errors(t *testing.T) {
	cert, _ := selfSigned(t)
	for name, cfg := range map[string]config.AppConfig{
		"bad CA":       {ECScaCert: "not a pem"},
		"missing file": {CaCertFile: "/does/not/exist"},
		"cert w/o key": {ECSclientCert: string(cert)},
	} {
		if _, err := buildTLSConfig(cfg); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}
