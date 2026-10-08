package syncer

import (
	"strings"

	"github.com/go-git/go-git/v5/plumbing/transport"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	gitssh "github.com/go-git/go-git/v5/plumbing/transport/ssh"
	gossh "golang.org/x/crypto/ssh"

	"git-suite/git-sync/internal/codes"
)

func buildAuth(loc Location, opt Options) (transport.AuthMethod, error) {
	hasSSH := len(opt.SSHKey) > 0
	hasHTTP := opt.Username != "" || opt.Password != ""
	hasKnown := opt.KnownHosts != ""
	if hasSSH && hasHTTP {
		return nil, codes.New(codes.BadCredentials, "pass HTTP credentials or an SSH key")
	}
	if opt.InsecureSkipHostKey && hasKnown {
		return nil, codes.New(codes.BadCredentials, "pass a known_hosts file or insecure-skip-host-key")
	}

	switch loc.Scheme {
	case "file":
		if hasSSH || hasHTTP || opt.InsecureSkipHostKey || hasKnown {
			return nil, codes.New(codes.BadCredentials, "file URLs do not accept credentials")
		}
		return nil, nil
	case "http", "https":
		if hasSSH || opt.InsecureSkipHostKey || hasKnown {
			return nil, codes.New(codes.BadCredentials, "HTTP URLs do not accept SSH settings")
		}
		if !hasHTTP {
			return nil, nil
		}
		if opt.Username == "" || opt.Password == "" {
			return nil, codes.New(codes.BadCredentials, "HTTP authentication requires both username and password")
		}
		return &githttp.BasicAuth{Username: opt.Username, Password: opt.Password}, nil
	case "ssh":
		if hasHTTP {
			return nil, codes.New(codes.BadCredentials, "SSH URLs do not accept a username and password")
		}
		if !hasSSH {
			return nil, codes.New(codes.BadCredentials, "SSH URLs require a private key")
		}
		if !opt.InsecureSkipHostKey && !hasKnown {
			return nil, codes.New(codes.HostKeyConfig, "SSH requires a known_hosts file or insecure-skip-host-key")
		}
		user := loc.User
		if user == "" {
			user = "git"
		}
		keys, err := gitssh.NewPublicKeys(user, opt.SSHKey, opt.SSHPassphrase)
		if err != nil {
			return nil, codes.New(codes.SSHKey, "cannot parse the SSH private key: %s", redact(err.Error(), opt.SSHPassphrase))
		}
		if opt.InsecureSkipHostKey {
			keys.HostKeyCallback = gossh.InsecureIgnoreHostKey()
			return keys, nil
		}
		callback, err := gitssh.NewKnownHostsCallback(opt.KnownHosts)
		if err != nil {
			return nil, codes.New(codes.HostKeyConfig, "cannot read known_hosts: %s", err.Error())
		}
		keys.HostKeyCallback = callback
		return keys, nil
	default:
		return nil, codes.New(codes.InvalidURL, "unsupported repository URL scheme %q", loc.Scheme)
	}
}

func redact(msg string, secrets ...string) string {
	for _, secret := range secrets {
		if len(secret) < 4 {
			continue
		}
		msg = strings.ReplaceAll(msg, secret, "***")
	}
	return msg
}
