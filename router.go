package jmux

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

type contextKeyType string

// ParamsKey is the key used to access the paramters in an http.Request's
// context when an http Handler has been wrapped as a jmux Handler.
const ParamsKey contextKeyType = "jmuxkey"

// Handler handles a request.
type Handler interface {
	ServeC(*Context)
}

// ToHTTP converts a jmux Handler to an http.Handler.
func ToHTTP(h Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ServeC(newContext(w, r, make(map[string]string)))
	})
}

// ToHTTPFunc converts a jmux HandlerFunc to an http.HandlerFunc.
func ToHTTPFunc(h HandlerFunc) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ServeHTTP(w, r)
	})
}

// WrapH wraps an http.Handler as a jmux Handler.
func WrapH(h http.Handler) Handler {
	return HandlerFunc(func(c *Context) {
		r := c.Request.WithContext(
			context.WithValue(c.Request.Context(), ParamsKey, c.Params),
		)
		h.ServeHTTP(c.Writer, r)
	})
}

// WrapF wraps a std handler function as a jmux Handler.
func WrapF(f func(http.ResponseWriter, *http.Request)) Handler {
	return HandlerFunc(func(c *Context) {
		r := c.Request.WithContext(
			context.WithValue(c.Request.Context(), ParamsKey, c.Params),
		)
		f(c.Writer, r)
	})
}

// HandlerFunc is the type for a jmux handler function.
type HandlerFunc func(*Context)

// ServeHTTP implements the ServeHTTP function for the http.Handler interface.
func (h HandlerFunc) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h(newContext(w, r, make(map[string]string)))
}

// ServeC implements the ServeC function for the jmux Handler interface.
func (h HandlerFunc) ServeC(c *Context) {
	h(c)
}

// Route is a route in a router.
type Route struct {
	name    string
	param   bool
	methods Methods
	// Used to match child routes that failed to match
	matchAny map[string]Handler
	routes   map[string]*Route
	handlers map[string]Handler
	parent   *Route
}

// MatchAny allows all of the given methods for the route. This makes the route
// a catch all for any requests that matched this but failed to match children.
// E.g., if this route is 3 in the tree 1/2/3/4/5 and 1/2/3/4/6 is requested,
// as long as 4 doesn't match, it will fall back to this (assuming the method
// is accepted).
// The failed matches use the handler that is associated with the route.
// This function calls Route.MatchAny(methods, nil) under the hood.
// Returns the calling route.
func (route *Route) MatchAny(methods Methods) *Route {
	return route.HandleAny(methods, nil)
}

// HandleAny uses the given handler for all of the given methods. This makes
// the route a catch all for any requests that matched this but failed to match
// children. E.g., if this route is 3 in the tree 1/2/3/4/5 and 1/2/3/4/6 is
// requested, as long as 4 doesn't match, it will fall back to this (assuming
// the method is accepted).
// The failed matches use the handler passed to this function. Passing nil
// causes them to use the route's default handler (same behavior as
// Route.MatchAny).
// Routes with a trailing slash get precendence in the fallback matching over
// those without. E.g., between "/slug1" and "/slug1/", "/slug1/" takes
// precendence when matching something like "/slug1/slug2". Additionally, this
// causes the following to be happen: with a route of "/slug1/" that matches
// any (but not a route of "/slug1"), a request for "/slug1" will fall back to
// "/slug1/". This inverse is not true, however, meaning that with a route of
// "/slug1" that matches any (but not a route of "/slug1/"), a request for
// "/slug1/" will not fall back to "/slug1".
// Returns the calling route.
func (route *Route) HandleAny(methods Methods, h Handler) *Route {
	for method := range methods {
		route.matchAny[method] = h
	}
	return route
}

// HandleAnyFunc uses the given hander func for all of the given methods.
// Returns the calling route.
// Returns the calling route.
func (route *Route) HandleAnyFunc(methods Methods, f HandlerFunc) *Route {
	return route.HandleAny(methods, f)
}

func (route *Route) getHandler(method string) Handler {
	h := route.handlers[method]
	if h == nil {
		return route.handlers[MethodAll]
	}
	return h
}

func (route *Route) getMatchAnyHandler(method string) Handler {
	h, ok := route.matchAny[method]
	if ok && h != nil {
		return h
	}
	h, okAll := route.matchAny[MethodAll]
	if okAll && h != nil {
		return h
	}
	if !ok && !okAll {
		return nil
	}
	return route.getHandler(method)
}

func (route *Route) getParentMatch(method string) Handler {
	if route.name != "/" {
		if r := route.routes["/"]; r != nil {
			h := r.getMatchAnyHandler(method)
			if h != nil {
				return h
			}
		}
	} else {
		route = route.parent
		if route != nil {
			route = route.parent
		}
	}
	for ; route != nil; route = route.parent {
		if handler := route.getMatchAnyHandler(method); handler != nil {
			return handler
		}
	}
	return nil
}

