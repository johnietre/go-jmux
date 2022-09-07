package jmux

/* TODO
 * Apply methods only for endpoints, rather than all parent routes in the path
 * Add way to allow only exact path matches (/a/b/c doesn't match /a/b or /)
 */

import (
  "context"
  "encoding/json"
  "net/http"
  "strings"
)

type contextKeyType string

const ParamsKey contextKeyType = "jmuxkey"

type HandlerFunc func(*Context)

func WrapH(h http.Handler) HandlerFunc {
  return func(c *Context) {
    r := c.Request.WithContext(
      context.WithValue(c.Request.Context(), ParamsKey, c.Params),
    )
    h.ServeHTTP(c.Writer, r)
  }
}

func WrapF(f func(http.ResponseWriter, *http.Request)) HandlerFunc {
  return func(c *Context) {
    r := c.Request.WithContext(
      context.WithValue(c.Request.Context(), ParamsKey, c.Params),
    )
    f(c.Writer, r)
  }
}

type Route struct {
  name string
  param bool
  methods Methods
  routes map[string]*Route
  handlers map[string]HandlerFunc
}

func (route *Route) getHandler(method string) HandlerFunc {
  f := route.handlers[method]
  if f == nil {
    f = route.handlers["*"]
  }
  return f
}

func (route *Route) handleFunc(pattern string, methods Methods, f HandlerFunc) {
  if pattern == "" {
    for method := range methods {
      route.handlers[method] = f
    }
    return
  }
  l := nextSlug(pattern)
  if l == -1 {
    l = len(pattern)
  }
  slug, param := pattern[:l], false
  if slug[0] == '{' {
    if slug[l-1] != '}' {
      panic("missing closing brace in pattern: " + pattern)
    }
    slug = slug[1:l-1]
    param = true
  }
  r, ok := route.routes[slug]
  if !ok {
    r = &Route{
      name: slug,
      param: param,
      methods: CopyMethods(methods),
      routes: make(map[string]*Route),
      handlers: make(map[string]HandlerFunc),
    }
    route.routes[slug] = r
  } else {
    r.methods.CopyFrom(methods)
  }
  if l == len(pattern) {
    r.handleFunc("", methods, f)
    return // TODO
  }
  r.handleFunc(pattern[l+1:], methods, f)
}

type Router struct {
  base *Route
  // map[method]HandlerFunc
  defaultHandlers map[string]HandlerFunc
}

func NewRouter() *Router {
  // TODO: Name base (i.e., add base as global)?
  return &Router{
    base: &Route{
      methods: make(Methods),
      routes: make(map[string]*Route),
      handlers: make(map[string]HandlerFunc),
    },
    defaultHandlers: make(map[string]HandlerFunc),
  }
}

func (router *Router) HandleFunc(pattern string, methods Methods, f HandlerFunc) {
  if pattern == "" {
    return
  } else if pattern == "/" {
    // TODO: Set base name
    for method := range methods {
      router.base.handlers[method] = f
    }
    router.base.methods.CopyFrom(methods)
    return
  }
  if pattern[0] == '/' {
    pattern = pattern[1:]
  }
  if l1 := len(pattern) - 1; pattern[l1] == '/' {
    pattern = pattern[:l1]
  }
  router.base.handleFunc(pattern, methods, f)
}

func (router *Router) Get(pattern string, f HandlerFunc) {
  router.HandleFunc(pattern, MethodsGet(), f)
}

func (router *Router) Post(pattern string, f HandlerFunc) {
  router.HandleFunc(pattern, MethodsPost(), f)
}

func (router *Router) Put(pattern string, f HandlerFunc) {
  router.HandleFunc(pattern, MethodsPut(), f)
}

func (router *Router) Delete(pattern string, f HandlerFunc) {
  router.HandleFunc(pattern, MethodsDelete(), f)
}

func (router *Router) All(pattern string, f HandlerFunc) {
  router.HandleFunc(pattern, MethodsAll(), f)
}

func (router *Router) Default(methods Methods, f HandlerFunc) {
  for method := range methods {
    router.defaultHandlers[method] = f
  }
}

