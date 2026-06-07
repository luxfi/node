// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package server

import (
	"errors"
	"fmt"
	"net/http"
	"sync"

	"github.com/go-json-experiment/json"
	"github.com/gorilla/mux"

	apitypes "github.com/luxfi/api/types"
	"github.com/luxfi/math/set"
)

const HTTPHeaderRoute = apitypes.HTTPHeaderRoute

var (
	errUnknownBaseURL  = errors.New("unknown base url")
	errUnknownEndpoint = errors.New("unknown endpoint")
	errAlreadyReserved = errors.New("route is either already aliased or already maps to a handle")
)

// RootInfo contains information returned at the root endpoint
type RootInfo struct {
	NodeID    string `json:"nodeId,omitempty"`
	NetworkID uint32 `json:"networkId,omitempty"`
	Version   string `json:"version,omitempty"`
	Ready     bool   `json:"ready"`
	Chains    struct {
		C string `json:"c"`
		P string `json:"p"`
		X string `json:"x"`
	} `json:"chains"`
	Endpoints struct {
		RPC       string `json:"rpc"`
		Websocket string `json:"ws"`
		Info      string `json:"info"`
		Health    string `json:"health"`
	} `json:"endpoints"`
}

// RootInfoProvider provides node information for the root endpoint
type RootInfoProvider interface {
	GetRootInfo() RootInfo
}

type router struct {
	lock   sync.RWMutex
	router *mux.Router

	routeLock      sync.Mutex
	reservedRoutes set.Set[string]     // Reserves routes so that there can't be alias that conflict
	aliases        map[string][]string // Maps a route to a set of reserved routes
	// headerRoutes contains routes based on http headers
	// aliasing is not currently supported
	headerRoutes map[string]http.Handler
	// legacy url-based routing
	routes map[string]map[string]http.Handler // Maps routes to a handler

	// rootInfoProvider provides node information for GET /
	rootInfoProvider RootInfoProvider
}

func newRouter() *router {
	return &router{
		router:         mux.NewRouter(),
		reservedRoutes: make(set.Set[string]),
		aliases:        make(map[string][]string),
		headerRoutes:   make(map[string]http.Handler),
		routes:         make(map[string]map[string]http.Handler),
	}
}

func (r *router) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	r.lock.RLock()
	defer r.lock.RUnlock()

	// Handle root "/" specially for EVM compatibility
	if request.URL.Path == "/" {
		r.handleRoot(writer, request)
		return
	}

	// /healthz — platform-standard health probe (K8s liveness/readiness)
	if request.URL.Path == "/healthz" {
		r.handleHealthz(writer, request)
		return
	}

	route, ok := request.Header[HTTPHeaderRoute]
	if !ok {
		// If there is no routing header, fall-back to the legacy path-based
		// routing
		r.router.ServeHTTP(writer, request)
		return
	}

	// Request specified the routing header key but did not provide a
	// corresponding value
	if len(route) != 1 {
		writer.WriteHeader(http.StatusBadRequest)
		return
	}

	handler, ok := r.headerRoutes[route[0]]
	if !ok {
		writer.WriteHeader(http.StatusNotFound)
		return
	}

	handler.ServeHTTP(writer, request)
}

// handleRoot handles requests to "/" - GET returns node info, POST proxies to C-chain RPC
func (r *router) handleRoot(w http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case http.MethodGet:
		r.handleRootGET(w, req)
	case http.MethodPost:
		r.handleRootPOST(w, req)
	case http.MethodOptions:
		// CORS preflight
		w.Header().Set("Allow", "GET, POST, OPTIONS")
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "Method not allowed. Use GET for node info or POST for EVM JSON-RPC.", http.StatusMethodNotAllowed)
	}
}

// handleRootGET returns node information as JSON
func (r *router) handleRootGET(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var info RootInfo
	if r.rootInfoProvider != nil {
		info = r.rootInfoProvider.GetRootInfo()
	} else {
		// Default info when provider not set
		info = RootInfo{
			Ready: true,
			Chains: struct {
				C string `json:"c"`
				P string `json:"p"`
				X string `json:"x"`
			}{
				C: "/ext/bc/C/rpc",
				P: "/ext/bc/P",
				X: "/ext/bc/X",
			},
			Endpoints: struct {
				RPC       string `json:"rpc"`
				Websocket string `json:"ws"`
				Info      string `json:"info"`
				Health    string `json:"health"`
			}{
				RPC:       "/ext/bc/C/rpc",
				Websocket: "/ext/bc/C/ws",
				Info:      "/ext/info",
				Health:    "/ext/health",
			},
		}
	}

	if err := json.MarshalWrite(w, info); err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
	}
}

