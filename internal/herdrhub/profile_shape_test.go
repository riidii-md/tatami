package herdrhub

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestMalformedV2HostsShapePreserved(t *testing.T) {
	for _, source := range []string{`{"version":2}`, `{"version":2,"hosts":null}`, `{"version":2,"hosts":{}}`} {
		path := filepath.Join(t.TempDir(), "hosts.json")
		if err := os.WriteFile(path, []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
		s := NewStore(path)
		if _, err := s.ListProfiles(); err == nil {
			t.Errorf("malformed shape loaded: %s", source)
		}
		if err := s.SaveProfiles(nil); err == nil {
			t.Errorf("malformed shape overwritten: %s", source)
		}
		data, _ := os.ReadFile(path)
		if !bytes.Equal(data, []byte(source)) {
			t.Errorf("source changed: %s", data)
		}
	}
	if _, _, err := decodeProfiles([]byte(`{"version":2,"hosts":[]}`)); err != nil {
		t.Fatal(err)
	}
}
func TestLegacyURIUsernameGrammarRemainsAccepted(t *testing.T) {
	for _, target := range []string{"ssh://-u@host", "_u@host"} {
		data := []byte(`{"hosts":[{"id":"box","label":"Box","target":"` + target + `"}]}`)
		profiles, _, err := decodeProfiles(data)
		if err != nil {
			t.Errorf("legacy %q rejected: %v", target, err)
			continue
		}
		if _, err := profiles[0].Endpoint(); err != nil {
			t.Errorf("legacy endpoint %q rejected: %v", target, err)
		}
	}
}
