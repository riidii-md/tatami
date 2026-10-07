package herdrhub

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
)

const HostStoreVersion = 2

type hostDocument struct {
	Version int         `json:"version"`
	Hosts   []SavedHost `json:"hosts"`
}

func decodeProfiles(data []byte) ([]SavedHost, int, error) {
	var version struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(data, &version); err != nil {
		return nil, 0, errors.New("Herdr hosts file is malformed; preserve it and repair before saving")
	}
	var profiles []SavedHost
	switch version.Version {
	case 0:
		var legacy struct {
			Hosts []Endpoint `json:"hosts"`
		}
		if err := json.Unmarshal(data, &legacy); err != nil {
			return nil, 0, errors.New("legacy Herdr hosts file is malformed")
		}
		for _, e := range legacy.Hosts {
			profiles = append(profiles, LegacyProfile(e))
		}
	case HostStoreVersion:
		var shape map[string]json.RawMessage
		if err := json.Unmarshal(data, &shape); err != nil || shape == nil || len(bytes.TrimSpace(shape["hosts"])) == 0 || bytes.TrimSpace(shape["hosts"])[0] != '[' {
			return nil, version.Version, errors.New("Herdr hosts file requires a non-null hosts array; preserve it before repair")
		}
		var doc hostDocument
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&doc); err != nil {
			return nil, version.Version, errors.New("Herdr hosts file has unsupported or malformed fields")
		}
		profiles = doc.Hosts
	default:
		return nil, version.Version, fmt.Errorf("unsupported Herdr hosts version %d", version.Version)
	}
	if len(profiles) > MaxInventoryHosts {
		return nil, version.Version, errors.New("too many saved hosts")
	}
	seen := map[string]bool{}
	for _, p := range profiles {
		if err := ValidateProfile(p); err != nil {
			return nil, version.Version, err
		}
		if seen[p.ID] {
			return nil, version.Version, errors.New("duplicate saved host ID")
		}
		seen[p.ID] = true
		if version.Version == HostStoreVersion {
			revision, err := hex.DecodeString(p.ConnectivityRevision)
			if err != nil || (len(revision) != 16 && len(revision) != 32) {
				return nil, version.Version, errors.New("saved host connectivity revision is invalid")
			}
			if p.Target != p.Destination() {
				return nil, version.Version, errors.New("saved host projection does not match its connection")
			}
		}
	}
	return profiles, version.Version, nil
}
func (s *Store) ListProfiles() ([]SavedHost, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return []SavedHost{}, nil
	}
	if err != nil {
		return nil, err
	}
	profiles, _, err := decodeProfiles(data)
	return profiles, err
}
func (s *Store) Resolve(id string) (SavedHost, error) {
	profiles, err := s.ListProfiles()
	if err != nil {
		return SavedHost{}, err
	}
	for _, p := range profiles {
		if p.ID == id {
			return p, nil
		}
	}
	return SavedHost{}, errors.New("saved SSH host no longer exists; reopen Tatami")
}
func (s *Store) SaveProfiles(profiles []SavedHost) error {
	source, readErr := os.ReadFile(s.path)
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return readErr
	}
	var old []SavedHost
	version := HostStoreVersion
	if readErr == nil {
		var err error
		old, version, err = decodeProfiles(source)
		if err != nil {
			return err
		}
	}
	previous := map[string]SavedHost{}
	for _, p := range old {
		previous[p.ID] = p
	}
	if len(profiles) > MaxInventoryHosts {
		return errors.New("too many saved hosts")
	}
	seen := map[string]bool{}
	normalized := make([]SavedHost, 0, len(profiles))
	for _, p := range profiles {
		var err error
		p, err = NormalizeProfile(p)
		if err != nil {
			return err
		}
		if seen[p.ID] {
			return errors.New("duplicate saved host ID")
		}
		seen[p.ID] = true
		prior, exists := previous[p.ID]
		if exists && prior.Target == p.Target && reflect.DeepEqual(prior.Connection, p.Connection) {
			p.ConnectivityRevision = prior.ConnectivityRevision
		} else {
			p.ConnectivityRevision, err = newRevision()
			if err != nil {
				return err
			}
		}
		normalized = append(normalized, p)
	}
	data, err := json.MarshalIndent(hostDocument{Version: HostStoreVersion, Hosts: normalized}, "", "  ")
	if err != nil {
		return err
	}
	if readErr == nil && version == 0 {
		if err := publishLegacyBackup(s.path, source); err != nil {
			return err
		}
	}
	return atomicWrite(s.path, append(data, '\n'))
}
func (s *Store) List() ([]Endpoint, error) {
	profiles, err := s.ListProfiles()
	if err != nil {
		return nil, err
	}
	out := []Endpoint{LocalEndpoint()}
	for _, p := range profiles {
		e, err := p.Endpoint()
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}

// Compatibility writers preserve existing v2 settings, never reconstruct them from wire data.
func (s *Store) Save(endpoints []Endpoint) error {
	existing, err := s.ListProfiles()
	if err != nil {
		return err
	}
	byID := map[string]SavedHost{}
	for _, p := range existing {
		byID[p.ID] = p
	}
	profiles := make([]SavedHost, 0, len(endpoints))
	for _, e := range endpoints {
		if e.ID == LocalEndpointID {
			continue
		}
		p := LegacyProfile(e)
		if e.Profile != nil {
			p = *e.Profile
			p.ID, p.Label = e.ID, e.Label
			if p.Connection.Mode == ModeLegacy {
				p.Target = e.Target
			}
		} else if old, ok := byID[e.ID]; ok {
			p = old
			p.Label = e.Label
			if p.Connection.Mode == ModeLegacy {
				p.Target = e.Target
			} else if e.Target != p.Destination() {
				return errors.New("structured host destination requires an explicit profile edit")
			}
		}
		profiles = append(profiles, p)
	}
	return s.SaveProfiles(profiles)
}
func (s *Store) Delete(id string) error {
	if id == LocalEndpointID {
		return errors.New("local endpoint cannot be deleted")
	}
	profiles, err := s.ListProfiles()
	if err != nil {
		return err
	}
	out := make([]SavedHost, 0, len(profiles))
	found := false
	for _, p := range profiles {
		if p.ID == id {
			found = true
		} else {
			out = append(out, p)
		}
	}
	if !found {
		return errors.New("saved host not found")
	}
	return s.SaveProfiles(out)
}
func validateLegacyBackup(path string, source []byte) error {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return errors.New("conflicting migration backup; preserve it and resolve its type or permissions before retrying")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	_, version, err := decodeProfiles(data)
	if err != nil || version != 0 || !bytes.Equal(data, source) {
		return errors.New("conflicting or incomplete v1 backup; preserve it and resolve the conflict before retrying migration")
	}
	return nil
}
func publishLegacyBackup(path string, source []byte) error {
	final := path + ".v1.bak"
	if _, err := os.Lstat(final); err == nil {
		return validateLegacyBackup(final, source)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".tmp-herdr-backup-")
	if err != nil {
		return err
	}
	name := temp.Name()
	defer os.Remove(name)
	if err := temp.Chmod(0600); err != nil {
		temp.Close()
		return err
	}
	if _, err := io.Copy(temp, bytes.NewReader(source)); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Link(name, final); err != nil {
		if errors.Is(err, os.ErrExist) {
			return validateLegacyBackup(final, source)
		}
		return errors.New("cannot publish completed v1 backup; host configuration was not replaced")
	}
	return syncDirectory(filepath.Dir(path))
}
func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
