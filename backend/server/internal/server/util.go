package server

import (
	"fmt"
	"math"
	"net/http"
	pprofhttp "net/http/pprof"
	"os"
	"regexp"
	"runtime"
	"strconv"

	httptrace "gopkg.in/DataDog/dd-trace-go.v1/contrib/net/http"
	"gopkg.in/DataDog/dd-trace-go.v1/ddtrace/tracer"
	"gopkg.in/DataDog/dd-trace-go.v1/profiler"
)

func getMaximumNumberOfAllowedUsers() int {
	maxNumUsersStr := os.Getenv("HISHTORY_MAX_NUM_USERS")
	if maxNumUsersStr == "" {
		return math.MaxInt
	}
	maxNumUsers, err := strconv.Atoi(maxNumUsersStr)
	if err != nil {
		return math.MaxInt
	}
	return maxNumUsers
}

func configureObservability(mux *httptrace.ServeMux, releaseVersion string) func() {
	// Profiler
	err := profiler.Start(
		profiler.WithService("hishtory-api"),
		profiler.WithVersion(releaseVersion),
		profiler.WithAPIKey(os.Getenv("DD_API_KEY")),
		profiler.WithUDS("/var/run/datadog/apm.socket"),
		profiler.WithProfileTypes(
			profiler.CPUProfile,
			profiler.HeapProfile,
		),
	)
	if err != nil {
		fmt.Printf("Failed to start DataDog profiler: %v\n", err)
	}
	// Tracer
	tracer.Start(
		tracer.WithRuntimeMetrics(),
		tracer.WithService("hishtory-api"),
		tracer.WithUDS("/var/run/datadog/apm.socket"),
	)

	// Pprof
	mux.HandleFunc("/debug/pprof/", pprofhttp.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprofhttp.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprofhttp.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprofhttp.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprofhttp.Trace)

	// Func to stop all of the above
	return func() {
		profiler.Stop()
		tracer.Stop()
	}
}

func getHishtoryVersion(r *http.Request) string {
	return r.Header.Get("X-Hishtory-Version")
}

func getRemoteAddr(r *http.Request) string {
	addr, ok := r.Header["X-Real-Ip"]
	if !ok || len(addr) == 0 {
		return "UnknownIp"
	}
	return addr[0]
}

func getRequiredQueryParam(r *http.Request, queryParam string) string {
	val := r.URL.Query().Get(queryParam)
	if val == "" {
		panic(fmt.Sprintf("request to %s is missing required query param=%#v", r.URL, queryParam))
	}
	return val
}

func getOptionalQueryParam(r *http.Request, queryParam string, isTestEnvironment bool) string {
	val := r.URL.Query().Get(queryParam)
	if val == "" && isTestEnvironment {
		panic(fmt.Sprintf("request to %s is missing optional query param=%#v that is required in test environments", r.URL, queryParam))
	}
	return val
}

func checkGormError(err error) {
	if err == nil {
		return
	}

	_, filename, line, _ := runtime.Caller(1)
	panic(fmt.Sprintf("DB error at %s:%d: %v", filename, line, err))
}

type parsedVersion struct {
	majorVersion int
	minorVersion int
}

func (pv parsedVersion) greaterThan(other parsedVersion) bool {
	if pv.majorVersion == other.majorVersion && pv.minorVersion == other.minorVersion {
		return false
	}
	return !pv.lessThan(other)
}

func (pv parsedVersion) lessThan(other parsedVersion) bool {
	if pv.majorVersion != other.majorVersion {
		return pv.majorVersion < other.majorVersion
	}
	return pv.minorVersion < other.minorVersion
}

func parseVersionString(versionString string) (parsedVersion, error) {
	re := regexp.MustCompile(`v(\d+)[.](\d+)`)
	matches := re.FindAllStringSubmatch(versionString, -1)
	if len(matches) != 1 {
		return parsedVersion{}, fmt.Errorf("failed to parse version=%#v (matches=%#v)", versionString, matches)
	}
	if len(matches[0]) != 3 {
		return parsedVersion{}, fmt.Errorf("failed to parse version=%#v (matches[0]=%#v)", versionString, matches[0])
	}
	majorVersion, err := strconv.Atoi(matches[0][1])
	if err != nil {
		return parsedVersion{}, fmt.Errorf("failed to parse major version %#v", matches[0][1])
	}
	minorVersion, err := strconv.Atoi(matches[0][2])
	if err != nil {
		return parsedVersion{}, fmt.Errorf("failed to parse minor version %#v", matches[0][2])
	}
	return parsedVersion{majorVersion, minorVersion}, nil
}
