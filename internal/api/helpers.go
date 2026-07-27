package api

import (
	"net"
	"net/http"

	"github.com/hubchat/hubchat/internal/authorization"
)

// actorFromRequest reads the actor requireActor attached to the context. Only
// safe to call from a handler mounted behind requireActor/requireCapability.
func actorFromRequest(r *http.Request) *authorization.Actor {
	return authorization.FromContext(r.Context())
}

func splitHostPort(addr string) (string, string, error) {
	return net.SplitHostPort(addr)
}
