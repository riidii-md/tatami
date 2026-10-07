package herdrhub

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/OleksandrBesan/tatami/internal/sshconn"
)

type ConnectionMode string

const (
	ModeLegacy   ConnectionMode = "legacy"
	ModeAlias    ConnectionMode = "alias"
	ModeExplicit ConnectionMode = "explicit"
)

type HostConnection struct {
	Mode                  ConnectionMode     `json:"mode"`
	Hostname              string             `json:"hostname,omitempty"`
	Alias                 string             `json:"alias,omitempty"`
	Username              string             `json:"username,omitempty"`
	Port                  int                `json:"port,omitempty"`
	Auth                  sshconn.AuthMethod `json:"auth,omitempty"`
	IdentityFile          string             `json:"identity_file,omitempty"`
	CertificateFile       string             `json:"certificate_file,omitempty"`
	Jump                  []string           `json:"jump,omitempty"`
	JumpAlias             string             `json:"jump_alias,omitempty"`
	AdvertisedDestination string             `json:"advertised_destination,omitempty"`
	TatamiExecutable      string             `json:"tatami_executable,omitempty"`
	HerdrExecutable       string             `json:"herdr_executable,omitempty"`
}
type SavedHost struct {
	ID                   string         `json:"id"`
	Label                string         `json:"label"`
	Target               string         `json:"target"`
	Group                string         `json:"group,omitempty"`
	Tags                 []string       `json:"tags,omitempty"`
	ConnectivityRevision string         `json:"connectivity_revision"`
	Connection           HostConnection `json:"connection"`
}

// AdvertisedHost is the complete allowlist shared with peers.
type AdvertisedHost struct {
	ID     string       `json:"id"`
	Label  string       `json:"label"`
	Target string       `json:"target"`
	Kind   EndpointKind `json:"kind,omitempty"`
}

