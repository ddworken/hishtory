//go:build offline
// +build offline

package lib

import "net/http"

func GetHttpClient() *http.Client {
	panic("Cannot GetHttpClient() from a hishtory client compiled with the offline tag!")
}

func IsOfflineBinary() bool {
	return true
}
