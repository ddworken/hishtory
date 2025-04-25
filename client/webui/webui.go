package webui

import (
	"context"
	"crypto/subtle"
	"embed"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"net/url"

	"github.com/ddworken/hishtory/client/data"
	"github.com/ddworken/hishtory/client/hctx"
	"github.com/ddworken/hishtory/client/lib"

	"github.com/google/uuid"
)

//go:embed templates
var templateFiles embed.FS

type webUiData struct {
	SearchQuery   string
	SearchResults [][]string
	ColumnNames   []string
}

func getTableRowsForDisplay(ctx context.Context, searchQuery string) (rows [][]string, err error) {
	results, err := lib.Search(ctx, hctx.GetDb(ctx), searchQuery, 100)
	if err != nil {
		return nil, err
	}
	return buildTableRows(ctx, results)
}

func htmx_resultsTable(w http.ResponseWriter, r *http.Request) {
	searchQuery := r.URL.Query().Get("q")
	tableRows, err := getTableRowsForDisplay(r.Context(), searchQuery)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		panic(err)
	}
	w.Header().Add("Content-Type", "text/html")
	w.Header().Add("HX-Replace-Url", getNewUrl(r, searchQuery))
	err = getTemplates().ExecuteTemplate(w, "resultsTable.html", webUiData{
		SearchQuery:   searchQuery,
		SearchResults: tableRows,
		ColumnNames:   hctx.GetConf(r.Context()).DisplayedColumns,
	})
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		panic(err)
	}
}

func getNewUrl(r *http.Request, searchQuery string) string {
	urlStr := r.Header.Get("Hx-Current-Url")
	if urlStr == "" {
		// In this function we purposefully want to silence any errors since updating the URL is non-critical, so
		// we always return an empty string rather than handling the error.
		return ""
	}
	url, err := url.Parse(urlStr)
	if err != nil {
		return ""
	}
	q := url.Query()
	q.Set("q", searchQuery)
	url.RawQuery = q.Encode()
	return url.String()
}

func webuiHandler(w http.ResponseWriter, r *http.Request) {
	searchQuery := r.URL.Query().Get("q")
	tableRows, err := getTableRowsForDisplay(r.Context(), searchQuery)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		panic(err)
	}
	w.Header().Add("Content-Type", "text/html")
	err = getTemplates().ExecuteTemplate(w, "webui.html", webUiData{
		SearchQuery:   searchQuery,
		SearchResults: tableRows,
		ColumnNames:   hctx.GetConf(r.Context()).DisplayedColumns,
	})
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		panic(err)
	}
}

func getTemplates() *template.Template {
	return template.Must(template.ParseFS(templateFiles, "templates/*"))
}

func buildTableRows(ctx context.Context, entries []*data.HistoryEntry) (rows [][]string, err error) {
	columnNames := hctx.GetConf(ctx).DisplayedColumns
	ret := make([][]string, 0)
	for _, entry := range entries {
		row, err := lib.BuildTableRow(ctx, columnNames, *entry, func(s string) string { return s })
		if err != nil {
			return nil, err
		}
		ret = append(ret, row)
	}
	return ret, nil
}

func withBasicAuth(expectedUsername, expectedPassword string) func(h http.Handler) http.Handler {
	return func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			username, password, hasCreds := r.BasicAuth()
			if !hasCreds || !secureStringEquals(username, expectedUsername) || !secureStringEquals(password, expectedPassword) {
				w.Header().Add("WWW-Authenticate", "Basic realm=\"User Visible Realm\"")
				w.WriteHeader(401)
				return
			}
			h.ServeHTTP(w, r)
		})
	}
}

func secureStringEquals(s1, s2 string) bool {
	return subtle.ConstantTimeCompare([]byte(s1), []byte(s2)) == 1
}

func StartWebUiServer(ctx context.Context, port int, disableAuth bool, overridenUsername, overridenPassword string) error {
	username := "hishtory"
	// Note that uuid.NewRandom() uses crypto/rand and returns a UUID with 122 bits of security
	password := uuid.Must(uuid.NewRandom()).String()
	if overridenUsername != "" && overridenPassword != "" {
		username = overridenUsername
		password = overridenPassword
	}
	wba := withBasicAuth(username, password)
	if disableAuth {
		// No-op wrapper that doesn't enforce auth
		wba = func(h http.Handler) http.Handler { return h }
	}
	http.Handle("/", wba(http.HandlerFunc(webuiHandler)))
	http.Handle("/htmx/results-table", wba(http.HandlerFunc(htmx_resultsTable)))

	server := http.Server{
		BaseContext: func(l net.Listener) context.Context { return ctx },
		Addr:        fmt.Sprintf(":%d", port),
	}
	fmt.Printf("Starting web server on %s...\n", server.Addr)
	fmt.Printf("Username: %s\nPassword: %s\n", username, password)
	return server.ListenAndServe()
}
