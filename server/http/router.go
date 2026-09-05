// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package server

import (
	"errors"
	"fmt"
	"net/http"
	"path"
	"strings"
	"sync"

	"github.com/go-json-experiment/json"

	apitypes "github.com/luxfi/api/types"
	"github.com/luxfi/constants"
	"github.com/luxfi/math/set"
)

const HTTPHeaderRoute = apitypes.HTTPHeaderRoute

var (
	errUnknownBaseURL  = errors.New("unknown base url")
	errUnknownEndpoint = errors.New("unknown endpoint")
	errAlreadyReserved = errors.New("route is either already aliased or already maps to a handle")
	errAnotherNetwork  = errors.New("chain alias belongs to another network")
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
	lock sync.RWMutex

	routeLock      sync.Mutex
	reservedRoutes set.Set[string]     // Reserves routes so that there can't be alias that conflict
	aliases        map[string][]string // Maps a route to a set of reserved routes
	// headerRoutes contains routes based on http headers
	// aliasing is not currently supported
	headerRoutes map[string]http.Handler
	// legacy url-based routing
	routes map[string]map[string]http.Handler // Maps routes to a handler
	// paths maps a full url (base+endpoint) to the handler registered at
	// exactly that path. Every endpoint appears here.
	paths map[string]http.Handler
	// mounts maps a full endpoint url (base+endpoint) to its handler. A base is
	// a namespace shared by sibling endpoints; an endpoint is a mount, and it
	// owns the paths beneath it. Only non-empty endpoints appear here.
	mounts map[string]http.Handler
	// chainSpelling maps a chain alias folded to lower case back to the
	// spelling it registered under, so a caller who wrote the alias in another
	// case can be sent to the one place it lives. Filled where routes are, so
	// it cannot drift from them.
	chainSpelling map[string]string

	// net is the network this node runs, and so which chain aliases are its
	// own. Held here because the router is where a route comes into being: a
	// name this network does not own never becomes a route, and the 404 then
	// falls out of dispatch rather than being a second rule that could
	// disagree with the first.
	net Network

	// rootInfoProvider provides node information for GET /
	rootInfoProvider RootInfoProvider
}

func newRouter(net Network) *router {
	return &router{
		reservedRoutes: make(set.Set[string]),
		aliases:        make(map[string][]string),
		headerRoutes:   make(map[string]http.Handler),
		routes:         make(map[string]map[string]http.Handler),
		paths:          make(map[string]http.Handler),
		mounts:         make(map[string]http.Handler),
		chainSpelling:  make(map[string]string),
		net:            net,
	}
}

// chainPrefix is what a path that names a chain starts with.
var chainPrefix = baseURL + "/" + constants.ChainAliasPrefix + "/"

// rpcEndpoint is the endpoint a chain's JSON-RPC registers at, and the one a
// caller may leave off.
const rpcEndpoint = "/rpc"

// spellings lists the other paths a request could have meant, in the order they
// are tried. THE one place that decides `/v1/chain/C/rpc`, `/v1/chain/c/rpc`,
// `/v1/chain/C` and `/v1/chain/c` are one route rather than four: the alias is
// matched without regard to case, and the `/rpc` a directory listing once told
// a client to write is optional. Only the alias is the caller's to spell — the
// words around it are literals — so nothing else is folded.
//
// A path naming no chain this node serves has no other spelling and is left
// exactly as the client wrote it.
func (r *router) spellings(p string) []string {
	alias, endpoint, isChain := chainNamed(p)
	if !isChain {
		return nil
	}

	r.routeLock.Lock()
	registered, known := r.chainSpelling[strings.ToLower(alias)]
	r.routeLock.Unlock()
	if !known {
		return nil
	}

	named := chainPrefix + registered
	if endpoint != "" {
		named += "/" + endpoint
		if named == p {
			return nil
		}
		return []string{named}
	}
	// No endpoint named: the chain itself, then its RPC. Trying the chain
	// first means a VM that answers at its own root keeps doing so.
	if named == p {
		return []string{named + rpcEndpoint}
	}
	return []string{named, named + rpcEndpoint}
}

