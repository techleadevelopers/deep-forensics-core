package api

import (
	"encoding/base64"
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var b64 = base64.StdEncoding

func promHandler() http.Handler { return promhttp.Handler() }
