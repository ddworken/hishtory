package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLoggerMiddleware(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("test"))
	})
	var out strings.Builder

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Add("X-Real-Ip", "127.0.0.1")
	logHandler := withLogging(nil, &out)(handler)
	logHandler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected %d, got %d", http.StatusOK, w.Code)
	}
	const expectedPiece = `127.0.0.1 GET "/"`
	if !strings.Contains(out.String(), expectedPiece) {
		t.Errorf("expected %q, got %q", expectedPiece, out.String())
	}
}

func TestLoggerMiddlewareWithPanic(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic(fmt.Errorf("synthetic panic for tests"))
	})

	var out strings.Builder

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Add("X-Real-Ip", "127.0.0.1")
	logHandler := withLogging(nil, &out)(handler)

	var panicked bool
	var panicError any
	func() {
		defer func() {
			if r := recover(); r != nil {
				panicked = true
				panicError = r
			}
		}()
		logHandler.ServeHTTP(w, req)
	}()

	if !panicked {
		t.Errorf("expected panic")
	}
	// the logger does not write anything if there is a panic, so the response code is the http default of 200
	if w.Code != http.StatusOK {
		t.Errorf("expected %d, got %d", http.StatusOK, w.Code)
	}

	const expectedPiece1 = `synthetic panic for tests`
	const expectedPiece2 = `127.0.0.1 GET "/"`
	outString := out.String()
	if !strings.Contains(outString, expectedPiece1) {
		t.Errorf("expected %q, got %q", expectedPiece1, outString)
	}
	if !strings.Contains(outString, expectedPiece2) {
		t.Errorf("expected %q, got %q", expectedPiece2, outString)
	}

	panicStr := fmt.Sprintf("%v", panicError)
	if !strings.Contains(panicStr, "synthetic panic for tests") {
		t.Errorf("expected panic error to contain %q, got %q", "synthetic panic for tests", panicStr)
	}
}

func TestPanicGuard(t *testing.T) {
	fmt.Println("Output prefix to avoid breaking gotestsum with panics")
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic(fmt.Errorf("synthetic panic for tests"))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Add("X-Real-Ip", "127.0.0.1")
	wrappedHandler := withPanicGuard(nil)(handler)

	var panicked bool
	func() {
		defer func() {
			if r := recover(); r != nil {
				panicked = true
			}
		}()
		wrappedHandler.ServeHTTP(w, req)
	}()

	if panicked {
		t.Fatalf("expected no panic")
	}
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d, got %d", http.StatusServiceUnavailable, w.Code)
	}
}

func TestPanicGuardNoPanic(t *testing.T) {
	fmt.Println("Output prefix to avoid breaking gotestsum with panics")
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Add("X-Real-Ip", "127.0.0.1")

	wrappedHandler := withPanicGuard(nil)(handler)

	var panicked bool
	func() {
		defer func() {
			if r := recover(); r != nil {
				panicked = true
			}
		}()
		wrappedHandler.ServeHTTP(w, req)
	}()

	if panicked {
		t.Fatalf("expected no panic")
	}
	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}
}

func TestMergeMiddlewares(t *testing.T) {
	fmt.Println("Output prefix to avoid breaking gotestsum with panics")
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("test"))
	})
	panicHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic(fmt.Errorf("synthetic panic for tests"))
	})

	// ===
	tests := []struct {
		name               string
		handler            http.Handler
		expectedStatusCode int
		expectedPieces     []string
	}{
		{
			name:               "no panics",
			handler:            handler,
			expectedStatusCode: http.StatusOK,
			expectedPieces: []string{
				`127.0.0.1 GET "/"`,
			},
		},
		{
			name:               "panics",
			handler:            panicHandler,
			expectedStatusCode: http.StatusServiceUnavailable,
			expectedPieces: []string{
				`synthetic panic for tests`,
				`127.0.0.1 GET "/"`,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var out strings.Builder
			middlewares := mergeMiddlewares(
				withPanicGuard(nil),
				withLogging(nil, &out),
			)

			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Add("X-Real-Ip", "127.0.0.1")

			wrappedHandler := middlewares(test.handler)
			var panicked bool
			func() {
				defer func() {
					if r := recover(); r != nil {
						panicked = true
					}
				}()
				wrappedHandler.ServeHTTP(w, req)
			}()

			if panicked {
				t.Fatalf("expected no panic")
			}
			if w.Code != test.expectedStatusCode {
				t.Errorf("expected response status to be %d, got %d", test.expectedStatusCode, w.Code)
			}

			for _, expectedPiece := range test.expectedPieces {
				if !strings.Contains(out.String(), expectedPiece) {
					t.Errorf("expected %q, got %q", expectedPiece, out.String())
				}
			}
		})
	}
}