// handleRootPOST proxies JSON-RPC requests to the C-chain
func (r *router) handleRootPOST(w http.ResponseWriter, req *http.Request) {
	// Look up the C-chain RPC handler
	handler, err := r.GetHandler("/ext/bc/C", "/rpc")
	if err != nil {
		// Try alternate path formats
		handler, err = r.GetHandler("/ext/bc/C/rpc", "")
		if err != nil {
			// Return proper JSON-RPC error
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":null,"error":{"code":-32603,"message":"C-chain not available"}}`))
			return
		}
	}

	// Forward the request to the C-chain handler
	handler.ServeHTTP(w, req)
}

// SetRootInfoProvider sets the provider for root endpoint information
func (r *router) SetRootInfoProvider(provider RootInfoProvider) {
	r.lock.Lock()
	defer r.lock.Unlock()
	r.rootInfoProvider = provider
}

// handleHealthz returns a minimal health response for K8s probes.
// This delegates to the full /ext/health handler when available,
// falling back to a static 200 response during early startup.
func (r *router) handleHealthz(w http.ResponseWriter, req *http.Request) {
	if handler, err := r.GetHandler("/ext/health", "/health"); err == nil {
		handler.ServeHTTP(w, req)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

func (r *router) GetHandler(base, endpoint string) (http.Handler, error) {
	r.routeLock.Lock()
	defer r.routeLock.Unlock()

	urlBase, exists := r.routes[base]
	if !exists {
		return nil, errUnknownBaseURL
	}
	handler, exists := urlBase[endpoint]
	if !exists {
		return nil, errUnknownEndpoint
	}
	return handler, nil
}

func (r *router) AddHeaderRoute(route string, handler http.Handler) bool {
	r.lock.Lock()
	defer r.lock.Unlock()

	_, ok := r.headerRoutes[route]
	if ok {
		return false
	}

	r.headerRoutes[route] = handler
	return true
}

func (r *router) AddRouter(base, endpoint string, handler http.Handler) error {
	r.lock.Lock()
	defer r.lock.Unlock()
	r.routeLock.Lock()
	defer r.routeLock.Unlock()

	return r.addRouter(base, endpoint, handler)
}

func (r *router) addRouter(base, endpoint string, handler http.Handler) error {
	if r.reservedRoutes.Contains(base) {
		return fmt.Errorf("%w: %s", errAlreadyReserved, base)
	}

	return r.forceAddRouter(base, endpoint, handler)
}

func (r *router) forceAddRouter(base, endpoint string, handler http.Handler) error {
	endpoints := r.routes[base]
	if endpoints == nil {
		endpoints = make(map[string]http.Handler)
	}
	url := base + endpoint
	if _, exists := endpoints[endpoint]; exists {
		return fmt.Errorf("failed to create endpoint as %s already exists", url)
	}

	endpoints[endpoint] = handler
	r.routes[base] = endpoints

	// Name routes based on their URL for easy retrieval in the future
	route := r.router.Handle(url, handler)
	if route == nil {
		return fmt.Errorf("failed to create new route for %s", url)
	}
	route.Name(url)

	var err error
	if aliases, exists := r.aliases[base]; exists {
		for _, alias := range aliases {
			if innerErr := r.forceAddRouter(alias, endpoint, handler); err == nil {
				err = innerErr
			}
		}
	}
	return err
}

func (r *router) AddAlias(base string, aliases ...string) error {
	r.lock.Lock()
	defer r.lock.Unlock()
	r.routeLock.Lock()
	defer r.routeLock.Unlock()

	for _, alias := range aliases {
		if r.reservedRoutes.Contains(alias) {
			return fmt.Errorf("%w: %s", errAlreadyReserved, alias)
		}
	}

	for _, alias := range aliases {
		r.reservedRoutes.Add(alias)
	}

	r.aliases[base] = append(r.aliases[base], aliases...)

	var err error
	if endpoints, exists := r.routes[base]; exists {
		for endpoint, handler := range endpoints {
			for _, alias := range aliases {
				if innerErr := r.forceAddRouter(alias, endpoint, handler); err == nil {
					err = innerErr
				}
			}
		}
	}
	return err
}
