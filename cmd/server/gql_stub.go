package main

import (
    "net/http"

    "groovarr/graph"
)

// registerGQL is a no-op stub used when gqlgen-generated code is not present.
// The real implementation is provided in gql_server.go which is built with the
// "gqlgen" build tag.
func registerGQL(mux *http.ServeMux, resolver *graph.Resolver) {
    // no-op
    _ = mux
    _ = resolver
}
