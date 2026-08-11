package httputil

import (
	"net/http"

	"ojv/cog/cam/pipeline"
)

// Chain assembles a slice of HTTP middlewares around a handler.
// Middlewares are applied in order: the first element runs outermost.
func Chain(middlewares []func(http.HandlerFunc) http.HandlerFunc, handler http.HandlerFunc) http.HandlerFunc {
	return pipeline.Chain[http.HandlerFunc](handler, middlewares...)
}
