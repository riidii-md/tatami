package herdrhub

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Frozen from 68b2466: this old reader/writer never uses current domain types.
type frozenV1Host struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Kind   string `json:"kind,omitempty"`
	Target string `json:"target,omitempty"`
}
type frozenV1Document struct {
	Hosts []frozenV1Host `json:"hosts"`
}

func frozenValidateEndpoint(e frozenV1Host) error {
	if e.ID == "local" {
		return errors.New("local endpoint is reserved")
	}
	if strings.TrimSpace(e.ID) == "" || strings.TrimSpace(e.Label) == "" {
		return errors.New("endpoint id and label are required")
	}
	if len(e.ID) > 64 || !frozenasciiAlphaNumeric(rune(e.ID[0])) || !frozenasciiAlphaNumeric(rune(e.ID[len(e.ID)-1])) {
		return errors.New("endpoint id must start and end with a lowercase letter or number")
	}
	for _, r := range e.ID {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
			return errors.New("endpoint id must be a stable safe slug")
		}
	}
	if err := frozenvalidateDisplayField("endpoint label", e.Label, 128); err != nil {
		return err
	}
	if e.Kind != "" && e.Kind != "ssh" {
		return fmt.Errorf("unsupported endpoint kind %q", e.Kind)
	}
	t := strings.TrimSpace(e.Target)
	if t == "" {
		return errors.New("SSH destination is required")
	}
	if err := frozenvalidateSSHDestination(t); err != nil {
		return err
	}
	return nil
}

func frozenasciiAlphaNumeric(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9'
}

func frozenvalidateSSHDestination(destination string) error {
	if strings.HasPrefix(destination, "ssh://") {
		return frozenvalidateSSHURI(destination)
	}
	if len(destination) > 320 || strings.HasPrefix(destination, "-") || strings.Count(destination, "@") > 1 {
		return frozeninvalidSSHDestination()
	}
	user, host, hasUser := strings.Cut(destination, "@")
	if !hasUser {
		host = user
		user = ""
	}
	if hasUser && !frozensafeSSHNamePart(user, 64, false) {
		return frozeninvalidSSHDestination()
	}
	if !frozensafeSSHNamePart(host, 253, true) {
		return frozeninvalidSSHDestination()
	}
	return nil
}

func frozenvalidateSSHURI(destination string) error {
	if len(destination) > 384 {
		return frozeninvalidSSHDestination()
	}
	parsed, err := url.Parse(destination)
	if err != nil || parsed.Scheme != "ssh" || parsed.Opaque != "" || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return frozeninvalidSSHDestination()
	}
	if parsed.User != nil {
		if _, hasPassword := parsed.User.Password(); hasPassword || !frozensafeSSHNamePart(parsed.User.Username(), 64, false) {
			return frozeninvalidSSHDestination()
		}
	}
	host := parsed.Hostname()
	if net.ParseIP(host) == nil && !frozensafeSSHNamePart(host, 253, true) {
		return frozeninvalidSSHDestination()
	}
	if strings.HasSuffix(parsed.Host, ":") {
		return frozeninvalidSSHDestination()
	}
	if port := parsed.Port(); port != "" {
		value, err := strconv.Atoi(port)
		if err != nil || value < 1 || value > 65535 {
			return frozeninvalidSSHDestination()
		}
	}
	return nil
}

func frozeninvalidSSHDestination() error {
	return errors.New("SSH destination must be a safe alias, host, IP, user@host, or ssh://user@host:port")
}

func frozensafeSSHNamePart(value string, max int, requireAlphaNumericEdges bool) bool {
	if value == "" || len(value) > max {
		return false
	}
	if requireAlphaNumericEdges && (!frozenasciiAlphaNumeric(rune(value[0])) || !frozenasciiAlphaNumeric(rune(value[len(value)-1]))) {
		return false
	}
	for _, r := range value {
		if !frozenasciiAlphaNumeric(r) && r != '-' && r != '_' && r != '.' {
			return false
		}
	}
	return true
}

func frozenvalidateDisplayField(name, value string, max int) error {
	if len(value) > max {
		return fmt.Errorf("%s is too long", name)
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f || r >= 0x80 && r <= 0x9f {
			return fmt.Errorf("%s contains terminal control characters", name)
		}
	}
	return nil
}

func TestFrozenV1SaveRoundTripAndBackupRollback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hosts.json")
	original, err := os.ReadFile("testdata/hosts-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	store := NewStore(path)
	profiles, err := store.ListProfiles()
	if err != nil {
		t.Fatal(err)
	}
	profiles[0].Label = "Edited legacy"
	profiles = append(profiles, SavedHost{ID: "new", Label: "New explicit", Connection: HostConnection{Mode: ModeExplicit, Hostname: "example.test", Username: "fixture", Port: 2222}}, SavedHost{ID: "alias", Label: "Local-only alias", Connection: HostConnection{Mode: ModeAlias, Alias: "private-alias"}})
	if err := store.SaveProfiles(profiles); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	var old frozenV1Document
	if err := json.Unmarshal(data, &old); err != nil {
		t.Fatal(err)
	}
	if len(old.Hosts) != 3 {
		t.Fatalf("old reader lost rows: %s", data)
	}
	for _, host := range old.Hosts {
		if err := frozenValidateEndpoint(host); err != nil {
			t.Fatalf("frozen v1 rejected %+v: %v", host, err)
		}
	}
	// An old save retains projections and deliberately strips additive v2 fields.
	rewritten, err := json.MarshalIndent(old, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(rewritten, []byte("connection")) || bytes.Contains(rewritten, []byte("group")) {
		t.Fatal("old writer retained new fields")
	}
	if err := os.WriteFile(path, rewritten, 0600); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.ListProfiles()
	if err != nil || len(loaded) != 3 {
		t.Fatalf("old save not readable: %+v %v", loaded, err)
	}
	for _, profile := range loaded {
		if profile.Connection.Mode != ModeLegacy {
			t.Fatal("old save unexpectedly retained auth")
		}
		if _, err := profile.Endpoint(); err != nil {
			t.Fatal(err)
		}
	}
	backup, err := os.ReadFile(path + ".v1.bak")
	if err != nil || !bytes.Equal(backup, original) {
		t.Fatal("backup not original")
	}
	if err := os.WriteFile(path, backup, 0600); err != nil {
		t.Fatal(err)
	}
	restored, err := store.ListProfiles()
	if err != nil || len(restored) != 1 || restored[0].Target != "ssh://u@box:2222" {
		t.Fatalf("rollback failed: %+v %v", restored, err)
	}
}
