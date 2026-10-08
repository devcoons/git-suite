package syncer

import "testing"

func TestParseLocation(t *testing.T) {
	https, err := ParseLocation("https://GitHub.com/Org/Repo.git")
	if err != nil {
		t.Fatal(err)
	}
	scp, err := ParseLocation("git@github.com:Org/Repo.git")
	if err != nil {
		t.Fatal(err)
	}
	ssh, err := ParseLocation("ssh://git@github.com:22/Org/Repo")
	if err != nil {
		t.Fatal(err)
	}
	if !https.Identity.Equal(scp.Identity) || !https.Identity.Equal(ssh.Identity) {
		t.Fatalf("equivalent URLs did not match: %+v %+v %+v", https.Identity, scp.Identity, ssh.Identity)
	}
	if https.Identity.Host != "github.com" || https.Identity.Path != "Org/Repo" {
		t.Fatalf("identity = %+v", https.Identity)
	}
	if scp.Scheme != "ssh" || scp.User != "git" {
		t.Fatalf("scp = %+v", scp)
	}

	withPort, err := ParseLocation("ssh://git@github.com:2222/Org/Repo.git")
	if err != nil {
		t.Fatal(err)
	}
	if withPort.Identity.Host != "github.com:2222" || withPort.Identity.Equal(https.Identity) {
		t.Fatalf("port was not kept: %+v", withPort.Identity)
	}

	fileLoc, err := ParseLocation("file://localhost/tmp/repo/")
	if err != nil {
		t.Fatal(err)
	}
	if fileLoc.Identity.Path != "/tmp/repo" || fileLoc.Scheme != "file" {
		t.Fatalf("file location = %+v", fileLoc)
	}
	nested, err := ParseLocation("file:///tmp/../tmp/repo")
	if err != nil {
		t.Fatal(err)
	}
	if !nested.Identity.Equal(fileLoc.Identity) {
		t.Fatalf("cleaned file URL = %+v", nested.Identity)
	}

	rejects := []string{
		"",
		"https://user:secret@github.com/org/repo.git",
		"https://github.com/org/repo.git?ref=1",
		"git://github.com/org/repo.git",
		"not a url",
	}
	for _, raw := range rejects {
		if _, err := ParseLocation(raw); err == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
}

func TestValidateBranch(t *testing.T) {
	if err := ValidateBranch(""); err != nil {
		t.Fatal(err)
	}
	if err := ValidateBranch("feature/name"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"-x", "a..b", "has space", "lock.lock"} {
		if err := ValidateBranch(name); err == nil {
			t.Fatalf("accepted %q", name)
		}
	}
}
