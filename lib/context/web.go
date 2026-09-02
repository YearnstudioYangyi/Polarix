package context

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

const temporaryRoutePrefix = "/_plugin"

var temporaryRoutePluginID = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// TemporaryRouteOptions controls a public, in-memory route created by a plugin.
// A route is always short lived: TTL must be greater than zero.
type TemporaryRouteOptions struct {
	TTL         time.Duration
	Method      string
	OneTime     bool
	ContentType string
}

// HTTPResponse lets a temporary route handler customise an HTTP response.
// Body accepts the same values as a regular handler result.
type HTTPResponse struct {
	Status  int
	Headers http.Header
	Body    any
}

// TemporaryRouteHandler receives the original HTTP request and returns its body.
// Strings and []byte are sent as content; other values are encoded as JSON.
type TemporaryRouteHandler func(*http.Request) (any, error)

type temporaryRoute struct {
	pluginID    string
	expiresAt   time.Time
	method      string
	oneTime     bool
	contentType string
	handler     TemporaryRouteHandler
}

var temporaryRoutes = struct {
	sync.Mutex
	items map[string]temporaryRoute
}{items: make(map[string]temporaryRoute)}

// RegisterTemporaryRoute creates a public route at
// /_plugin/{pluginID}/{random-token}. It is safe to call while the HTTP server
// is already serving requests. The returned remove function is idempotent.
func (context *Context) RegisterTemporaryRoute(options TemporaryRouteOptions, handler TemporaryRouteHandler) (path string, remove func(), err error) {
	pluginID := context.PluginID
	if !temporaryRoutePluginID.MatchString(pluginID) {
		return "", nil, fmt.Errorf("temporary routes require a plugin-bound context")
	}
	if options.TTL <= 0 {
		return "", nil, errors.New("temporary route TTL must be greater than zero")
	}
	if handler == nil {
		return "", nil, errors.New("temporary route handler is required")
	}
	method := strings.ToUpper(strings.TrimSpace(options.Method))
	if method == "" {
		method = http.MethodGet
	}
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", nil, fmt.Errorf("generate temporary route token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(tokenBytes)
	temporaryRoutes.Lock()
	temporaryRoutes.items[token] = temporaryRoute{pluginID: pluginID, expiresAt: time.Now().Add(options.TTL), method: method, oneTime: options.OneTime, contentType: options.ContentType, handler: handler}
	temporaryRoutes.Unlock()
	return temporaryRoutePrefix + "/" + pluginID + "/" + token, func() {
		temporaryRoutes.Lock()
		delete(temporaryRoutes.items, token)
		temporaryRoutes.Unlock()
	}, nil
}

// ServeTemporaryRoute dispatches a request received by the application's fixed
// temporary-route gateway. Unknown and expired links deliberately look alike.
func ServeTemporaryRoute(w http.ResponseWriter, r *http.Request, pluginID, token string) {
	temporaryRoutes.Lock()
	route, ok := temporaryRoutes.items[token]
	if !ok || route.pluginID != pluginID || !time.Now().Before(route.expiresAt) {
		delete(temporaryRoutes.items, token)
		temporaryRoutes.Unlock()
		http.NotFound(w, r)
		return
	}
	if r.Method != route.method {
		temporaryRoutes.Unlock()
		w.Header().Set("Allow", route.method)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if route.oneTime {
		delete(temporaryRoutes.items, token)
	}
	temporaryRoutes.Unlock()
	result, err := route.handler(r)
	if err != nil {
		http.Error(w, "temporary route handler failed", http.StatusInternalServerError)
		return
	}
	writeTemporaryRouteResponse(w, result, route.contentType)
}

func writeTemporaryRouteResponse(w http.ResponseWriter, result any, contentType string) {
	status := http.StatusOK
	if response, ok := result.(HTTPResponse); ok {
		if response.Status != 0 {
			status = response.Status
		}
		for key, values := range response.Headers {
			w.Header()[key] = append([]string(nil), values...)
		}
		result = response.Body
	}
	if result == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if contentType != "" && w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", contentType)
	}
	switch body := result.(type) {
	case string:
		if w.Header().Get("Content-Type") == "" {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	case []byte:
		if w.Header().Get("Content-Type") == "" {
			w.Header().Set("Content-Type", "application/octet-stream")
		}
		w.WriteHeader(status)
		_, _ = w.Write(body)
	default:
		if w.Header().Get("Content-Type") == "" {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
		}
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	}
}
