package group

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/go-hclog"
	"gitlab.com/gitlab-org/fleeting/fleeting/provider"
	"golang.org/x/crypto/ssh"
)

func TestGenerateSSHKey(t *testing.T) {
	authKey, privPEM, err := generateSSHKey()
	if err != nil {
		t.Fatalf("generateSSHKey: %v", err)
	}
	if !strings.HasPrefix(authKey, "ssh-ed25519 ") {
		t.Errorf("public key not in authorized_keys form: %q", authKey)
	}
	// The PEM private key must parse, and its public half must match authKey.
	signer, err := ssh.ParsePrivateKey(privPEM)
	if err != nil {
		t.Fatalf("private key does not parse: %v", err)
	}
	got := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey())))
	if got != authKey {
		t.Errorf("private key's public half %q != advertised %q", got, authKey)
	}
}

func TestInit_GeneratesSSHKeyWhenNotStatic(t *testing.T) {
	g := New(cfg(), &fakeCloud{})
	if _, err := g.Init(context.Background(), hclog.NewNullLogger(), provider.Settings{}); err != nil {
		t.Fatal(err)
	}
	if len(g.SSHKeys) == 0 || !strings.HasPrefix(g.SSHKeys[len(g.SSHKeys)-1], "ssh-ed25519 ") {
		t.Errorf("expected a generated ssh key appended, got %v", g.SSHKeys)
	}
	if len(g.settings.Key) == 0 {
		t.Error("expected the private key set on settings for the connector")
	}
}

func TestInit_StaticCredentialsSkipsKeygen(t *testing.T) {
	g := New(cfg(), &fakeCloud{})
	settings := provider.Settings{}
	settings.UseStaticCredentials = true
	if _, err := g.Init(context.Background(), hclog.NewNullLogger(), settings); err != nil {
		t.Fatal(err)
	}
	if len(g.SSHKeys) != 0 {
		t.Errorf("must not generate a key when static credentials are used: %v", g.SSHKeys)
	}
	if len(g.settings.Key) != 0 {
		t.Error("must not set a private key under static credentials")
	}
}
