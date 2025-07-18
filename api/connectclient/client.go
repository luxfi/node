
package connectclient

import (
  "context"

  "connectrpc.com/connect"

  "github.com/luxfi/node/api/server"
)

var _ connect.Interceptor = (*SetRouteHeaderInterceptor)(nil)

// SetRouteHeaderInterceptor sets the api routing header for connect-rpc requests
type SetRouteHeaderInterceptor struct {
  Route string
}

func (s SetRouteHeaderInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
  return func(ctx context.Context, request connect.AnyRequest) (connect.AnyResponse, error) {
    request.Header().Set(server.HTTPHeaderRoute, s.Route)
    return next(ctx, request)
  }
}

func (s SetRouteHeaderInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
  return func(ctx context.Context, spec connect.Spec) connect.StreamingClientConn {
    conn := next(ctx, spec)
    conn.RequestHeader().Set(server.HTTPHeaderRoute, s.Route)
    return conn
  }
}

func (SetRouteHeaderInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
  return next
}
