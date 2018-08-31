package expr

import (
	"fmt"
	"net/url"
	"path"
	"sort"
	"strings"

	"github.com/dimfeld/httppath"
	"goa.design/goa/eval"
)

type (
	// HTTPServiceExpr describes a HTTP service. It defines both a result
	// type and a set of endpoints that can be executed through HTTP
	// requests. HTTPServiceExpr embeds a HTTPServiceExpr and adds HTTP specific
	// properties.
	HTTPServiceExpr struct {
		eval.DSLFunc
		// ServiceExpr is the service expression that backs this
		// service.
		ServiceExpr *ServiceExpr
		// Common URL prefixes to all service endpoint HTTP requests
		Paths []string
		// Params defines the HTTP request path and query parameters
		// common to all the service endpoints.
		Params *MappedAttributeExpr
		// Headers defines the HTTP request headers common to all the
		// service endpoints.
		Headers *MappedAttributeExpr
		// Name of parent service if any
		ParentName string
		// Endpoint with canonical service path
		CanonicalEndpointName string
		// HTTPEndpoints is the list of service endpoints.
		HTTPEndpoints []*HTTPEndpointExpr
		// HTTPErrors lists HTTP errors that apply to all endpoints.
		HTTPErrors []*HTTPErrorExpr
		// FileServers is the list of static asset serving endpoints
		FileServers []*HTTPFileServerExpr
		// Meta is a set of key/value pairs with semantic that is
		// specific to each generator.
		Meta MetaExpr
	}
)

// Name of service (service)
func (svc *HTTPServiceExpr) Name() string {
	return svc.ServiceExpr.Name
}

// Description of service (service)
func (svc *HTTPServiceExpr) Description() string {
	return svc.ServiceExpr.Description
}

// Schemes returns the service endpoint HTTP schemes.
func (svc *HTTPServiceExpr) Schemes() []string {
	schemes := make(map[string]bool)
	for _, s := range svc.ServiceExpr.Servers {
		if u, err := url.Parse(s.URL); err != nil {
			schemes[u.Scheme] = true
		}
	}
	if len(schemes) == 0 {
		return nil
	}
	ss := make([]string, len(schemes))
	i := 0
	for s := range schemes {
		ss[i] = s
		i++
	}
	sort.Strings(ss)
	return ss
}

// Error returns the error with the given name.
func (svc *HTTPServiceExpr) Error(name string) *ErrorExpr {
	for _, erro := range svc.ServiceExpr.Errors {
		if erro.Name == name {
			return erro
		}
	}
	return Root.Error(name)
}

// Endpoint returns the service endpoint with the given name or nil if there
// isn't one.
func (svc *HTTPServiceExpr) Endpoint(name string) *HTTPEndpointExpr {
	for _, a := range svc.HTTPEndpoints {
		if a.Name() == name {
			return a
		}
	}
	return nil
}

// EndpointFor builds the endpoint for the given method.
func (svc *HTTPServiceExpr) EndpointFor(name string, m *MethodExpr) *HTTPEndpointExpr {
	if a := svc.Endpoint(name); a != nil {
		return a
	}
	a := &HTTPEndpointExpr{
		MethodExpr: m,
		Service:    svc,
	}
	svc.HTTPEndpoints = append(svc.HTTPEndpoints, a)
	return a
}

// CanonicalEndpoint returns the canonical endpoint of the service if any.
// The canonical endpoint is used to compute hrefs to services.
func (svc *HTTPServiceExpr) CanonicalEndpoint() *HTTPEndpointExpr {
	name := svc.CanonicalEndpointName
	if name == "" {
		name = "show"
	}
	return svc.Endpoint(name)
}

// URITemplate returns a URI template to this service.
// The result is the empty string if the service does not have a "show" endpoint
// and does not define a different canonical endpoint.
func (svc *HTTPServiceExpr) URITemplate() string {
	ca := svc.CanonicalEndpoint()
	if ca == nil || len(ca.Routes) == 0 {
		return ""
	}
	return ca.Routes[0].FullPaths()[0]
}

// FullPaths computes the base paths to the service endpoints concatenating the
// API and parent service base paths as needed.
func (svc *HTTPServiceExpr) FullPaths() []string {
	if len(svc.Paths) == 0 {
		return []string{path.Join(Root.API.HTTP.Path)}
	}
	var paths []string
	for _, p := range svc.Paths {
		if strings.HasPrefix(p, "//") {
			paths = append(paths, httppath.Clean(p))
			continue
		}
		var basePaths []string
		if p := svc.Parent(); p != nil {
			if ca := p.CanonicalEndpoint(); ca != nil {
				if routes := ca.Routes; len(routes) > 0 {
					// Note: all these tests should be true at code
					// generation time as DSL validation makes sure
					// that parent services have a canonical path.
					fullPaths := routes[0].FullPaths()
					basePaths = make([]string, len(fullPaths))
					for i, p := range fullPaths {
						basePaths[i] = path.Join(p)
					}
				}
			}
		} else {
			basePaths = []string{Root.API.HTTP.Path}
		}
		for _, base := range basePaths {
			paths = append(paths, httppath.Clean(path.Join(base, p)))
		}
	}
	return paths
}

// Parent returns the parent service if any, nil otherwise.
func (svc *HTTPServiceExpr) Parent() *HTTPServiceExpr {
	if svc.ParentName != "" {
		if parent := Root.API.HTTP.Service(svc.ParentName); parent != nil {
			return parent
		}
	}
	return nil
}

// HTTPError returns the service HTTP error with given name if any.
func (svc *HTTPServiceExpr) HTTPError(name string) *HTTPErrorExpr {
	for _, erro := range svc.HTTPErrors {
		if erro.Name == name {
			return erro
		}
	}
	return nil
}

// EvalName returns the generic definition name used in error messages.
func (svc *HTTPServiceExpr) EvalName() string {
	if svc.Name() == "" {
		return "unnamed service"
	}
	return fmt.Sprintf("service %#v", svc.Name())
}

// Prepare initializes the error responses.
func (svc *HTTPServiceExpr) Prepare() {
	for _, er := range svc.HTTPErrors {
		er.Response.Prepare()
	}
}

// Validate makes sure the service is valid.
func (svc *HTTPServiceExpr) Validate() error {
	verr := new(eval.ValidationErrors)
	if svc.Params != nil {
		verr.Merge(svc.Params.Validate("parameters", svc))
	}
	if svc.Headers != nil {
		verr.Merge(svc.Headers.Validate("headers", svc))
	}
	if n := svc.ParentName; n != "" {
		if p := Root.API.HTTP.Service(n); p == nil {
			verr.Add(svc, "Parent service %s not found", n)
		} else {
			if p.CanonicalEndpoint() == nil {
				verr.Add(svc, "Parent service %s has no canonical endpoint", n)
			}
			if p.ParentName == svc.Name() {
				verr.Add(svc, "Parent service %s is also child", n)
			}
		}
	}
	if n := svc.CanonicalEndpointName; n != "" {
		if a := svc.Endpoint(n); a == nil {
			verr.Add(svc, "Unknown canonical endpoint %s", n)
		}
	}

	// Validate errors (have status codes and bodies are valid)
	for _, er := range svc.HTTPErrors {
		verr.Merge(er.Validate())
	}
	for _, er := range Root.API.HTTP.Errors {
		// This may result in the same error being validated multiple
		// times however service is the top level expression being
		// walked and errors cannot be walked until all expressions have
		// run. Another solution could be to append a new dynamically
		// generated root that the eval engine would process after. Keep
		// things simple for now.
		verr.Merge(er.Validate())
	}

	return verr
}
