package integration

import (
	"net/http"

	"connectme/internal/server"
)

func serverNew(e *env) http.Handler { return server.New(e.deps).Handler }
