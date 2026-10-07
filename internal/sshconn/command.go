// Package sshconn is the common raw-argv and shell-rendering boundary for OpenSSH.
package sshconn

import (
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"
	"unicode"
)

type AuthMethod string

const (
	ConfigAgent    AuthMethod = "config-agent"
	PasswordPrompt AuthMethod = "password-prompt"
	Identity       AuthMethod = "identity"
	Certificate    AuthMethod = "certificate"
	SecurityKey    AuthMethod = "security-key"
)

type Operation int

const (
	Background Operation = iota
	Interactive
	Attach
)

type Connection struct {
	Destination                   string
	Username                      string
	Port                          int
	IdentityFile, CertificateFile string
	Auth                          AuthMethod
	Jump                          []string
}
type Command struct {
	Executable string
	Args       []string
}

func SafeText(s string) bool {
	for _, r := range s {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
func SafeName(s string, max int) bool {
	if s == "" || len(s) > max || strings.HasPrefix(s, "-") {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.') {
			return false
		}
	}
	return true
}
func ValidateDestination(s string) error {
	invalid := errors.New("SSH destination must be a safe alias, host, IP, user@host, or ssh://user@host:port")
	if !SafeText(s) || len(s) > 384 || s == "" || strings.HasPrefix(s, "-") {
		return invalid
	}
	if strings.HasPrefix(s, "ssh://") {
		u, err := url.Parse(s)
		if err != nil || u.Scheme != "ssh" || u.Opaque != "" || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
			return invalid
		}
		if u.User != nil {
			if _, yes := u.User.Password(); yes || !safeLegacyUsername(u.User.Username()) {
				return invalid
			}
		}
		if net.ParseIP(u.Hostname()) == nil && !SafeName(u.Hostname(), 253) {
			return invalid
		}
		if strings.HasSuffix(u.Host, ":") {
			return invalid
		}
		if p := u.Port(); p != "" {
			n, err := strconv.Atoi(p)
			if err != nil || n < 1 || n > 65535 {
				return invalid
			}
		}
		return nil
	}
	if strings.Count(s, "@") > 1 {
		return invalid
	}
	user, host, has := strings.Cut(s, "@")
	if !has {
		host = user
	} else if !safeLegacyUsername(user) {
		return invalid
	}
	if net.ParseIP(host) == nil && !SafeName(host, 253) {
		return invalid
	}
	return nil
}
func safeLegacyUsername(s string) bool {
	// Embedded usernames are not command-line options. Preserve the opaque v1 grammar.
	return s != "" && len(s) <= 64 && SafeName("u"+s, 65)
}
func Validate(c Connection) error {
	if err := ValidateDestination(c.Destination); err != nil {
		return err
	}
	if c.Username != "" && !SafeName(c.Username, 64) {
		return errors.New("username contains unsupported characters")
	}
	if c.Port < 0 || c.Port > 65535 {
		return errors.New("port must be 1–65535 or unspecified")
	}
	for _, path := range []string{c.IdentityFile, c.CertificateFile} {
		if !SafeText(path) || len(path) > 4096 {
			return errors.New("identity or certificate path contains unsupported characters")
		}
	}
	switch c.Auth {
	case "", ConfigAgent:
	case PasswordPrompt:
		if c.IdentityFile != "" || c.CertificateFile != "" {
			return errors.New("password prompt cannot specify identity or certificate")
		}
	case Identity, SecurityKey:
		if c.IdentityFile == "" || c.CertificateFile != "" {
			return errors.New("selected authentication requires only an identity file")
		}
	case Certificate:
		if c.IdentityFile == "" || c.CertificateFile == "" {
			return errors.New("certificate requires identity and certificate files")
		}
	default:
		return errors.New("unsupported authentication method")
	}
	if len(c.Jump) > 3 {
		return errors.New("SSH route exceeds four hosts")
	}
	seen := map[string]bool{c.Destination: true}
	for _, hop := range c.Jump {
		if err := ValidateDestination(hop); err != nil {
			return err
		}
		if seen[hop] {
			return errors.New("SSH route contains a cycle")
		}
		seen[hop] = true
	}
	return nil
}
func Build(c Connection, op Operation, remoteCommand string) (Command, error) {
	if err := Validate(c); err != nil {
		return Command{}, err
	}
	cmd := Command{Executable: "ssh", Args: []string{"-a"}}
	if op == Background {
		cmd.Args = append(cmd.Args, "-o", "BatchMode=yes")
	} else if op != Interactive && op != Attach {
		return Command{}, errors.New("unsupported SSH operation")
	}
	if c.Username != "" {
		cmd.Args = append(cmd.Args, "-l", c.Username)
	}
	if c.Port != 0 {
		cmd.Args = append(cmd.Args, "-p", strconv.Itoa(c.Port))
	}
	if c.IdentityFile != "" {
		cmd.Args = append(cmd.Args, "-i", c.IdentityFile)
	}
	if c.CertificateFile != "" {
		cmd.Args = append(cmd.Args, "-o", "CertificateFile="+quoteConfigValue(c.CertificateFile))
	}
	switch c.Auth {
	case Identity, Certificate, SecurityKey:
		cmd.Args = append(cmd.Args, "-o", "IdentitiesOnly=yes")
	case PasswordPrompt:
		cmd.Args = append(cmd.Args, "-o", "PreferredAuthentications=keyboard-interactive,password", "-o", "PubkeyAuthentication=no")
	}
	if len(c.Jump) > 0 {
		cmd.Args = append(cmd.Args, "-J", strings.Join(c.Jump, ","))
	}
	if op == Attach {
		cmd.Args = append(cmd.Args, "-t")
	}
	cmd.Args = append(cmd.Args, "--", c.Destination)
	if remoteCommand != "" {
		cmd.Args = append(cmd.Args, remoteCommand)
	}
	return cmd, nil
}

// OpenSSH parses -o values as configuration, independently of the local shell.
func quoteConfigValue(s string) string {
	return "\"" + strings.NewReplacer("\\", "\\\\", "\"", "\\\"").Replace(s) + "\""
}
func QuotePOSIX(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
func RenderPOSIX(c Command) string {
	values := append([]string{c.Executable}, c.Args...)
	for i := range values {
		values[i] = QuotePOSIX(values[i])
	}
	return strings.Join(values, " ")
}
