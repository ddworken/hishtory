package lib

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	_ "embed" // for embedding config.sh

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/ddworken/hishtory/client/data"
	"github.com/ddworken/hishtory/client/hctx"
	"github.com/ddworken/hishtory/shared"
	"github.com/muesli/termenv"
	"golang.org/x/term"
)

const TABLE_HEIGHT = 20
const PADDED_NUM_ENTRIES = TABLE_HEIGHT * 5

var selectedCommand string = ""

var baseStyle = lipgloss.NewStyle().
	BorderStyle(lipgloss.NormalBorder()).
	BorderForeground(lipgloss.Color("240"))

type model struct {
	// context
	ctx *context.Context

	// Model for the loading spinner.
	spinner spinner.Model
	// Whether data is still loading and the spinner should still be displayed.
	isLoading bool

	// Whether the TUI is quitting.
	quitting bool

	// The table used for displaying search results.
	table table.Model
	// The entries in the table
	tableEntries []*data.HistoryEntry
	// Whether the user has hit enter to select an entry and the TUI is thus about to quit.
	selected bool

	// The search box for the query
	queryInput textinput.Model
	// The query to run. Reset to nil after it was run.
	runQuery *string
	// The previous query that was run.
	lastQuery string

	// Unrecoverable error.
	fatalErr error
	// An error while searching. Recoverable and displayed as a warning message.
	searchErr error
	// Whether the device is offline. If so, a warning will be displayed.
	isOffline bool

	// A banner from the backend to be displayed. Generally an empty string.
	banner string
}

type doneDownloadingMsg struct{}
type offlineMsg struct{}
type bannerMsg struct {
	banner string
}

func initialModel(ctx *context.Context, t table.Model, tableEntries []*data.HistoryEntry, initialQuery string) model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))
	queryInput := textinput.New()
	queryInput.Placeholder = "ls"
	queryInput.Focus()
	queryInput.CharLimit = 156
	queryInput.Width = 50
	if initialQuery != "" {
		queryInput.SetValue(initialQuery)
	}
	return model{ctx: ctx, spinner: s, isLoading: true, table: t, tableEntries: tableEntries, runQuery: &initialQuery, queryInput: queryInput}
}

func (m model) Init() tea.Cmd {
	return m.spinner.Tick
}

func runQueryAndUpdateTable(m model, updateTable bool) model {
	if (m.runQuery != nil && *m.runQuery != m.lastQuery) || updateTable {
		if m.runQuery == nil {
			m.runQuery = &m.lastQuery
		}
		rows, entries, err := getRows(m.ctx, hctx.GetConf(m.ctx).DisplayedColumns, *m.runQuery, PADDED_NUM_ENTRIES)
		m.searchErr = err
		if err != nil {
			return m
		}
		m.tableEntries = entries
		if updateTable {
			t, err := makeTable(m.ctx, rows)
			if err != nil {
				m.fatalErr = err
				return m
			}
			m.table = t
		}
		m.table.SetRows(rows)
		m.table.SetCursor(0)
		m.lastQuery = *m.runQuery
		m.runQuery = nil
	}
	if m.table.Cursor() >= len(m.tableEntries) {
		// Ensure that we can't scroll past the end of the table
		m.table.SetCursor(len(m.tableEntries) - 1)
	}
	return m
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc", "ctrl+c", "ctrl+d":
			m.quitting = true
			return m, tea.Quit
		case "enter":
			if len(m.tableEntries) != 0 {
				m.selected = true
			}
			return m, tea.Quit
		case "ctrl+k":
			err := deleteHistoryEntry(m.ctx, *m.tableEntries[m.table.Cursor()])
			if err != nil {
				m.fatalErr = err
				return m, nil
			}
			m = runQueryAndUpdateTable(m, true)
			return m, nil
		default:
			t, cmd1 := m.table.Update(msg)
			m.table = t
			if strings.HasPrefix(msg.String(), "alt+") {
				return m, tea.Batch(cmd1)
			}
			i, cmd2 := m.queryInput.Update(msg)
			m.queryInput = i
			searchQuery := m.queryInput.Value()
			m.runQuery = &searchQuery
			m = runQueryAndUpdateTable(m, false)
			return m, tea.Batch(cmd1, cmd2)
		}
	case tea.WindowSizeMsg:
		m = runQueryAndUpdateTable(m, true)
		return m, nil
	case offlineMsg:
		m.isOffline = true
		return m, nil
	case bannerMsg:
		m.banner = msg.banner
		return m, nil
	case doneDownloadingMsg:
		m.isLoading = false
		return m, nil
	default:
		var cmd tea.Cmd
		if m.isLoading {
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		} else {
			m.table, cmd = m.table.Update(msg)
			return m, cmd
		}
	}
}