// chainNamed is the chain a path names and what it asks of that chain. One
// parse, read both when a route is created and when a request is dispatched,
// so what a node may register and what it will answer cannot drift apart.
func chainNamed(p string) (alias, endpoint string, ok bool) {
	rest, isChain := strings.CutPrefix(p, chainPrefix)
	if !isChain {
		return "", "", false
	}
	alias, endpoint, _ = strings.Cut(rest, "/")
	return alias, endpoint, alias != ""
}

// serves reports whether a chain answers here at all, whichever endpoint it
// hangs its handlers on.
func (r *router) serves(alias string) bool {
	r.routeLock.Lock()
	defer r.routeLock.Unlock()
	_, ok := r.chainSpelling[strings.ToLower(alias)]
	return ok
}

// serveBelowMount answers a request that matched no exact route by handing it
// to the endpoint it lives under, with the mount stripped off.
//
// A VM names the paths it serves relative to its own mount — zkvm's
// /getStatus, aivm's /providers — because it cannot know the chain id the node
// will mount it at. Mounted as a leaf, the handler was only ever reached at the
// mount path itself, read that absolute path as its own, matched none of its
// routes and answered 404. Z-Chain and A-Chain ran, finalized blocks and
// reported metrics while serving nothing.
//
// Exact routes are matched first by construction, so an endpoint can never
// shadow a sibling.
func (r *router) serveBelowMount(writer http.ResponseWriter, request *http.Request) {
	if handler, mount, ok := r.mountFor(request.URL.Path); ok {
		http.StripPrefix(mount, handler).ServeHTTP(writer, request)
		return
	}
	http.NotFound(writer, request)
}

// handlerAt is the route registered at exactly this path.
func (r *router) handlerAt(p string) (http.Handler, bool) {
	r.routeLock.Lock()
	defer r.routeLock.Unlock()
	handler, ok := r.paths[p]
	return handler, ok
}

// mountFor is the endpoint a path lives under, and the prefix to strip to reach
// it. The longest mount wins, so an endpoint never shadows a sibling.
func (r *router) mountFor(p string) (http.Handler, string, bool) {
	for i := len(p) - 1; i > 0; i-- {
		if p[i] != '/' {
			continue
		}
		mount := p[:i]
		r.routeLock.Lock()
		handler, ok := r.mounts[mount]
		r.routeLock.Unlock()
		if ok {
			return handler, mount, true
		}
	}
	return nil, "", false
}

// servePath dispatches on the URL: the canonical spelling of the path, then
// the route registered at exactly that path, then the endpoint it lives under.
//
// A doubled separator or a dot segment names a place the client could have
// named plainly, so it is sent to the plain name rather than served at two
// addresses. A trailing slash is not noise and is kept — /rpc is the mount
// and /rpc/ is the root beneath it, which are two different requests.
func (r *router) servePath(writer http.ResponseWriter, request *http.Request) {
	if clean := canonical(request.URL.Path); clean != request.URL.Path {
		here := *request.URL
		here.Path = clean
		writer.Header().Set("Location", here.String())
		writer.WriteHeader(http.StatusMovedPermanently)
		return
	}

	if handler, ok := r.handlerAt(request.URL.Path); ok {
		handler.ServeHTTP(writer, request)
		return
	}

	// The same chain, spelled another way. The request is REWRITTEN to the
	// spelling the chain registered and then takes the SAME two steps — the
	// exact route, then the endpoint it lives under — so a mounted VM reads
	// the path it was mounted at rather than the one the caller typed.
	for _, spelling := range r.spellings(request.URL.Path) {
		named := request.Clone(request.Context())
		named.URL.Path = spelling
		if handler, ok := r.handlerAt(spelling); ok {
			handler.ServeHTTP(writer, named)
			return
		}
		if handler, mount, ok := r.mountFor(spelling); ok {
			http.StripPrefix(mount, handler).ServeHTTP(writer, named)
			return
		}
	}

	r.serveBelowMount(writer, request)
}

// canonical is path.Clean with the trailing slash left on.
func canonical(p string) string {
	if p == "" {
		return "/"
	}
	if p[0] != '/' {
		p = "/" + p
	}
	clean := path.Clean(p)
	if clean != "/" && p[len(p)-1] == '/' {
		clean += "/"
	}
	return clean
}