func (route *Route) getRoute(pattern string, methods Methods, h Handler) *Route {
	if pattern == "" {
		for method := range methods {
			route.handlers[method] = h
		}
		return route
	}
	if pattern[0] == '/' {
		pattern = pattern[1:]
	}
	lp := len(pattern)
	l := nextSlug(pattern)
	if l == -1 {
		l = lp
	}
	slug, param := pattern[:l], false
	if slug == "" {
		slug = "/"
	}
	if slug[0] == '{' {
		if slug[l-1] != '}' {
			panic("missing closing brace in pattern: " + pattern)
		}
		slug = slug[1 : l-1]
		param = true
	}
	r, ok := route.routes[slug]
	if !ok {
		r = &Route{
			name:     slug,
			param:    param,
			methods:  CopyMethods(methods),
			matchAny: make(map[string]Handler),
			routes:   make(map[string]*Route),
			handlers: make(map[string]Handler),
			parent:   route,
		}
		route.routes[slug] = r
	} else {
		r.methods.CopyFrom(methods)
	}
	if l == lp {
		return r.getRoute("", methods, h)
	}
	/*
	  // Ends with slash
	  if pattern[lp-1] == '/' {
	    return r.getRoute(pattern[l+1:]+"/", methods, h)
	  }
	*/
	//return r.getRoute(pattern[l+1:], methods, h)
	return r.getRoute(pattern[l:], methods, h)
}

// Router is a router.
type Router struct {
	base *Route
	// map[method]Handler
	defaultHandlers map[string]Handler
	notFoundHandler Handler
}

// NewRouter creates a new router.
func NewRouter() *Router {
	return &Router{
		base: &Route{
			methods:  make(Methods),
			matchAny: make(map[string]Handler),
			routes:   make(map[string]*Route),
			handlers: make(map[string]Handler),
		},
		defaultHandlers: make(map[string]Handler),
		notFoundHandler: HandlerFunc(func(c *Context) {
			c.WriteHeader(http.StatusNotFound)
		}),
	}
}

// Handle handles the given pattern, allowing the given methods, and using the
// given handler. If the pattern is an empty string (""), nothing is done and
// nil is returned.
func (router *Router) Handle(pattern string, methods Methods, h Handler) *Route {
	// NOTE: If adding the functionality below, make sure to move the
	// documentation to the appropriate place.

	/* DOCUMENTATION
	// A pattern with a trailing slash ('/') that is now the base path (just "/")
	// is equivalent to calling MatchAny(methods) on the resulting Route (e.g.,
	// calling Router.Handle("/static/", MethodsGet, HANDLER) is the same as
	// Router.Handle("/static", MethodsGet, HANDLER).MatchAny(MethodsGet)).
	*/

	if pattern == "" {
		return nil
	} else if pattern == "/" {
		for method := range methods {
			router.base.handlers[method] = h
		}
		router.base.methods.CopyFrom(methods)
		return router.base
	}
	if pattern[0] == '/' {
		pattern = pattern[1:]
	}
	/*
		if l1 := len(pattern) - 1; pattern[l1] == '/' {
			pattern = pattern[:l1]
		}
	*/
	return router.base.getRoute(pattern, methods, h)
}

// Get handles the given pattern with the given handler for GET requests.
func (router *Router) Get(pattern string, h Handler) *Route {
	return router.Handle(pattern, MethodsGet(), h)
}

// Post handles the given pattern with the given handler for POST requests.
func (router *Router) Post(pattern string, h Handler) *Route {
	return router.Handle(pattern, MethodsPost(), h)
}

// Put handles the given pattern with the given handler for PUT requests.
func (router *Router) Put(pattern string, h Handler) *Route {
	return router.Handle(pattern, MethodsPut(), h)
}

// Delete handles the given pattern with the given handler for DELETE requests.
func (router *Router) Delete(pattern string, h Handler) *Route {
	return router.Handle(pattern, MethodsDelete(), h)
}

// All handles the given pattern with the given handler for any/all methods.
func (router *Router) All(pattern string, h Handler) *Route {
	return router.Handle(pattern, MethodsAll(), h)
}

// Default sets the default handler for the given methods.
func (router *Router) Default(methods Methods, h Handler) {
	for method := range methods {
		router.defaultHandlers[method] = h
	}
}

// NotFound sets the handler for when a request results in a NotFound. It is
// not required for the handler to actually handle the request with a not found
// response. The default behavior is to just write a NotFound (404) status
// code.
func (router *Router) NotFound(h Handler) {
	if h == nil {
		h = HandlerFunc(func(c *Context) {
			c.WriteHeader(http.StatusNotFound)
		})
	}
	router.notFoundHandler = h
}

