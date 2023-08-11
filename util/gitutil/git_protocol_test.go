package gitutil

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseProtocol(t *testing.T) {
	tests := []struct {
		url      string
		protocol string
		remote   string
		path     string
	}{
		{
			url:      "http://github.com/moby/buildkit",
			protocol: HTTPProtocol,
			remote:   "github.com",
			path:     "moby/buildkit",
		},
		{
			url:      "https://github.com/moby/buildkit",
			protocol: HTTPSProtocol,
			remote:   "github.com",
			path:     "moby/buildkit",
		},
		{
			url:      "http://github.com/moby/buildkit#v1.0.0",
			protocol: HTTPProtocol,
			remote:   "github.com",
			path:     "moby/buildkit#v1.0.0",
		},
		{
			url:      "http://foo:bar@github.com/moby/buildkit#v1.0.0",
			protocol: HTTPProtocol,
			remote:   "foo:bar@github.com",
			path:     "moby/buildkit#v1.0.0",
		},
		{
			url:      "ssh://git@github.com/moby/buildkit.git",
			protocol: SSHProtocol,
			remote:   "git@github.com",
			path:     "moby/buildkit.git",
		},
		{
			url:      "ssh://git@github.com:22/moby/buildkit.git",
			protocol: SSHProtocol,
			remote:   "git@github.com:22",
			path:     "moby/buildkit.git",
		},
		{
			url:      "git@github.com:moby/buildkit.git",
			protocol: SSHProtocol,
			remote:   "git@github.com",
			path:     "moby/buildkit.git",
		},
		{
			url:      "git@github.com:moby/buildkit.git#v1.0.0",
			protocol: SSHProtocol,
			remote:   "git@github.com",
			path:     "moby/buildkit.git#v1.0.0",
		},
		{
			url:      "nonstandarduser@example.com:/srv/repos/weird/project.git",
			protocol: SSHProtocol,
			remote:   "nonstandarduser@example.com",
			path:     "/srv/repos/weird/project.git",
		},
		{
			url:      "ssh://root@subdomain.example.hostname:2222/root/my/really/weird/path/foo.git",
			protocol: SSHProtocol,
			remote:   "root@subdomain.example.hostname:2222",
			path:     "root/my/really/weird/path/foo.git",
		},
		{
			url:      "git://host.xz:1234/path/to/repo.git",
			protocol: GitProtocol,
			remote:   "host.xz:1234",
			path:     "path/to/repo.git",
		},
	}
	for _, test := range tests {
		t.Run(test.url, func(t *testing.T) {
			protocol, remote, path := ParseProtocol(test.url)
			require.Equal(t, test.protocol, protocol)
			require.Equal(t, test.remote, remote)
			require.Equal(t, test.path, path)
		})
	}
}
