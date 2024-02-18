package webui

import (
	"context"
	"embed"
	"fmt"
	"net"
	"net/http"
	"net/url"

	"html/template"

	"github.com/ddworken/hishtory/client/data"
	"github.com/ddworken/hishtory/client/hctx"
	"github.com/ddworken/hishtory/client/lib"
)

//go:embed templates
var templateFiles embed.FS

type webUiData struct {
	SearchQuery   string
	SearchResults [][]string
	ColumnNames   []string
}

func getTableRowsForDisplay(ctx context.Context, searchQuery string) ([][]string, error) {
	results, err := lib.Search(ctx, hctx.GetDb(ctx), searchQuery, 100)
	if err != nil {
		panic(err)
	}
	return buildTableRows(ctx, results)
}

func htmx_resultsTable(w http.ResponseWriter, r *http.Request) {
	searchQuery := r.URL.Query().Get("q")
	tableRows, err := getTableRowsForDisplay(r.Context(), searchQuery)
	if err != nil {
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
		panic(err)
	}
	w.Header().Add("Content-Type", "text/html")
	err = getTemplates().ExecuteTemplate(w, "webui.html", webUiData{
		SearchQuery:   searchQuery,
		SearchResults: tableRows,
		ColumnNames:   hctx.GetConf(r.Context()).DisplayedColumns,
	})
	if err != nil {
		panic(err)
	}
}

func getTemplates() *template.Template {
	return template.Must(template.ParseFS(templateFiles, "templates/*"))

}

func buildTableRows(ctx context.Context, entries []*data.HistoryEntry) ([][]string, error) {
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

func StartWebUiServer(ctx context.Context) error {
	http.HandleFunc("/", webuiHandler)
	http.HandleFunc("/htmx/results-table", htmx_resultsTable)

	server := http.Server{
		BaseContext: func(l net.Listener) context.Context { return ctx },
		Addr:        ":8000",
	}
	fmt.Printf("Starting web server on %s...\n", server.Addr)
	return server.ListenAndServe()
}
