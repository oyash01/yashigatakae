// Package tls gives bifrost + kintsugi a one-flag path to HTTPS.
//
// Usage from a server cmd:
//
//   tlsCfg, finalize, err := tls.Configure(ctx, tls.Options{
//     Domain:    *tlsDomain,    // e.g. "yashi.example.com" — Let's Encrypt
//     CacheDir:  *tlsCacheDir,  // optional, defaults under ~/.yashigatakae/
//     SelfSignedFor: "yashigatakae", // CN used when no domain set
//   })
//   if err != nil { return err }
//   if finalize != nil { defer finalize() }
//   server.TLSConfig = tlsCfg
//   server.ListenAndServeTLS("", "")
//
// When Options.Domain is set, autocert manages a real Let's Encrypt cert
// and you ALSO need to start the HTTP-01 challenge listener on :80
// (Configure returns one ready to go via ChallengeServer).
//
// Without Options.Domain we fall back to a self-signed cert cached at
// ~/.yashigatakae/self-signed/{cert.pem,key.pem} — fine for trusted
// internal use, will trip browser warnings if exposed publicly.
package tls

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	stdtls "crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/crypto/acme/autocert"

	"github.com/oyash01/yashigatakae/internal/osdetect"
)

// Options is the public surface.
type Options struct {
	// Domain — when non-empty, request a real cert from Let's Encrypt.
	// Requires the public DNS A record for this domain to point at this host
	// AND port 80 to be reachable for the HTTP-01 challenge.
	Domain string

	// CacheDir — where autocert caches the cert it gets back. Defaults to
	// ~/.yashigatakae/autocert/.
	CacheDir string

	// SelfSignedFor — the CN written into the self-signed cert when no
	// Domain is set. Cosmetic only; required by x509.
	SelfSignedFor string
}

// Configure returns a *tls.Config the caller assigns to http.Server.TLSConfig
// and a finalize() func to defer (closes any background resources). On
// autocert mode, it also returns an http.Handler for the HTTP-01 challenge
// listener (caller must serve it on :80).
type Configured struct {
	Config           *stdtls.Config
	Finalize         func()
	ChallengeHandler http.Handler // nil when self-signed
	Mode             string       // "autocert" | "self-signed"
	SelfSignedPaths  [2]string    // [cert, key] for "self-signed" mode (display)
}

func Configure(ctx context.Context, opts Options) (*Configured, error) {
	if opts.Domain != "" {
		return configureAutocert(opts)
	}
	return configureSelfSigned(opts)
}

func configureAutocert(opts Options) (*Configured, error) {
	cacheDir := opts.CacheDir
	if cacheDir == "" {
		yash, _ := osdetect.YashigatakaeDir()
		cacheDir = filepath.Join(yash, "autocert")
	}
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		return nil, err
	}
	m := &autocert.Manager{
		Cache:      autocert.DirCache(cacheDir),
		Prompt:     autocert.AcceptTOS,
		HostPolicy: autocert.HostWhitelist(opts.Domain),
	}
	return &Configured{
		Config:           m.TLSConfig(),
		ChallengeHandler: m.HTTPHandler(nil), // ALPN-01 fallback if HTTP-01 unreachable
		Mode:             "autocert",
		Finalize:         func() {},
	}, nil
}

func configureSelfSigned(opts Options) (*Configured, error) {
	cn := opts.SelfSignedFor
	if cn == "" {
		cn = "yashigatakae"
	}
	yash, _ := osdetect.YashigatakaeDir()
	dir := filepath.Join(yash, "self-signed")
	certPath := filepath.Join(dir, "cert.pem")
	keyPath := filepath.Join(dir, "key.pem")

	if err := ensureSelfSigned(dir, certPath, keyPath, cn); err != nil {
		return nil, err
	}
	cert, err := stdtls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, err
	}
	return &Configured{
		Config:          &stdtls.Config{Certificates: []stdtls.Certificate{cert}, MinVersion: stdtls.VersionTLS12},
		Mode:            "self-signed",
		SelfSignedPaths: [2]string{certPath, keyPath},
		Finalize:        func() {},
	}, nil
}

func ensureSelfSigned(dir, certPath, keyPath, cn string) error {
	if _, err := os.Stat(certPath); err == nil {
		if _, err := os.Stat(keyPath); err == nil {
			return nil // already exist + readable
		}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return err
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: cn, Organization: []string{"yashigatakae"}},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment | x509.KeyUsageCertSign,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		IsCA:        true,
		DNSNames:    []string{cn, "localhost"},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		return err
	}

	certOut, err := os.Create(certPath)
	if err != nil {
		return err
	}
	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: der}); err != nil {
		certOut.Close()
		return err
	}
	certOut.Close()

	keyBytes, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return err
	}
	keyOut, err := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer keyOut.Close()
	return pem.Encode(keyOut, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})
}

// String describes the configured TLS state for startup logging.
func (c *Configured) String() string {
	switch c.Mode {
	case "autocert":
		return "TLS: autocert (Let's Encrypt) — challenge listener on :80 required"
	case "self-signed":
		return fmt.Sprintf("TLS: self-signed (cert=%s key=%s)", c.SelfSignedPaths[0], c.SelfSignedPaths[1])
	default:
		return "TLS: disabled"
	}
}