// HandleFunc is the same as Handle but takes a HandlerFunc.
func (router *Router) HandleFunc(pattern string, methods Methods, f HandlerFunc) *Route {
	return router.Handle(pattern, methods, f)
}

// GetFunc is the same as Get but takes a HandlerFunc.
func (router *Router) GetFunc(pattern string, f HandlerFunc) *Route {
	return router.HandleFunc(pattern, MethodsGet(), f)
}

// PostFunc is the same as Post but takes a HandlerFunc.
func (router *Router) PostFunc(pattern string, f HandlerFunc) *Route {
	return router.HandleFunc(pattern, MethodsPost(), f)
}

// PutFunc is the same as Put but takes a HandlerFunc.
func (router *Router) PutFunc(pattern string, f HandlerFunc) *Route {
	return router.HandleFunc(pattern, MethodsPut(), f)
}

// DeleteFunc is the same as Delete but takes a HandlerFunc.
func (router *Router) DeleteFunc(pattern string, f HandlerFunc) *Route {
	return router.HandleFunc(pattern, MethodsDelete(), f)
}

// AllFunc is the same as All but takes a HandlerFunc.
func (router *Router) AllFunc(pattern string, f HandlerFunc) *Route {
	return router.HandleFunc(pattern, MethodsAll(), f)
}

// DefaultFunc is the same as Default but takes a HandlerFunc.
func (router *Router) DefaultFunc(methods Methods, f HandlerFunc) {
	router.Default(methods, f)
}

// NotFoundFunc is the same as NotFound but takes a HandlerFunc.
func (router *Router) NotFoundFunc(f HandlerFunc) {
	router.NotFound(f)
}

func (router *Router) getDefaultHandler(method string) Handler {
	h := router.defaultHandlers[method]
	if h == nil {
		return router.defaultHandlers[MethodAll]
	}
	return h
}

// ServeHTTP implements the ServeHTTP function for the http.Handler interface.
func (router *Router) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	urlPath := r.URL.Path
	upl := len(urlPath)
	if upl != 0 && urlPath[0] == '/' {
		urlPath = urlPath[1:]
		upl--
	}
	trailingSlash := upl != 0 && urlPath[upl-1] == '/'
	route, params := router.base, make(map[string]string)
pathLoop:
	for l := nextSlug(urlPath); urlPath != ""; l = nextSlug(urlPath) {
		var slug string
		if l != -1 {
			slug = urlPath[:l]
			urlPath = urlPath[l+1:]
			if trailingSlash && l != 0 && urlPath == "" {
				urlPath = "/"
			}
		} else {
			slug = urlPath
			urlPath = ""
		}
		if slug == "" {
			slug = "/"
		}

		ro := route.routes[slug]
		if ro == nil {
			for _, ro := range route.routes {
				if ro.param && ro.methods.HasOrAll(r.Method) {
					params[ro.name] = slug
					route = ro
					continue pathLoop
				}
			}
			if slug == "/" {
				route = route.parent
			}
			if route != nil {
				if handler := route.getParentMatch(r.Method); handler != nil {
					handler.ServeC(newContext(w, r, params))
					return
				}
			}
			router.serveDefault(w, r)
			return
		}
		route = ro
		if !route.methods.HasOrAll(r.Method) {
			break
		}
		if route.param {
			params[route.name] = slug
		}
	}

	// True if the route doesn't have an associated handler (not an endpoint)
	handler := route.getHandler(r.Method)
	if handler == nil {
		if handler := route.getParentMatch(r.Method); handler != nil {
			handler.ServeC(newContext(w, r, params))
			return
		}
		router.serveDefault(w, r)
		return
	}
	handler.ServeC(newContext(w, r, params))
}

// ServeC implements the ServeC function for the jmux Handler interface.
func (router *Router) ServeC(c *Context) {
	// TODO: Check to make sure things work
	WrapH(router).ServeC(c)
}

func (router *Router) serveDefault(w http.ResponseWriter, r *http.Request) {
	handler := router.getDefaultHandler(r.Method)
	if handler == nil {
		ToHTTP(router.notFoundHandler).ServeHTTP(w, r)
		return
	}
	handler.ServeC(newContext(w, r, make(map[string]string)))
}

func nextSlug(path string) int {
	return strings.IndexByte(path, '/')
}

// Context is what is passed to jmux handlers.
type Context struct {
	// Request is the request.
	Request *http.Request
	// Writer is the response writer associated with the request.
	Writer http.ResponseWriter
	// Params are any path parameters.
	Params map[string]string
}

func newContext(w http.ResponseWriter, r *http.Request, params map[string]string) *Context {
	return &Context{Writer: w, Request: r, Params: params}
}

// Write writes the bytes to the underlying resposne writer.
func (c *Context) Write(p []byte) (int, error) {
	return c.Writer.Write(p)
}