func (router *Router) ServeHTTP(w http.ResponseWriter, r *http.Request) {
  urlPath := r.URL.Path
  if len(urlPath) != 0 && urlPath[0] == '/' {
    urlPath = urlPath[1:]
  }
  route, params := router.base, make(map[string]string)
pathLoop:
  for l := nextSlug(urlPath); urlPath != ""; l = nextSlug(urlPath) {
    var slug string
    if l != -1 {
      slug = urlPath[:l]
      urlPath = urlPath[l+1:]
    } else {
      slug = urlPath
      urlPath = ""
    }
    ro := route.routes[slug]
    if ro == nil {
      for _, route = range route.routes {
        if route.param && route.methods.HasOrAll(r.Method) {
          params[route.name] = slug
          continue pathLoop
        }
      }
      //router.serveDefault(w, r)
      //return
      break
    }
    route = ro
    if !route.methods.HasOrAll(r.Method) {
      router.serveDefault(w, r)
      return
    }
    if route.param {
      params[route.name] = slug
    }
  }

  // True if the route doesn't have an associated handler (not an endpoint)
  handler := route.getHandler(r.Method)
  if handler == nil {
    router.serveDefault(w, r)
    return
  }
  handler(newContext(w, r, params))
}

func (router *Router) serveDefault(w http.ResponseWriter, r *http.Request) {
  handler := router.defaultHandlers[r.Method]
  if handler == nil {
    // TODO: Return different status?
    w.WriteHeader(http.StatusNotFound)
    return
  }
  handler(&Context{
    Request: r,
    Writer: w,
    Params: make(map[string]string),
  })
}

func nextSlug(path string) int {
  return strings.IndexByte(path, '/')
}

type Context struct {
  Request *http.Request
  Writer http.ResponseWriter
  Params map[string]string
}

func newContext(w http.ResponseWriter, r *http.Request, params map[string]string) *Context {
  return &Context{Writer: w, Request: r, Params: params}
}

func (c *Context) Write(p []byte) (int, error) {
  return c.Writer.Write(p)
}

func (c *Context) WriteHeader(statusCode int) {
  c.Writer.WriteHeader(statusCode)
}

func (c *Context) WriteJSON(what any) error {
  return json.NewEncoder(c.Writer).Encode(what)
}

func (c *Context) WriteError(code int, msg string) {
  http.Error(c.Writer, msg, code)
}

func (c *Context) ReadBodyJSON(to any) error {
  defer c.Request.Body.Close()
  return json.NewDecoder(c.Request.Body).Decode(to)
}

type Unit struct{}

type Methods map[string]Unit

func NewMethods(methods ...string) Methods {
  m := make(Methods, len(methods))
  for _, method := range methods {
    m[method] = Unit{}
  }
  return m
}

func MethodsGet() Methods {
  return Methods{http.MethodGet: Unit{}}
}

func MethodsPost() Methods {
  return Methods{http.MethodPost: Unit{}}
}

func MethodsPut() Methods {
  return Methods{http.MethodPut: Unit{}}
}

func MethodsDelete() Methods {
  return Methods{http.MethodDelete: Unit{}}
}

func MethodsAll() Methods {
  return Methods{"*": Unit{}}
}

func CopyMethods(methods Methods) Methods {
  m := make(Methods, len(methods))
  for method := range methods {
    m[method] = Unit{}
  }
  return m
}

func (m Methods) Get() Methods {
  m[http.MethodGet] = Unit{}
  return m
}

func (m Methods) Post() Methods {
  m[http.MethodPost] = Unit{}
  return m
}

func (m Methods) Put() Methods {
  m[http.MethodPut] = Unit{}
  return m
}

func (m Methods) Delete() Methods {
  m[http.MethodDelete] = Unit{}
  return m
}

func (m Methods) All() Methods {
  m["*"] = Unit{}
  return m
}

func (m Methods) CopyFrom(methods Methods) Methods {
  for method := range methods {
    m[method] = Unit{}
  }
  return m
}

func (m Methods) Set(method string) Methods {
  m[method] = Unit{}
  return m
}

func (m Methods) Unset(method string) Methods {
  delete(m, method)
  return m
}

func (m Methods) Has(method string) bool {
  _, ok := m[method]
  return ok
}

func (m Methods) HasOrAll(method string) bool {
  _, has := m[method]
  _, all := m["*"]
  return has || all
}
