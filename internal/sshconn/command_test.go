package sshconn

import (
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

func TestBuildModes(t *testing.T) {
	c := Connection{Destination: "server", Username: "oles", Port: 2222, Auth: Identity, IdentityFile: "/keys/my key", Jump: []string{"relay"}}
	cmd, err := Build(c, Background, "true")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"-a", "-o", "BatchMode=yes", "-l", "oles", "-p", "2222", "-i", "/keys/my key", "-o", "IdentitiesOnly=yes", "-J", "relay", "--", "server", "true"}
	if !reflect.DeepEqual(cmd.Args, want) {
		t.Fatalf("args=%q want=%q", cmd.Args, want)
	}
	for _, op := range []Operation{Interactive, Attach} {
		got, err := Build(Connection{Destination: "server", Auth: PasswordPrompt}, op, "true")
		if err != nil {
			t.Fatal(err)
		}
		text := strings.Join(got.Args, " ")
		if strings.Contains(text, "BatchMode") || strings.Contains(text, " -A ") {
			t.Fatal(text)
		}
		if strings.Contains(text, "-t") != (op == Attach) {
			t.Fatal(text)
		}
		if !strings.Contains(text, "PubkeyAuthentication=no") {
			t.Fatal(text)
		}
	}
}

func TestRejectUnsafeConnections(t *testing.T) {
	for _, c := range []Connection{
		{Destination: "-oProxyCommand=evil"}, {Destination: "host;evil"}, {Destination: "host\n"},
		{Destination: "host", Port: 65536}, {Destination: "host", Username: "user@other"},
		{Destination: "host", Auth: Certificate, IdentityFile: "key"},
		{Destination: "host", Auth: SecurityKey},
		{Destination: "host", Auth: "unknown"}, {Destination: "host", Jump: []string{"a,b"}},
		{Destination: "ssh://user:secret@host"}, {Destination: "host", IdentityFile: "key\n"},
	} {
		if _, err := Build(c, Background, "true"); err == nil {
			t.Fatalf("accepted %+v", c)
		}
	}
	for _, dest := range []string{"host", "u@host", "ssh://u@host:2222", "ssh://u@[::1]:2222", "::1"} {
		if _, err := Build(Connection{Destination: dest}, Interactive, "true"); err != nil {
			t.Fatalf("%s: %v", dest, err)
		}
	}
}

func TestRenderPOSIXPreservesArguments(t *testing.T) {
	values := []string{"simple", "a b", "a'b", "a\"b", "$HOME", "semi;colon", "back\\slash", ""}
	c := Command{Executable: "printf", Args: append([]string{"%s\\n"}, values...)}
	out, err := exec.Command("/bin/sh", "-c", RenderPOSIX(c)).Output()
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != strings.Join(values, "\n")+"\n" {
		t.Fatalf("got %q", out)
	}
}

func TestCertificateConfigValueEncoding(t *testing.T) {
	if _, err := exec.LookPath("ssh"); err != nil {
		t.Skip("OpenSSH unavailable")
	}
	for _, path := range []string{"/tmp/key cert.pub", "/tmp/key\"cert.pub", "/tmp/key\\cert.pub"} {
		c, err := Build(Connection{Destination: "localhost", Auth: Certificate, IdentityFile: "/tmp/key", CertificateFile: path}, Interactive, "true")
		if err != nil {
			t.Fatal(err)
		}
		c.Args = append([]string{"-F", "/dev/null", "-G"}, c.Args...)
		for _, rendered := range []bool{false, true} {
			var out []byte
			if rendered {
				out, err = exec.Command("/bin/sh", "-c", RenderPOSIX(c)).CombinedOutput()
			} else {
				out, err = exec.Command(c.Executable, c.Args...).CombinedOutput()
			}
			if err != nil {
				t.Fatalf("%s: %v %s", path, err, out)
			}
			if !strings.Contains(string(out), "certificatefile "+path+"\n") {
				t.Fatalf("path changed: %s", out)
			}
		}
	}
}
