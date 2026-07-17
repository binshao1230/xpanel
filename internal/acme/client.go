package acme

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-acme/lego/v4/certcrypto"
	"github.com/go-acme/lego/v4/certificate"
	"github.com/go-acme/lego/v4/lego"
	"github.com/go-acme/lego/v4/providers/dns/alidns"
	"github.com/go-acme/lego/v4/providers/dns/cloudflare"
	"github.com/go-acme/lego/v4/providers/dns/tencentcloud"
	"github.com/go-acme/lego/v4/registration"
)

const (
	ChallengeHTTP01 = "http-01"
	ChallengeDNS01  = "dns-01"

	ProviderCloudflare   = "cloudflare"
	ProviderAliDNS       = "alidns"
	ProviderTencentCloud = "tencentcloud"
	ProviderManual       = "manual"

	// CertPlaceholder is replaced by the agent with its local certs directory.
	CertPlaceholder = "{{CERTS}}"
)

// IssueRequest describes a certificate obtain/renew job.
type IssueRequest struct {
	Email        string
	Domains      []string
	Challenge    string // http-01 | dns-01
	DNSProvider  string // cloudflare | alidns | tencentcloud
	DNSAPIToken  string // cloudflare token
	DNSAPIKey    string // aliyun AccessKeyId / tencent SecretId
	DNSAPISecret string // aliyun AccessKeySecret / tencent SecretKey
	Staging      bool
	Bundle       bool
}

type Result struct {
	Domain    string
	CertPEM   string
	KeyPEM    string
	IssuerPEM string
	ExpireAt  time.Time
	URL       string
}

type Manager struct {
	DataDir string
	HTTP01  *HTTP01Provider
	account *Account
}

func NewManager(dataDir string) (*Manager, error) {
	if err := os.MkdirAll(filepath.Join(dataDir, "acme"), 0o700); err != nil {
		return nil, err
	}
	return &Manager{
		DataDir: dataDir,
		HTTP01:  NewHTTP01Provider(),
	}, nil
}

type Account struct {
	Email        string
	Registration *registration.Resource
	key          crypto.PrivateKey
}

func (a *Account) GetEmail() string                        { return a.Email }
func (a *Account) GetRegistration() *registration.Resource { return a.Registration }
func (a *Account) GetPrivateKey() crypto.PrivateKey        { return a.key }

func (m *Manager) loadOrCreateAccount(email string, staging bool) (*Account, *lego.Client, error) {
	if email == "" {
		return nil, nil, fmt.Errorf("acme email required")
	}
	keyPath := filepath.Join(m.DataDir, "acme", accountKeyName(email, staging))
	var priv *ecdsa.PrivateKey
	if b, err := os.ReadFile(keyPath); err == nil {
		block, _ := pem.Decode(b)
		if block == nil {
			return nil, nil, fmt.Errorf("invalid account key pem")
		}
		k, err := x509.ParseECPrivateKey(block.Bytes)
		if err != nil {
			return nil, nil, err
		}
		priv = k
	} else {
		k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return nil, nil, err
		}
		priv = k
		der, err := x509.MarshalECPrivateKey(priv)
		if err != nil {
			return nil, nil, err
		}
		pemBytes := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der})
		if err := os.WriteFile(keyPath, pemBytes, 0o600); err != nil {
			return nil, nil, err
		}
	}

	acc := &Account{Email: email, key: priv}
	cfg := lego.NewConfig(acc)
	if staging {
		cfg.CADirURL = lego.LEDirectoryStaging
	} else {
		cfg.CADirURL = lego.LEDirectoryProduction
	}
	cfg.Certificate.KeyType = certcrypto.EC256

	client, err := lego.NewClient(cfg)
	if err != nil {
		return nil, nil, err
	}

	reg, err := client.Registration.Register(registration.RegisterOptions{TermsOfServiceAgreed: true})
	if err != nil {
		reg, err = client.Registration.ResolveAccountByKey()
		if err != nil {
			return nil, nil, fmt.Errorf("acme register: %w", err)
		}
	}
	acc.Registration = reg
	m.account = acc
	return acc, client, nil
}

func accountKeyName(email string, staging bool) string {
	safe := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '@' || r == '.' || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, email)
	suffix := "prod"
	if staging {
		suffix = "staging"
	}
	return "account-" + safe + "-" + suffix + ".key"
}

func (m *Manager) Obtain(req IssueRequest) (*Result, error) {
	if len(req.Domains) == 0 {
		return nil, fmt.Errorf("domains required")
	}
	for i := range req.Domains {
		req.Domains[i] = strings.TrimSpace(strings.ToLower(req.Domains[i]))
	}
	if req.Challenge == "" {
		req.Challenge = ChallengeHTTP01
	}

	_, client, err := m.loadOrCreateAccount(req.Email, req.Staging)
	if err != nil {
		return nil, err
	}

	switch req.Challenge {
	case ChallengeHTTP01:
		if err := client.Challenge.SetHTTP01Provider(m.HTTP01); err != nil {
			return nil, err
		}
	case ChallengeDNS01:
		if err := m.setDNSProvider(client, req); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unsupported challenge: %s", req.Challenge)
	}

	cert, err := client.Certificate.Obtain(certificate.ObtainRequest{
		Domains: req.Domains,
		Bundle:  true,
	})
	if err != nil {
		return nil, fmt.Errorf("obtain: %w", err)
	}
	return parseCertResult(cert)
}