// WriteString writes the string to the underlying response writer.
func (c *Context) WriteString(p string) (int, error) {
	return c.Writer.Write([]byte(p))
}

// WriteHeader writes the header to the underlying resposne writer.
func (c *Context) WriteHeader(statusCode int) {
	c.Writer.WriteHeader(statusCode)
}

// WriteJSON writes the given argument as JSON to the underlying response
// writer.
func (c *Context) WriteJSON(what any) error {
	return json.NewEncoder(c.Writer).Encode(what)
}

// WriteMarshaledJSON first marshals the given argument into a byte array, then
// writes the bytes, assuming no error occurred in marshaling.
func (c *Context) WriteMarshaledJSON(what any) error {
	b, err := json.Marshal(what)
	if err != nil {
		return err
	}
	_, err = c.Write(b)
	return err
}

// WriteFile writes the named file to the response writer (uses
// http.ServeFile).
func (c *Context) WriteFile(name string) {
	http.ServeFile(c.Writer, c.Request, name)
}

// writeError writes the given error code and message to the underlying
// response writer.
func (c *Context) WriteError(code int, msg string) {
	http.Error(c.Writer, msg, code)
}

// ReadBodyJSON reads the body into the given object (should be a pointer).
func (c *Context) ReadBodyJSON(to any) error {
	defer c.Request.Body.Close()
	return json.NewDecoder(c.Request.Body).Decode(to)
}

// Unit is just an alias for an empty struct.
type Unit = struct{}

// MethodAll is used as a wildcard to match any method. Is subject to change.
const MethodAll = ""

// Methods is a collection of HTTP methods.
type Methods map[string]Unit

// NewMethods creates a new methods object with the given methods. Does not do
// any sort of cleaning (e.g., capitalization).
func NewMethods(methods ...string) Methods {
	m := make(Methods, len(methods))
	for _, method := range methods {
		m[method] = Unit{}
	}
	return m
}

// MethodsGet creates a new Methods object with only the GET method.
func MethodsGet() Methods {
	return Methods{http.MethodGet: Unit{}}
}

// MethodsPost creates a new Methods object with only the POST method.
func MethodsPost() Methods {
	return Methods{http.MethodPost: Unit{}}
}

// MethodsPut creates a new Methods object with only the PUT method.
func MethodsPut() Methods {
	return Methods{http.MethodPut: Unit{}}
}

// MethodsDelete creates a new Methods object with only the DELETE method.
func MethodsDelete() Methods {
	return Methods{http.MethodDelete: Unit{}}
}

// MethodsAll creates a new Methods object for all (*) methods.
func MethodsAll() Methods {
	return Methods{MethodAll: Unit{}}
}

// CopyMethods makes a copy of the methods.
//
// Deprecated: Use CloneMethods.
func CopyMethods(methods Methods) Methods {
	return CloneMethods(methods)
}

// CloneMethods clones the methods.
func CloneMethods(methods Methods) Methods {
	m := make(Methods, len(methods))
	for method := range methods {
		m[method] = Unit{}
	}
	return m
}

// Get adds the GET method to the methods.
func (m Methods) Get() Methods {
	m[http.MethodGet] = Unit{}
	return m
}

// POST adds the POST method to the methods.
func (m Methods) Post() Methods {
	m[http.MethodPost] = Unit{}
	return m
}

// Put adds the PUT method to the methods.
func (m Methods) Put() Methods {
	m[http.MethodPut] = Unit{}
	return m
}

// Delete adds the DELETE method to the methods.
func (m Methods) Delete() Methods {
	m[http.MethodDelete] = Unit{}
	return m
}

// All adds the ALL (wildcard) method to the methods.
func (m Methods) All() Methods {
	m[MethodAll] = Unit{}
	return m
}

// CopyFrom copies the methods from the given methods object into the callee.
// Nothing in the callee is removed, it is only addition.
func (m Methods) CopyFrom(methods Methods) Methods {
	for method := range methods {
		m[method] = Unit{}
	}
	return m
}

// Set adds the given method to the methods. No cleaning is done (e.g.,
// capitalization).
func (m Methods) Set(method string) Methods {
	m[method] = Unit{}
	return m
}

// Unset removes the method from the methods. No cleaning is done (e.g.,
// capitalization).
func (m Methods) Unset(method string) Methods {
	delete(m, method)
	return m
}

// Has returns whether the methods contains the given method. No cleaning is
// done (e.g., capitalization).
func (m Methods) Has(method string) bool {
	_, ok := m[method]
	return ok
}

// HasOrAll returns whether the methods contains the given method or if the
// wildcard is present. No cleaning is done (e.g., capitalization).
func (m Methods) HasOrAll(method string) bool {
	_, has := m[method]
	_, all := m[MethodAll]
	return has || all
}
