package honeypot

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/crypto/ssh"
)

func stateDir(dir string) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("state path must be a private directory")
	}
	return os.Chmod(dir, 0700)
}
func privateFile(path string, create func() ([]byte, error)) ([]byte, error) {
	info, err := os.Lstat(path)
	if err == nil {
		if !info.Mode().IsRegular() || info.Size() > 16384 {
			return nil, fmt.Errorf("invalid key file %s", path)
		}
		if err = os.Chmod(path, 0600); err != nil {
			return nil, err
		}
		return os.ReadFile(path)
	}
	if !os.IsNotExist(err) {
		return nil, err
	}
	b, err := create()
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, err
	}
	_, writeErr := f.Write(b)
	closeErr := f.Close()
	if writeErr != nil {
		return nil, writeErr
	}
	return b, closeErr
}
func newKey() ([]byte, error) {
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	b, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: b}), nil
}
func hostKey(dir string) (ssh.Signer, error) {
	b, err := privateFile(filepath.Join(dir, "ssh_host_ed25519"), newKey)
	if err != nil {
		return nil, err
	}
	return ssh.ParsePrivateKey(b)
}
func tlsCertificate(dir string) (tls.Certificate, error) {
	keyPEM, err := privateFile(filepath.Join(dir, "tls_key.pem"), newKey)
	if err != nil {
		return tls.Certificate{}, err
	}
	certPEM, err := privateFile(filepath.Join(dir, "tls_cert.pem"), func() ([]byte, error) {
		block, _ := pem.Decode(keyPEM)
		if block == nil {
			return nil, fmt.Errorf("invalid TLS key")
		}
		key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, err
		}
		priv, ok := key.(ed25519.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("TLS key must be Ed25519")
		}
		serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
		if err != nil {
			return nil, err
		}
		template := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "Grey Sector"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().AddDate(1, 0, 0), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, DNSNames: []string{"station.invalid"}}
		der, err := x509.CreateCertificate(rand.Reader, template, template, priv.Public(), priv)
		if err != nil {
			return nil, err
		}
		return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), nil
	})
	if err != nil {
		return tls.Certificate{}, err
	}
	return tls.X509KeyPair(certPEM, keyPEM)
}
