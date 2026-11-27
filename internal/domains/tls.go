package domains

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sudoku-golang/internal/fsutil"
	"sudoku-golang/internal/sysproc"
	"time"
)

const (
	CertName     = "sudoku.pem"
	KeyName      = "sudoku-key.pem"
	TLSConfig    = "sudoku-tls.yaml"
	certSubdir   = "certs"
	mkcertTimout = time.Minute
)

var ErrNoMkcert = errors.New("mkcert не найден — установите его: https://github.com/FiloSottile/mkcert#installation")

// CertStatus describes the local certificate sudoku manages for Traefik.
type CertStatus struct {
	Mkcert       bool       `json:"mkcert"`
	MkcertPath   string     `json:"mkcertPath"`
	CARoot       string     `json:"caRoot"`
	CAReady      bool       `json:"caReady"`
	InstallHint  string     `json:"installHint"`
	CertFile     string     `json:"certFile"`
	Exists       bool       `json:"exists"`
	Hosts        []string   `json:"hosts"`
	Missing      []string   `json:"missing"`
	NotAfter     *time.Time `json:"notAfter"`
	TraefikFile  string     `json:"traefikFile"`
	TraefikReady bool       `json:"traefikReady"`
	Dir          string     `json:"dir"`
}

func installHint() string {
	switch {
	case os.Getenv("WSL_DISTRO_NAME") != "":
		return "WSL: запустите mkcert -install в Windows, а CAROOT укажите на его папку"
	default:
		return "mkcert -install   # один раз: добавит локальный CA в системное и браузерное хранилище"
	}
}

func mkcert(ctx context.Context, args ...string) (string, error) {
	bin, err := exec.LookPath("mkcert")
	if err != nil {
		return "", ErrNoMkcert
	}
	ctx, cancel := context.WithTimeout(ctx, mkcertTimout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	sysproc.HideWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("mkcert: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func readCert(file string) (*x509.Certificate, error) {
	raw, err := os.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("read cert: %w", err)
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		return nil, errors.New("cert: no PEM block")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse cert: %w", err)
	}
	return cert, nil
}

func covers(cert *x509.Certificate, host string) bool {
	return cert.VerifyHostname(host) == nil
}

// Status inspects mkcert, the CA and the certificate in dir (the Traefik custom dir on the host).
func Status(ctx context.Context, dir string, hosts []string) CertStatus {
	st := CertStatus{
		Dir: dir, CertFile: filepath.Join(dir, certSubdir, CertName), TraefikFile: filepath.Join(dir, TLSConfig),
		Hosts: []string{}, Missing: []string{}, InstallHint: installHint(),
	}
	if p, err := exec.LookPath("mkcert"); err == nil {
		st.Mkcert, st.MkcertPath = true, p
		if out, err := mkcert(ctx, "-CAROOT"); err == nil {
			st.CARoot = strings.TrimSpace(out)
			if _, err := os.Stat(filepath.Join(st.CARoot, "rootCA.pem")); err == nil {
				st.CAReady = true
			}
		}
	}
	if _, err := os.Stat(st.TraefikFile); err == nil {
		st.TraefikReady = true
	}
	cert, err := readCert(st.CertFile)
	if err != nil {
		st.Missing = append(st.Missing, hosts...)
		return st
	}
	st.Exists = true
	na := cert.NotAfter.UTC()
	st.NotAfter = &na
	st.Hosts = append(st.Hosts, cert.DNSNames...)
	for _, h := range hosts {
		if !covers(cert, h) {
			st.Missing = append(st.Missing, h)
		}
	}
	return st
}

// Covered reports which hosts the current certificate covers.
func Covered(dir string, hosts []string) map[string]bool {
	out := map[string]bool{}
	cert, err := readCert(filepath.Join(dir, certSubdir, CertName))
	if err != nil {
		return out
	}
	for _, h := range hosts {
		out[h] = covers(cert, h)
	}
	return out
}

// Issue creates (or replaces) the certificate for hosts with mkcert and writes the Traefik dynamic
// config that serves it as the default certificate. containerDir is where dir is mounted inside the
// Traefik container (e.g. /etc/traefik/custom).
func Issue(ctx context.Context, dir, containerDir string, hosts []string) (string, error) {
	names := map[string]bool{"localhost": true}
	for _, h := range hosts {
		if validHost(h) {
			names[h] = true
		}
	}
	list := make([]string, 0, len(names)+2)
	for h := range names {
		list = append(list, h)
	}
	sort.Strings(list)
	list = append(list, "127.0.0.1", "::1")
	certDir := filepath.Join(dir, certSubdir)
	if err := os.MkdirAll(certDir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", certDir, err)
	}
	args := append([]string{"-cert-file", filepath.Join(certDir, CertName), "-key-file", filepath.Join(certDir, KeyName)}, list...)
	out, err := mkcert(ctx, args...)
	if err != nil {
		return out, err
	}
	inCert := path.Join(containerDir, certSubdir, CertName)
	inKey := path.Join(containerDir, certSubdir, KeyName)
	cfg := fmt.Sprintf(`# Создано sudoku: локальный сертификат mkcert для Traefik. Перевыпуск — «Сеть → Домены и TLS».
tls:
  certificates:
    - certFile: %[1]s
      keyFile: %[2]s
  stores:
    default:
      defaultCertificate:
        certFile: %[1]s
        keyFile: %[2]s
`, inCert, inKey)
	if err := fsutil.WriteFileAtomicNoBackup(filepath.Join(dir, TLSConfig), []byte(cfg)); err != nil {
		return out, err //nolint:wrapcheck // descriptive
	}
	return out, nil
}
