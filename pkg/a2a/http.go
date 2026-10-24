package a2a

import (
	"context"
	"net/http"
	"net/url"

	sdk "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
)

// Authenticate resolves a stable peer identity. Use credential verification for
// authenticated deployments, or TrustedPeerIdentity inside a trusted network.
// Never derive identity from body fields, contextId, or taskId.
type Authenticate func(*http.Request) (string, error)

const CorePath = "/a2a"

// Advertise adds the optional tape-view extension without replacing core SDK
// types or changing other capabilities. The endpoint must be a Connect service URL.
func Advertise(card *sdk.AgentCard, endpoint string) error {
	if card == nil {
		return ErrInvalidParams
	}
	if _, err := connectBaseURL(endpoint); err != nil {
		return err
	}
	for _, e := range card.Capabilities.Extensions {
		if e.URI == ExtensionURI {
			return ErrConflict
		}
	}
	card.Capabilities.Extensions = append(
		card.Capabilities.Extensions,
		sdk.AgentExtension{
			URI:         ExtensionURI,
			Description: "Permissioned borrowing of one tape context range",
			Params: map[string]any{
				"endpoint":        endpoint,
				"entryMediaTypes": []string{TextMediaType},
				"maxPageSize":     MaxPageSize,
			},
		})
	return nil
}

// NewPeerHandler serves the public card and independently authenticated core
// and extension endpoints. SDK handler options still control task lifecycle,
// streaming and custom task stores. Card URLs must match these mount paths.
func NewPeerHandler(card *sdk.AgentCard, core a2asrv.RequestHandler, s *Service, auth Authenticate) (http.Handler, error) {
	if card == nil || core == nil || auth == nil {
		return nil, ErrInvalidParams
	}
	views, err := NewViewHandler(s, auth)
	if err != nil {
		return nil, err
	}
	found := false
	for _, ext := range card.Capabilities.Extensions {
		if ext.URI == ExtensionURI {
			endpoint, ok := ext.Params["endpoint"].(string)
			if !ok {
				return nil, ErrInvalidParams
			}
			if _, err := connectBaseURL(endpoint); err != nil {
				return nil, err
			}
			u, err := url.Parse(endpoint)
			if err != nil {
				return nil, err
			}
			if u.Path != ViewPath {
				return nil, ErrInvalidParams
			}
			found = true
		}
	}
	if !found {
		return nil, ErrInvalidParams
	}
	mux, err := newMessageMux(card, core, auth)
	if err != nil {
		return nil, err
	}
	mux.Handle(ViewPath+"/", views)
	return mux, nil
}

// NewMessageHandler serves discovery and authenticated core A2A messaging
// without advertising or enabling tape borrowing.
func NewMessageHandler(card *sdk.AgentCard, core a2asrv.RequestHandler, auth Authenticate) (http.Handler, error) {
	return newMessageMux(card, core, auth)
}

func newMessageMux(card *sdk.AgentCard, core a2asrv.RequestHandler, auth Authenticate) (*http.ServeMux, error) {
	if card == nil || core == nil || auth == nil {
		return nil, ErrInvalidParams
	}
	mux := http.NewServeMux()
	mux.Handle(a2asrv.WellKnownAgentCardPath, a2asrv.NewStaticAgentCardHandler(card))
	sdkHandler := a2asrv.NewJSONRPCHandler(&a2asrv.InterceptedHandler{Handler: core, Interceptors: []a2asrv.CallInterceptor{peerAuthentication{}}})
	mux.Handle(CorePath, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, err := auth(r)
		if err != nil || p == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		sdkHandler.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), peerKey{}, p)))
	}))
	return mux, nil
}

type peerKey struct{}

type peerAuthentication struct {
	a2asrv.PassthroughCallInterceptor
}

func (peerAuthentication) Before(ctx context.Context, call *a2asrv.CallContext, _ *a2asrv.Request) (context.Context, any, error) {
	p, ok := ctx.Value(peerKey{}).(string)
	if !ok || p == "" {
		return ctx, nil, sdk.ErrUnauthenticated
	}
	call.User = a2asrv.NewAuthenticatedUser(p, nil)
	return ctx, nil, nil
}