func (a AdvertisedHost) Endpoint() Endpoint {
	return Endpoint{ID: a.ID, Label: a.Label, Target: a.Target, Kind: EndpointSSH}
}
func LegacyProfile(e Endpoint) SavedHost {
	digest := sha256.Sum256([]byte(fmt.Sprintf("%d:%s%d:%s", len(e.ID), e.ID, len(e.Target), e.Target)))
	return SavedHost{ID: e.ID, Label: e.Label, Target: e.Target, ConnectivityRevision: hex.EncodeToString(digest[:]), Connection: HostConnection{Mode: ModeLegacy}}
}
func safeExecutableOverride(value string) bool {
	if len(value) < 2 || len(value) > 4096 || !strings.HasPrefix(value, "/") {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("/._+~-", r)) {
			return false
		}
	}
	return true
}
func ValidateProfile(p SavedHost) error {
	if err := ValidateEndpoint(Endpoint{ID: p.ID, Label: p.Label, Target: "validation"}); err != nil {
		return err
	}
	if err := validateDisplayField("group", p.Group, 128); err != nil {
		return err
	}
	if len(p.Tags) > 32 {
		return errors.New("at most 32 tags are supported")
	}
	for _, tag := range p.Tags {
		if err := validateDisplayField("tag", tag, 64); err != nil {
			return err
		}
	}
	c := p.Connection
	for _, value := range []string{c.TatamiExecutable, c.HerdrExecutable} {
		if value != "" && !safeExecutableOverride(value) {
			return errors.New("remote executable must be an absolute POSIX path using letters, numbers, / . _ + ~ - only")
		}
	}
	if c.JumpAlias != "" && !safeSSHNamePart(c.JumpAlias, 253, true) {
		return errors.New("jump alias must be an OpenSSH alias")
	}
	if c.AdvertisedDestination != "" {
		if err := validateSSHDestination(c.AdvertisedDestination); err != nil {
			return err
		}
	}
	switch c.Mode {
	case ModeLegacy:
		if err := validateSSHDestination(p.Target); err != nil {
			return err
		}
		if c.Hostname != "" || c.Alias != "" || c.Username != "" || c.Port != 0 || c.IdentityFile != "" || c.CertificateFile != "" || len(c.Jump) > 0 || c.Auth != "" && c.Auth != sshconn.ConfigAgent {
			return errors.New("legacy mode preserves only its opaque destination; convert explicitly to edit credentials")
		}
	case ModeAlias:
		if !safeSSHNamePart(c.Alias, 253, true) {
			return errors.New("SSH alias is required")
		}
		if c.Hostname != "" || c.Username != "" || c.Port != 0 || c.IdentityFile != "" || c.CertificateFile != "" || len(c.Jump) > 0 || c.Auth != "" && c.Auth != sshconn.ConfigAgent {
			return errors.New("alias mode owns credentials, username, port and jumps in OpenSSH config")
		}
	case ModeExplicit:
		if (c.Auth == "" || c.Auth == sshconn.ConfigAgent) && (c.IdentityFile != "" || c.CertificateFile != "") {
			return errors.New("select identity or certificate authentication to specify credential paths")
		}
		if c.Alias != "" || strings.Contains(c.Hostname, "@") || strings.HasPrefix(c.Hostname, "ssh://") {
			return errors.New("hostname must not include username or SSH URI")
		}
		if net.ParseIP(c.Hostname) == nil && !safeSSHNamePart(c.Hostname, 253, true) {
			return errors.New("hostname or IP is required")
		}
	default:
		return errors.New("unsupported connection mode")
	}
	return sshconn.Validate(p.RawConnection())
}
func (p SavedHost) RawConnection() sshconn.Connection {
	c := p.Connection
	destination := c.Hostname
	if c.Mode == ModeAlias {
		destination = c.Alias
	}
	if c.Mode == ModeLegacy {
		destination = p.Target
	}
	return sshconn.Connection{Destination: destination, Username: c.Username, Port: c.Port, Auth: c.Auth, IdentityFile: c.IdentityFile, CertificateFile: c.CertificateFile, Jump: append([]string(nil), c.Jump...)}
}
func (p SavedHost) Destination() string {
	c := p.Connection
	if c.Mode == ModeLegacy {
		return p.Target
	}
	if c.Mode == ModeAlias {
		return c.Alias
	}
	host := c.Hostname
	if net.ParseIP(host) != nil && strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if c.Port > 0 {
		host += ":" + strconv.Itoa(c.Port)
	}
	u := url.URL{Scheme: "ssh", Host: host}
	if c.Username != "" {
		u.User = url.User(c.Username)
	}
	return u.String()
}
func (p SavedHost) Endpoint() (Endpoint, error) {
	if err := ValidateProfile(p); err != nil {
		return Endpoint{}, err
	}
	c := p.RawConnection()
	ep := Endpoint{ID: p.ID, Label: p.Label, Target: p.Destination(), Kind: EndpointSSH, Via: append([]string(nil), p.Connection.Jump...), Profile: &p, TatamiExecutable: p.Connection.TatamiExecutable, HerdrExecutable: p.Connection.HerdrExecutable, Group: p.Group, Tags: append([]string(nil), p.Tags...), RootID: p.ID, RootRevision: p.ConnectivityRevision}
	if p.Connection.Mode != ModeLegacy {
		ep.Connection = &c
	}
	return ep, nil
}
func (p SavedHost) Advertised() (AdvertisedHost, bool, error) {
	if err := ValidateProfile(p); err != nil {
		return AdvertisedHost{}, false, err
	}
	target := p.Destination()
	if p.Connection.AdvertisedDestination != "" {
		target = p.Connection.AdvertisedDestination
	} else if p.Connection.Mode == ModeAlias {
		return AdvertisedHost{}, false, nil
	}
	return AdvertisedHost{ID: p.ID, Label: p.Label, Target: target}, true, nil
}
func projectEndpoint(e Endpoint) (AdvertisedHost, bool, error) {
	if e.Profile != nil {
		return e.Profile.Advertised()
	}
	if err := ValidateEndpoint(e); err != nil {
		return AdvertisedHost{}, false, err
	}
	return AdvertisedHost{ID: e.ID, Label: e.Label, Target: e.Target, Kind: EndpointSSH}, true, nil
}
func GenerateHostID(label string, existing []SavedHost) string {
	var b strings.Builder
	for _, r := range strings.ToLower(label) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		} else if b.Len() > 0 && !strings.HasSuffix(b.String(), "-") {
			b.WriteByte('-')
		}
	}
	base := strings.Trim(b.String(), "-")
	if base == "" {
		base = "host"
	}
	if len(base) > 64 {
		base = strings.TrimRight(base[:64], "-")
	}
	used := map[string]bool{LocalEndpointID: true}
	for _, p := range existing {
		used[p.ID] = true
	}
	for n := 1; ; n++ {
		id := base
		if n > 1 {
			suffix := "-" + strconv.Itoa(n)
			head := base
			if len(head)+len(suffix) > 64 {
				head = strings.TrimRight(head[:64-len(suffix)], "-")
			}
			id = head + suffix
		}
		if !used[id] {
			return id
		}
	}
}
func newRevision() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
func NewConnectivityRevision() (string, error) { return newRevision() }
func NormalizeProfile(p SavedHost) (SavedHost, error) {
	p.Label = strings.TrimSpace(p.Label)
	p.Group = strings.TrimSpace(p.Group)
	tags := []string{}
	seen := map[string]bool{}
	for _, t := range p.Tags {
		t = strings.TrimSpace(t)
		if t != "" && !seen[t] {
			seen[t] = true
			tags = append(tags, t)
		}
	}
	p.Tags = tags
	for _, path := range []*string{&p.Connection.IdentityFile, &p.Connection.CertificateFile} {
		if *path == "" {
			continue
		}
		if strings.HasPrefix(*path, "~/") {
			home, err := os.UserHomeDir()
			if err != nil {
				return p, err
			}
			*path = filepath.Join(home, (*path)[2:])
		}
		absolute, err := filepath.Abs(*path)
		if err != nil {
			return p, err
		}
		*path = absolute
	}
	p.Target = p.Destination()
	return p, ValidateProfile(p)
}
func (p SavedHost) CheckIdentityFiles() error {
	for name, path := range map[string]string{"identity file": p.Connection.IdentityFile, "certificate file": p.Connection.CertificateFile} {
		if path == "" {
			continue
		}
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("%s is missing or not a regular file; check the local path", name)
		}
	}
	return nil
}
