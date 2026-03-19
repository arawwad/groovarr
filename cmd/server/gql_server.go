//go:build gqlgen

package main

import (
    "net/http"

    "github.com/99designs/gqlgen/graphql/handler"
    "github.com/99designs/gqlgen/graphql/playground"
    "groovarr/graph"
    "groovarr/graph/generated"
)

// registerGQL installs a gqlgen-backed GraphQL handler. This file is only
// compiled when building with the "gqlgen" build tag, e.g.:
//    go build -tags gqlgen ./cmd/server
func registerGQL(mux *http.ServeMux, resolver *graph.Resolver) {
    srv := handler.NewDefaultServer(generated.NewExecutableSchema(generated.Config{Resolvers: resolver}))
    mux.Handle("/graphql", srv)
    mux.Handle("/playground", playground.Handler("GraphQL playground", "/graphql"))
}
