package main

import (
	"fmt"
	"github.com/DataDog/datadog-go/statsd"
	"gopkg.in/DataDog/dd-trace-go.v1/ddtrace/ext"
	"gopkg.in/DataDog/dd-trace-go.v1/ddtrace/tracer"
	"net/http"
	"reflect"
	"runtime"
	"strings"
	"time"
)

type loggedResponseData struct {
	size int
}

type loggingResponseWriter struct {
	http.ResponseWriter
	responseData *loggedResponseData
}

func (r *loggingResponseWriter) Write(b []byte) (int, error) {
	size, err := r.ResponseWriter.Write(b)
	r.responseData.size += size
	return size, err
}

func (r *loggingResponseWriter) WriteHeader(statusCode int) {
	r.ResponseWriter.WriteHeader(statusCode)
}

func getFunctionName(temp interface{}) string {
	strs := strings.Split((runtime.FuncForPC(reflect.ValueOf(temp).Pointer()).Name()), ".")
	return strs[len(strs)-1]
}

func byteCountToString(b int) string {
	const unit = 1000
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "kMG"[exp])
}

type Middleware func(http.HandlerFunc) http.Handler

func withLogging(s *statsd.Client) Middleware {
	return func(h http.HandlerFunc) http.Handler {
		return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			var responseData loggedResponseData
			lrw := loggingResponseWriter{
				ResponseWriter: rw,
				responseData:   &responseData,
			}
			start := time.Now()
			span, ctx := tracer.StartSpanFromContext(
				r.Context(),
				getFunctionName(h),
				tracer.SpanType(ext.SpanTypeSQL),
				tracer.ServiceName("hishtory-api"),
			)
			defer span.Finish()

			h.ServeHTTP(&lrw, r.WithContext(ctx))

			duration := time.Since(start)
			fmt.Printf("%s %s %#v %s %s %s\n", getRemoteAddr(r), r.Method, r.RequestURI, getHishtoryVersion(r), duration.String(), byteCountToString(responseData.size))
			if s != nil {
				s.Distribution("hishtory.request_duration", float64(duration.Microseconds())/1_000, []string{"HANDLER=" + getFunctionName(h)}, 1.0)
				s.Incr("hishtory.request", []string{}, 1.0)
			}
		})
	}
}
