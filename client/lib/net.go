//go:build !offline
// +build !offline

package lib

import (
	"net/http"
)

func GetHttpClient() *http.Client {
	return http.DefaultClient
}

func IsOfflineBinary() bool {
	return false
}