func (m model) View() string {
	if m.fatalErr != nil {
		return fmt.Sprintf("An unrecoverable error occured: %v\n", m.fatalErr)
	}
	if m.selected {
		selectedCommand = m.tableEntries[m.table.Cursor()].Command
		return ""
	}
	if m.quitting {
		return ""
	}
	loadingMessage := ""
	if m.isLoading {
		loadingMessage = fmt.Sprintf("%s Loading hishtory entries from other devices...", m.spinner.View())
	}
	warning := ""
	if m.isOffline {
		warning += "Warning: failed to contact the hishtory backend (are you offline?), so some results may be stale\n\n"
	}
	if m.searchErr != nil {
		warning += fmt.Sprintf("Warning: failed to search: %v\n\n", m.searchErr)
	}
	return fmt.Sprintf("\n%s\n%s%s\nSearch Query: %s\n\n%s\n", loadingMessage, warning, m.banner, m.queryInput.View(), baseStyle.Render(m.table.View()))
}

func getRows(ctx *context.Context, columnNames []string, query string, numEntries int) ([]table.Row, []*data.HistoryEntry, error) {
	db := hctx.GetDb(ctx)
	config := hctx.GetConf(ctx)
	data, err := Search(ctx, db, query, numEntries)
	if err != nil {
		return nil, nil, err
	}
	var rows []table.Row
	lastCommand := ""
	for i := 0; i < numEntries; i++ {
		if i < len(data) {
			entry := data[i]
			if strings.TrimSpace(entry.Command) == strings.TrimSpace(lastCommand) && config.FilterDuplicateCommands {
				continue
			}
			entry.Command = strings.ReplaceAll(entry.Command, "\n", "\\n")
			row, err := buildTableRow(ctx, columnNames, *entry)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to build row for entry=%#v: %v", entry, err)
			}
			rows = append(rows, row)
			lastCommand = entry.Command
		} else {
			rows = append(rows, table.Row{})
		}
	}
	return rows, data, nil
}

func calculateColumnWidths(rows []table.Row, numColumns int) []int {
	neededColumnWidth := make([]int, numColumns)
	for _, row := range rows {
		for i, v := range row {
			neededColumnWidth[i] = max(neededColumnWidth[i], len(v))
		}
	}
	return neededColumnWidth
}

func getTerminalSize() (int, int, error) {
	return term.GetSize(2)
}

var bigQueryResults []table.Row

