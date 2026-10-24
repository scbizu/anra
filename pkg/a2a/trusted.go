package a2a

import (
	"errors"
	"net/http"
	"regexp"
)

// PeerIdentityHeader carries the caller's stable agent identity inside a trusted
// deployment. It is an assertion, not a credential or proof of membership.
const PeerIdentityHeader = "Anra-Agent"

var peerIdentity = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// TrustedPeerIdentity resolves an identity supplied by a trusted network member.
// Both the registry and peer endpoints must be restricted to trusted members;
// callers are trusted not to impersonate one another.
func TrustedPeerIdentity(req *http.Request) (string, error) {
	values := req.Header.Values(PeerIdentityHeader)
	if len(values) != 1 || !peerIdentity.MatchString(values[0]) {
		return "", errors.New("a valid Anra-Agent identity is required")
	}
	return values[0], nil
}

// TrustedHTTPClient identifies this agent on peer requests without shared secrets.
func TrustedHTTPClient(agent string) *http.Client {
	return &http.Client{Transport: identityTransport{agent: agent}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

type identityTransport struct{ agent string }

func (t identityTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.Header.Set(PeerIdentityHeader, t.agent)
	return http.DefaultTransport.RoundTrip(clone)
}
