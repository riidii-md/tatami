package sshconn

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSharedCommandsDisableConfiguredAgentForwarding(t *testing.T) {
	if _, err := exec.LookPath("ssh"); err != nil {
		t.Skip("OpenSSH unavailable")
	}
	config := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(config, []byte("Host *\n ForwardAgent yes\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, op := range []Operation{Background, Interactive, Attach} {
		cmd, err := Build(Connection{Destination: "example.test"}, op, "true")
		if err != nil {
			t.Fatal(err)
		}
		cmd.Args = append([]string{"-F", config, "-G"}, cmd.Args...)
		for _, rendered := range []bool{false, true} {
			var out []byte
			if rendered {
				out, err = exec.Command("/bin/sh", "-c", RenderPOSIX(cmd)).CombinedOutput()
			} else {
				out, err = exec.Command(cmd.Executable, cmd.Args...).CombinedOutput()
			}
			if err != nil {
				t.Fatalf("%v %s", err, out)
			}
			if !strings.Contains(string(out), "forwardagent no\n") {
				t.Fatalf("operation %v rendered=%v forwarding not disabled: %s", op, rendered, out)
			}
		}
	}
}
