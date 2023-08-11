package gitutil

import (
	"net/url"
	"regexp"
	"strings"

	"github.com/moby/buildkit/util/sshutil"
)

const (
	HTTPProtocol    = "http"
	HTTPSProtocol   = "https"
	SSHProtocol     = "ssh"
	GitProtocol     = "git"
	UnknownProtocol = "unknown"
)

var protos = map[string]struct{}{
	"http":  {},
	"https": {},
	"git":   {},
	"ssh":   {},
}

var hasProtoRegexp = regexp.MustCompile(`^[a-z0-9]+://`)

// ParseProtocol parses a git URL and returns the protocol type, the remote,
// and the path.
//
// ParseProtocol understands implicit ssh URLs such as "git@host:repo", and
// returns the same response as if the URL were "ssh://git@host/repo".
func ParseProtocol(remote string) (string, string, string) {
	if hasProtoRegexp.MatchString(remote) {
		u, err := url.Parse(remote)
		if err != nil {
			return remote, "", UnknownProtocol
		}

		proto := u.Scheme
		_, ok := protos[proto]
		if !ok {
			proto = UnknownProtocol
		}

		host := u.Host
		if u.User != nil {
			host = u.User.String() + "@" + host
		}

		path := u.Path
		path = strings.TrimPrefix(path, "/")
		if u.Fragment != "" {
			path += "#" + u.Fragment
		}

		return proto, host, path
	}

	if sshutil.IsImplicitSSHTransport(remote) {
		remote, path, _ := strings.Cut(remote, ":")
		return SSHProtocol, remote, path
	}

	return UnknownProtocol, remote, ""
}