func (r *router) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	r.lock.RLock()
	defer r.lock.RUnlock()

	// Handle root "/" specially for EVM compatibility
	if request.URL.Path == "/" {
		r.handleRoot(writer, request)
		return
	}

	// /healthz — the platform-standard liveness address, and an alias for
	// /v1/health/ops/liveness rather than an answer of its own. A probe that
	// computes its own verdict is a probe that can disagree with the node.
	if request.URL.Path == "/healthz" {
		alias := request.Clone(request.Context())
		alias.URL.Path = baseURL + "/health" + Ops + "/liveness"
		r.servePath(writer, alias)
		return
	}

	route, ok := request.Header[HTTPHeaderRoute]
	if !ok {
		// If there is no routing header, fall-back to the legacy path-based
		// routing
		r.servePath(writer, request)
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
		own := Chain("", r.net.Alias())
		info = RootInfo{
			Ready: true,
			Chains: struct {
				C string `json:"c"`
				P string `json:"p"`
				X string `json:"x"`
			}{
				C: own + rpcEndpoint,
				P: Chain("", "P"),
				X: Chain("", "X"),
			},
			Endpoints: struct {
				RPC       string `json:"rpc"`
				Websocket string `json:"ws"`
				Info      string `json:"info"`
				Health    string `json:"health"`
			}{
				RPC:       own + rpcEndpoint,
				Websocket: own + "/ws",
				Info:      baseURL + "/info",
				Health:    baseURL + "/health",
			},
		}
	}

	if err := json.MarshalWrite(w, info); err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
	}
}

// handleRootPOST serves this network's OWN chain, so a wallet pointed at the
// bare host reaches the EVM this node runs rather than one named for a network
// it is not on.
//
// The request is rewritten and dispatched, not looked up: a chain that answers
// at its own root and one that answers under /rpc are both reached that way,
// and by the same code that reaches them when the caller types the path out.
func (r *router) handleRootPOST(w http.ResponseWriter, req *http.Request) {
	if !r.serves(r.net.Alias()) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":null,"error":{"code":-32603,"message":"chain not available"}}`))
		return
	}

	own := req.Clone(req.Context())
	own.URL.Path = Chain("", r.net.Alias())
	r.servePath(w, own)
}

// SetRootInfoProvider sets the provider for root endpoint information
func (r *router) SetRootInfoProvider(provider RootInfoProvider) {
	r.lock.Lock()
	defer r.lock.Unlock()
	r.rootInfoProvider = provider
}

// GetHandler is the handler registered at one base and endpoint.
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
	// A chain named for another network never becomes a route here. This is
	// the single write path — every alias, and every alias of an alias,
	// recurses back through it — so refusing here is refusing everywhere, and
	// no read afterwards has to know the rule. Checked before anything is
	// written, so a refusal leaves nothing behind.
	alias, _, isChain := chainNamed(base)
	if isChain && !r.net.Owns(alias) {
		return fmt.Errorf("%w: %s", errAnotherNetwork, alias)
	}

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
	if endpoint != "" {
		r.mounts[url] = handler
	}
	// A base and an endpoint can concatenate to a url another pair already
	// holds; the pair that got there first keeps it.
	if _, taken := r.paths[url]; !taken {
		r.paths[url] = handler
	}
	// Remember how this chain spelled its alias, so a caller who folds it can
	// be sent here. Recorded beside the path it belongs to — the two are
	// written in one place and cannot disagree.
	if isChain {
		if _, taken := r.chainSpelling[strings.ToLower(alias)]; !taken {
			r.chainSpelling[strings.ToLower(alias)] = alias
		}
	}

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

	// Refused before anything is recorded. An alias is remembered here and
	// applied whenever the chain it names registers a route, so an alias kept
	// for a chain that may never be served is an error that repeats for the
	// life of the node rather than one that happens once.
	for _, alias := range aliases {
		if r.reservedRoutes.Contains(alias) {
			return fmt.Errorf("%w: %s", errAlreadyReserved, alias)
		}
		if name, _, isChain := chainNamed(alias); isChain && !r.net.Owns(name) {
			return fmt.Errorf("%w: %s", errAnotherNetwork, name)
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
