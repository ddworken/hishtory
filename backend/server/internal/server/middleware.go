package server

import (
	"fmt"
	"io"
	"net/http"
	"reflect"
	"runtime"
	"strings"
	"time"

	"github.com/DataDog/datadog-go/statsd"
	"gopkg.in/DataDog/dd-trace-go.v1/ddtrace/ext"
	"gopkg.in/DataDog/dd-trace-go.v1/ddtrace/tracer"
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

type Middleware func(http.Handler) http.Handler

// mergeMiddlewares creates a new middleware that runs the given middlewares in reverse order. The first middleware
// passed will be the "outermost" one
func mergeMiddlewares(middlewares ...Middleware) Middleware {
	return func(h http.Handler) http.Handler {
		for i := len(middlewares) - 1; i >= 0; i-- {
			h = middlewares[i](h)
		}
		return h
	}
}

// withLogging will log every request made to the wrapped endpoint. It will also log
// panics, but won't stop them.
func withLogging(s *statsd.Client, out io.Writer) Middleware {
	return func(h http.Handler) http.Handler {
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

			defer func() {
				// log panics
				if err := recover(); err != nil {
					duration := time.Since(start)
					fmt.Fprintf(out, "%s %s %#v %s %s %s %v\n", getRemoteAddr(r), r.Method, r.RequestURI, getHishtoryVersion(r), duration.String(), byteCountToString(responseData.size), err)

					// keep panicking
					panic(err)
				}
			}()

			h.ServeHTTP(&lrw, r.WithContext(ctx))

			duration := time.Since(start)
			fmt.Fprintf(out, "%s %s %#v %s %s %s\n", getRemoteAddr(r), r.Method, r.RequestURI, getHishtoryVersion(r), duration.String(), byteCountToString(responseData.size))
			if s != nil {
				s.Distribution("hishtory.request_duration", float64(duration.Microseconds())/1_000, []string{"handler:" + getFunctionName(h)}, 1.0)
				s.Incr("hishtory.request", []string{"handler:" + getFunctionName(h)}, 1.0)
			}
		})
	}
}

// withPanicGuard is the last defence from a panic. it will log them and return a 500 error
// to the client and prevent the http server from breaking
func withPanicGuard() Middleware {
	return func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			defer func() {
				if r := recover(); r != nil {
					fmt.Printf("panic: %s\n", r)
					rw.WriteHeader(http.StatusInternalServerError)
				}
			}()
			h.ServeHTTP(rw, r)
		})
	}
}
