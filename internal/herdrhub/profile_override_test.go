package herdrhub

import "testing"

func TestExecutableOverrideApprovedAbsoluteGrammar(t *testing.T) {
	for _, path := range []string{"/", "tatami", "~/bin/tatami", "/tmp/my tool", "/tmp/tool;marker", "/tmp/tool\narg"} {
		p := SavedHost{ID: "box", Label: "Box", Connection: HostConnection{Mode: ModeExplicit, Hostname: "box", TatamiExecutable: path}}
		if err := ValidateProfile(p); err == nil {
			t.Errorf("unsupported override accepted: %q", path)
		}
	}
	p := SavedHost{ID: "box", Label: "Box", Connection: HostConnection{Mode: ModeExplicit, Hostname: "box", TatamiExecutable: "/opt/bin/tatami-v2_+~"}}
	if err := ValidateProfile(p); err != nil {
		t.Fatal(err)
	}
}