func makeTableColumns(ctx *context.Context, columnNames []string, rows []table.Row) ([]table.Column, error) {
	// Handle an initial query with no results
	if len(rows) == 0 || len(rows[0]) == 0 {
		allRows, _, err := getRows(ctx, columnNames, "", 25)
		if err != nil {
			return nil, err
		}
		if len(allRows) == 0 || len(allRows[0]) == 0 {
			// There are truly zero history entries. Let's still display a table in this case rather than erroring out.
			allRows = make([]table.Row, 0)
			row := make([]string, 0)
			for range columnNames {
				row = append(row, " ")
			}
			allRows = append(allRows, row)
		}
		return makeTableColumns(ctx, columnNames, allRows)
	}

	// Calculate the minimum amount of space that we need for each column for the current actual search
	columnWidths := calculateColumnWidths(rows, len(columnNames))
	totalWidth := 20
	for i, name := range columnNames {
		columnWidths[i] = max(columnWidths[i], len(name))
		totalWidth += columnWidths[i]
	}

	// Calculate the maximum column width that is useful for each column if we search for the empty string
	if bigQueryResults == nil {
		bigRows, _, err := getRows(ctx, columnNames, "", 1000)
		if err != nil {
			return nil, err
		}
		bigQueryResults = bigRows
	}
	maximumColumnWidths := calculateColumnWidths(bigQueryResults, len(columnNames))

	// Get the actual terminal width. If we're below this, opportunistically add some padding aiming for the maximum column widths
	terminalWidth, _, err := getTerminalSize()
	if err != nil {
		return nil, fmt.Errorf("failed to get terminal size: %v", err)
	}
	for totalWidth < (terminalWidth - len(columnNames)) {
		prevTotalWidth := totalWidth
		for i := range columnNames {
			if columnWidths[i] < maximumColumnWidths[i]+5 {
				columnWidths[i] += 1
				totalWidth += 1
			}
		}
		if totalWidth == prevTotalWidth {
			break
		}
	}

	// And if we are too large from the initial query, let's shrink things to make the table fit. We'll use the heuristic of always shrinking the widest column.
	for totalWidth > terminalWidth {
		largestColumnIdx := -1
		largestColumnSize := -1
		for i := range columnNames {
			if columnWidths[i] > largestColumnSize {
				largestColumnIdx = i
				largestColumnSize = columnWidths[i]
			}
		}
		columnWidths[largestColumnIdx] -= 2
		totalWidth -= 2
	}

	// And finally, create some actual columns!
	columns := make([]table.Column, 0)
	for i, name := range columnNames {
		columns = append(columns, table.Column{Title: name, Width: columnWidths[i]})
	}
	return columns, nil
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func makeTable(ctx *context.Context, rows []table.Row) (table.Model, error) {
	config := hctx.GetConf(ctx)
	columns, err := makeTableColumns(ctx, config.DisplayedColumns, rows)
	if err != nil {
		return table.Model{}, err
	}
	km := table.KeyMap{
		LineUp: key.NewBinding(
			key.WithKeys("up", "alt+OA", "ctrl+p"),
			key.WithHelp("↑", "scroll up"),
		),
		LineDown: key.NewBinding(
			key.WithKeys("down", "alt+OB", "ctrl+n"),
			key.WithHelp("↓", "scroll down"),
		),
		PageUp: key.NewBinding(
			key.WithKeys("pgup"),
			key.WithHelp("pgup", "page up"),
		),
		PageDown: key.NewBinding(
			key.WithKeys("pgdown"),
			key.WithHelp("pgdn", "page down"),
		),
		GotoTop: key.NewBinding(
			key.WithKeys("home"),
			key.WithHelp("home", "go to start"),
		),
		GotoBottom: key.NewBinding(
			key.WithKeys("end"),
			key.WithHelp("end", "go to end"),
		),
	}
	_, terminalHeight, err := getTerminalSize()
	if err != nil {
		return table.Model{}, err
	}
	tableHeight := min(TABLE_HEIGHT, terminalHeight-12)
	t := table.New(
		table.WithColumns(columns),
		table.WithRows(rows),
		table.WithFocused(true),
		table.WithHeight(tableHeight),
		table.WithKeyMap(km),
	)

	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240")).
		BorderBottom(true).
		Bold(false)
	s.Selected = s.Selected.
		Foreground(lipgloss.Color("229")).
		Background(lipgloss.Color("57")).
		Bold(false)
	t.SetStyles(s)
	t.Focus()
	return t, nil
}

func deleteHistoryEntry(ctx *context.Context, entry data.HistoryEntry) error {
	db := hctx.GetDb(ctx)
	// Delete locally
	r := db.Model(&data.HistoryEntry{}).Where("device_id = ? AND end_time = ?", entry.DeviceId, entry.EndTime).Delete(&data.HistoryEntry{})
	if r.Error != nil {
		return r.Error
	}
	// Delete remotely
	dr := shared.DeletionRequest{
		UserId:   data.UserId(hctx.GetConf(ctx).UserSecret),
		SendTime: time.Now(),
	}
	dr.Messages.Ids = append(dr.Messages.Ids, shared.MessageIdentifier{Date: entry.EndTime, DeviceId: entry.DeviceId})
	return SendDeletionRequest(dr)
}

func TuiQuery(ctx *context.Context, initialQuery string) error {
	lipgloss.SetColorProfile(termenv.ANSI)
	rows, entries, err := getRows(ctx, hctx.GetConf(ctx).DisplayedColumns, initialQuery, PADDED_NUM_ENTRIES)
	if err != nil {
		if initialQuery != "" {
			// initialQuery is likely invalid in some way, let's just drop it
			return TuiQuery(ctx, "")
		}
		// Something else has gone wrong, crash
		return err
	}
	t, err := makeTable(ctx, rows)
	if err != nil {
		return err
	}
	p := tea.NewProgram(initialModel(ctx, t, entries, initialQuery), tea.WithOutput(os.Stderr))
	// Async: Retrieve additional entries from the backend
	go func() {
		err := RetrieveAdditionalEntriesFromRemote(ctx)
		if err != nil {
			p.Send(err)
		}
		p.Send(doneDownloadingMsg{})
	}()
	// Async: Process deletion requests
	go func() {
		err := ProcessDeletionRequests(ctx)
		if err != nil {
			p.Send(err)
		}
	}()
	// Async: Check for any banner from the server
	go func() {
		banner, err := GetBanner(ctx)
		if err != nil {
			if IsOfflineError(err) {
				p.Send(offlineMsg{})
			} else {
				p.Send(err)
			}
		}
		p.Send(bannerMsg{banner: string(banner)})
	}()
	// Blocking: Start the TUI
	_, err = p.Run()
	if err != nil {
		return err
	}
	if selectedCommand == "" && os.Getenv("HISHTORY_TERM_INTEGRATION") != "" {
		// Print out the initialQuery instead so that we don't clear the terminal
		selectedCommand = initialQuery
	}
	fmt.Printf("%s\n", strings.ReplaceAll(selectedCommand, "\\n", "\n"))
	return nil
}