func (m *Manager) Renew(req IssueRequest) (*Result, error) {
	return m.Obtain(req)
}

func (m *Manager) setDNSProvider(client *lego.Client, req IssueRequest) error {
	switch strings.ToLower(req.DNSProvider) {
	case ProviderCloudflare, "cf", "":
		token := req.DNSAPIToken
		if token == "" {
			token = firstEnv("CLOUDFLARE_DNS_API_TOKEN", "CF_DNS_API_TOKEN")
		}
		if token == "" {
			return fmt.Errorf("cloudflare: dns_api_token required")
		}
		cfg := cloudflare.NewDefaultConfig()
		cfg.AuthToken = token
		p, err := cloudflare.NewDNSProviderConfig(cfg)
		if err != nil {
			return err
		}
		return client.Challenge.SetDNS01Provider(p)

	case ProviderAliDNS, "aliyun", "alibaba":
		key := req.DNSAPIKey
		secret := req.DNSAPISecret
		if key == "" {
			key = firstEnv("ALICLOUD_ACCESS_KEY", "ALIYUN_ACCESS_KEY")
		}
		if secret == "" {
			secret = firstEnv("ALICLOUD_SECRET_KEY", "ALIYUN_SECRET_KEY")
		}
		if key == "" || secret == "" {
			return fmt.Errorf("alidns: dns_api_key + dns_api_secret required")
		}
		cfg := alidns.NewDefaultConfig()
		cfg.APIKey = key
		cfg.SecretKey = secret
		p, err := alidns.NewDNSProviderConfig(cfg)
		if err != nil {
			return err
		}
		return client.Challenge.SetDNS01Provider(p)

	case ProviderTencentCloud, "dnspod", "tencent", "qcloud":
		id := req.DNSAPIKey
		secret := req.DNSAPISecret
		if id == "" {
			id = firstEnv("TENCENTCLOUD_SECRET_ID", "DNSPOD_SECRET_ID")
		}
		if secret == "" {
			secret = firstEnv("TENCENTCLOUD_SECRET_KEY", "DNSPOD_SECRET_KEY")
		}
		// also allow token field as secret id for convenience
		if id == "" && req.DNSAPIToken != "" {
			id = req.DNSAPIToken
		}
		if id == "" || secret == "" {
			return fmt.Errorf("tencentcloud: dns_api_key(SecretId) + dns_api_secret required")
		}
		cfg := tencentcloud.NewDefaultConfig()
		cfg.SecretID = id
		cfg.SecretKey = secret
		p, err := tencentcloud.NewDNSProviderConfig(cfg)
		if err != nil {
			return err
		}
		return client.Challenge.SetDNS01Provider(p)

	default:
		return fmt.Errorf("unsupported dns provider %q (cloudflare|alidns|tencentcloud)", req.DNSProvider)
	}
}

func firstEnv(keys ...string) string {
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return ""
}

func parseCertResult(cert *certificate.Resource) (*Result, error) {
	if cert == nil {
		return nil, fmt.Errorf("empty certificate")
	}
	expire, err := leafNotAfter(cert.Certificate)
	if err != nil {
		expire = time.Now().Add(90 * 24 * time.Hour)
	}
	return &Result{
		Domain:    cert.Domain,
		CertPEM:   string(cert.Certificate),
		KeyPEM:    string(cert.PrivateKey),
		IssuerPEM: string(cert.IssuerCertificate),
		ExpireAt:  expire,
		URL:       cert.CertURL,
	}, nil
}

func leafNotAfter(certPEM []byte) (time.Time, error) {
	rest := certPEM
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		c, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			continue
		}
		return c.NotAfter, nil
	}
	return time.Time{}, fmt.Errorf("no certificate in pem")
}

func (m *Manager) WriteFiles(domain, certPEM, keyPEM string) error {
	dir := filepath.Join(m.DataDir, "certs", sanitizeDomain(domain))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "fullchain.pem"), []byte(certPEM), 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "privkey.pem"), []byte(keyPEM), 0o600)
}

// AgentRelativePaths returns placeholder paths for xray config (agent expands {{CERTS}}).
func AgentRelativePaths(domain string) (certFile, keyFile string) {
	d := sanitizeDomain(domain)
	return CertPlaceholder + "/" + d + "/fullchain.pem", CertPlaceholder + "/" + d + "/privkey.pem"
}

func sanitizeDomain(d string) string {
	d = strings.TrimSpace(strings.ToLower(d))
	d = strings.ReplaceAll(d, "*", "_wildcard_")
	return d
}

func ParseExpireFromPEM(certPEM string) (time.Time, error) {
	return leafNotAfter([]byte(certPEM))
}

// SupportedDNSProviders for UI/API discovery.
func SupportedDNSProviders() []string {
	return []string{ProviderCloudflare, ProviderAliDNS, ProviderTencentCloud}
}
