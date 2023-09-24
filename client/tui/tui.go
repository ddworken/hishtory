package tui

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	_ "embed" // for embedding config.sh

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/ddworken/hishtory/client/data"
	"github.com/ddworken/hishtory/client/hctx"
	"github.com/ddworken/hishtory/client/lib"
	"github.com/ddworken/hishtory/client/table"
	"github.com/ddworken/hishtory/shared"
	"github.com/muesli/termenv"
	"golang.org/x/term"
)

const TABLE_HEIGHT = 20
const PADDED_NUM_ENTRIES = TABLE_HEIGHT * 5

var CURRENT_QUERY_FOR_HIGHLIGHTING string = ""
var SELECTED_COMMAND string = ""

var baseStyle = lipgloss.NewStyle().
	BorderStyle(lipgloss.NormalBorder()).
	BorderForeground(lipgloss.Color("240"))

type keyMap struct {
	Up                      key.Binding
	Down                    key.Binding
	PageUp                  key.Binding
	PageDown                key.Binding
	SelectEntry             key.Binding
	SelectEntryAndChangeDir key.Binding
	Left                    key.Binding
	Right                   key.Binding
	TableLeft               key.Binding
	TableRight              key.Binding
	DeleteEntry             key.Binding
	Help                    key.Binding
	Quit                    key.Binding
}

var fakeTitleKeyBinding key.Binding = key.NewBinding(
	key.WithKeys(""),
	key.WithHelp("hiSHtory: Search your shell history", ""),
)

var fakeEmptyKeyBinding key.Binding = key.NewBinding(
	key.WithKeys(""),
	key.WithHelp("", ""),
)

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{fakeTitleKeyBinding, k.Help}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{fakeTitleKeyBinding, k.Up, k.Left, k.SelectEntry, k.SelectEntryAndChangeDir},
		{fakeEmptyKeyBinding, k.Down, k.Right, k.DeleteEntry},
		{fakeEmptyKeyBinding, k.PageUp, k.TableLeft, k.Quit},
		{fakeEmptyKeyBinding, k.PageDown, k.TableRight, k.Help},
	}
}

var keys = keyMap{
	Up: key.NewBinding(
		key.WithKeys("up", "alt+OA", "ctrl+p"),
		key.WithHelp("↑ ", "scroll up "),
	),
	Down: key.NewBinding(
		key.WithKeys("down", "alt+OB", "ctrl+n"),
		key.WithHelp("↓ ", "scroll down "),
	),
	PageUp: key.NewBinding(
		key.WithKeys("pgup"),
		key.WithHelp("pgup", "page up "),
	),
	PageDown: key.NewBinding(
		key.WithKeys("pgdown"),
		key.WithHelp("pgdn", "page down "),
	),
	SelectEntry: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "select an entry "),
	),
	SelectEntryAndChangeDir: key.NewBinding(
		key.WithKeys("ctrl+x"),
		key.WithHelp("ctrl+x", "select an entry and cd into that directory"),
	),
	Left: key.NewBinding(
		key.WithKeys("left"),
		key.WithHelp("← ", "move left "),
	),
	Right: key.NewBinding(
		key.WithKeys("right"),
		key.WithHelp("→ ", "move right "),
	),
	TableLeft: key.NewBinding(
		key.WithKeys("shift+left"),
		key.WithHelp("shift+← ", "scroll the table left "),
	),
	TableRight: key.NewBinding(
		key.WithKeys("shift+right"),
		key.WithHelp("shift+→ ", "scroll the table right "),
	),
	DeleteEntry: key.NewBinding(
		key.WithKeys("ctrl+k"),
		key.WithHelp("ctrl+k", "delete the highlighted entry "),
	),
	Help: key.NewBinding(
		key.WithKeys("ctrl+h"),
		key.WithHelp("ctrl+h", "help "),
	),
	Quit: key.NewBinding(
		key.WithKeys("esc", "ctrl+c", "ctrl+d"),
		key.WithHelp("esc", "exit hiSHtory "),
	),
}

type SelectStatus int64

const (
	NotSelected SelectStatus = iota
	Selected
	SelectedWithChangeDir
)

type model struct {
	// context
	ctx context.Context

	// Model for the loading spinner.
	spinner spinner.Model
	// Whether data is still loading and the spinner should still be displayed.
	isLoading bool

	// Model for the help bar at the bottom of the page
	help help.Model

	// Whether the TUI is quitting.
	quitting bool

	// The table used for displaying search results. Nil if the initial search query hasn't returned yet.
	table *table.Model
	// The entries in the table
	tableEntries []*data.HistoryEntry
	// Whether the user has hit enter to select an entry and the TUI is thus about to quit.
	selected SelectStatus

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
type asyncQueryFinishedMsg struct {
	rows                  []table.Row
	entries               []*data.HistoryEntry
	searchErr             error
	forceUpdateTable      bool
	maintainCursor        bool
	overriddenSearchQuery *string
}

func initialModel(ctx context.Context, initialQuery string) model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))
	queryInput := textinput.New()
	queryInput.Placeholder = "ls"
	queryInput.Focus()
	queryInput.CharLimit = 200
	width, _, err := getTerminalSize()
	if err == nil {
		queryInput.Width = width
	} else {
		hctx.GetLogger().Infof("getTerminalSize() return err=%#v, defaulting queryInput to a width of 50", err)
		queryInput.Width = 50
	}
	if initialQuery != "" {
		queryInput.SetValue(initialQuery)
	}
	CURRENT_QUERY_FOR_HIGHLIGHTING = initialQuery
	return model{ctx: ctx, spinner: s, isLoading: true, table: nil, tableEntries: []*data.HistoryEntry{}, runQuery: &initialQuery, queryInput: queryInput, help: help.New()}
}

func (m model) Init() tea.Cmd {
	return m.spinner.Tick
}

func updateTable(m model, rows []table.Row, entries []*data.HistoryEntry, searchErr error, forceUpdateTable, maintainCursor bool) model {
	if m.runQuery == nil {
		m.runQuery = &m.lastQuery
	}
	m.searchErr = searchErr
	if searchErr != nil {
		return m
	}
	m.tableEntries = entries
	initialCursor := 0
	if m.table != nil {
		initialCursor = m.table.Cursor()
	}
	if forceUpdateTable || m.table == nil {
		t, err := makeTable(m.ctx, rows)
		if err != nil {
			m.fatalErr = err
			return m
		}
		m.table = &t
	}
	m.table.SetRows(rows)
	if maintainCursor {
		m.table.SetCursor(initialCursor)
	} else {
		m.table.SetCursor(0)
	}
	m.lastQuery = *m.runQuery
	m.runQuery = nil
	if m.table.Cursor() >= len(m.tableEntries) {
		// Ensure that we can't scroll past the end of the table
		m.table.SetCursor(len(m.tableEntries) - 1)
	}
	return m
}

func preventTableOverscrolling(m model) {
	if m.table != nil {
		if m.table.Cursor() >= len(m.tableEntries) {
			// Ensure that we can't scroll past the end of the table
			m.table.SetCursor(len(m.tableEntries) - 1)
		}
	}
}

func runQueryAndUpdateTable(m model, forceUpdateTable, maintainCursor bool) tea.Cmd {
	if (m.runQuery != nil && *m.runQuery != m.lastQuery) || forceUpdateTable || m.searchErr != nil {
		query := m.lastQuery
		if m.runQuery != nil {
			query = *m.runQuery
		}
		return func() tea.Msg {
			rows, entries, searchErr := getRows(m.ctx, hctx.GetConf(m.ctx).DisplayedColumns, query, PADDED_NUM_ENTRIES)
			return asyncQueryFinishedMsg{rows, entries, searchErr, forceUpdateTable, maintainCursor, nil}
		}
	}
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, keys.Quit):
			m.quitting = true
			return m, tea.Quit
		case key.Matches(msg, keys.SelectEntry):
			if len(m.tableEntries) != 0 && m.table != nil {
				m.selected = Selected
			}
			return m, tea.Quit
		case key.Matches(msg, keys.SelectEntryAndChangeDir):
			if len(m.tableEntries) != 0 && m.table != nil {
				m.selected = SelectedWithChangeDir
			}
			return m, tea.Quit
		case key.Matches(msg, keys.DeleteEntry):
			if m.table == nil {
				return m, nil
			}
			err := deleteHistoryEntry(m.ctx, *m.tableEntries[m.table.Cursor()])
			if err != nil {
				m.fatalErr = err
				return m, nil
			}
			cmd := runQueryAndUpdateTable(m, true, true)
			preventTableOverscrolling(m)
			return m, cmd
		case key.Matches(msg, keys.Help):
			m.help.ShowAll = !m.help.ShowAll
			return m, nil
		default:
			pendingCommands := tea.Batch()
			if m.table != nil {
				t, cmd1 := m.table.Update(msg)
				m.table = &t
				if strings.HasPrefix(msg.String(), "alt+") {
					return m, tea.Batch(cmd1)
				}
				pendingCommands = tea.Batch(pendingCommands, cmd1)
			}
			i, cmd2 := m.queryInput.Update(msg)
			m.queryInput = i
			searchQuery := m.queryInput.Value()
			m.runQuery = &searchQuery
			CURRENT_QUERY_FOR_HIGHLIGHTING = searchQuery
			cmd3 := runQueryAndUpdateTable(m, false, false)
			preventTableOverscrolling(m)
			return m, tea.Batch(pendingCommands, cmd2, cmd3)
		}
	case tea.WindowSizeMsg:
		m.help.Width = msg.Width
		m.queryInput.Width = msg.Width
		cmd := runQueryAndUpdateTable(m, true, true)
		return m, cmd
	case offlineMsg:
		m.isOffline = true
		return m, nil
	case bannerMsg:
		m.banner = msg.banner
		return m, nil
	case doneDownloadingMsg:
		m.isLoading = false
		return m, nil
	case asyncQueryFinishedMsg:
		m = updateTable(m, msg.rows, msg.entries, msg.searchErr, msg.forceUpdateTable, msg.maintainCursor)
		if msg.overriddenSearchQuery != nil {
			m.queryInput.SetValue(*msg.overriddenSearchQuery)
		}
		return m, nil
	default:
		var cmd tea.Cmd
		if m.isLoading {
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		} else {
			if m.table != nil {
				t, cmd := m.table.Update(msg)
				m.table = &t
				return m, cmd
			}
			return m, nil
		}
	}
}

func (m model) View() string {
	if m.fatalErr != nil {
		return fmt.Sprintf("An unrecoverable error occured: %v\n", m.fatalErr)
	}
	if m.selected == Selected || m.selected == SelectedWithChangeDir {
		SELECTED_COMMAND = m.tableEntries[m.table.Cursor()].Command
		if m.selected == SelectedWithChangeDir {
			changeDir := m.tableEntries[m.table.Cursor()].CurrentWorkingDirectory
			SELECTED_COMMAND = "cd " + changeDir + "; " + SELECTED_COMMAND
		}
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
		warning += "Warning: failed to contact the hishtory backend (are you offline?), so some results may be stale"
	}
	if m.searchErr != nil {
		warning += fmt.Sprintf("Warning: failed to search: %v", m.searchErr)
	}
	warning += "\n"
	helpView := m.help.View(keys)
	return fmt.Sprintf("\n%s%s%s\nSearch Query: %s\n\n%s\n", loadingMessage, warning, m.banner, m.queryInput.View(), renderNullableTable(m)) + helpView
}

func renderNullableTable(m model) string {
	if m.table == nil {
		return strings.Repeat("\n", TABLE_HEIGHT+3)
	}
	return baseStyle.Render(m.table.View())
}

func getRows(ctx context.Context, columnNames []string, query string, numEntries int) ([]table.Row, []*data.HistoryEntry, error) {
	db := hctx.GetDb(ctx)
	config := hctx.GetConf(ctx)
	searchResults, err := lib.Search(ctx, db, query, numEntries)
	if err != nil {
		return nil, nil, err
	}
	var rows []table.Row
	var filteredData []*data.HistoryEntry
	lastCommand := ""
	for i := 0; i < numEntries; i++ {
		if i < len(searchResults) {
			entry := searchResults[i]
			if strings.TrimSpace(entry.Command) == strings.TrimSpace(lastCommand) && config.FilterDuplicateCommands {
				continue
			}
			entry.Command = strings.ReplaceAll(entry.Command, "\n", "\\n")
			row, err := lib.BuildTableRow(ctx, columnNames, *entry)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to build row for entry=%#v: %w", entry, err)
			}
			rows = append(rows, row)
			filteredData = append(filteredData, entry)
			lastCommand = entry.Command
		} else {
			rows = append(rows, table.Row{})
		}
	}
	return rows, filteredData, nil
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

func makeTableColumns(ctx context.Context, columnNames []string, rows []table.Row) ([]table.Column, error) {
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
		return nil, fmt.Errorf("failed to get terminal size: %w", err)
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

func makeTable(ctx context.Context, rows []table.Row) (table.Model, error) {
	config := hctx.GetConf(ctx)
	columns, err := makeTableColumns(ctx, config.DisplayedColumns, rows)
	if err != nil {
		return table.Model{}, err
	}
	km := table.KeyMap{
		LineUp:   keys.Up,
		LineDown: keys.Down,
		PageUp:   keys.PageUp,
		PageDown: keys.PageDown,
		GotoTop: key.NewBinding(
			key.WithKeys("home"),
			key.WithHelp("home", "go to start"),
		),
		GotoBottom: key.NewBinding(
			key.WithKeys("end"),
			key.WithHelp("end", "go to end"),
		),
		MoveLeft:  keys.TableLeft,
		MoveRight: keys.TableRight,
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
	if config.BetaMode {
		s.RenderCell = func(model table.Model, value string, position table.CellPosition) string {
			MATCH_NOTHING_REGEXP := regexp.MustCompile("a^")
			var re *regexp.Regexp
			CURRENT_QUERY_FOR_HIGHLIGHTING = strings.TrimSpace(CURRENT_QUERY_FOR_HIGHLIGHTING)
			if CURRENT_QUERY_FOR_HIGHLIGHTING == "" {
				re = MATCH_NOTHING_REGEXP
			} else {
				queryRegex := lib.MakeRegexFromQuery(CURRENT_QUERY_FOR_HIGHLIGHTING)
				r, err := regexp.Compile(queryRegex)
				if err != nil {
					// Failed to compile the regex for highlighting matches, this should never happen. In this
					// case, just use a regexp that matches nothing to ensure that the TUI doesn't crash.
					re = MATCH_NOTHING_REGEXP
				} else {
					re = r
				}
			}

			renderChunk := func(v string, isMatching, isLeftMost, isRightMost bool) string {
				hctx.GetLogger().Infof("Rendering chunk=%#v with isMatching=%#v", v, isMatching)
				baseStyle := lipgloss.NewStyle()
				if position.IsRowSelected {
					baseStyle = s.Selected.Copy()
				}
				if isLeftMost {
					baseStyle = baseStyle.PaddingLeft(1)
				}
				if isRightMost {
					baseStyle = baseStyle.PaddingRight(1)
				}
				if isMatching {
					baseStyle = baseStyle.Bold(true)
				}
				return baseStyle.Render(v)
			}

			matches := re.FindAllStringIndex(value, -1)
			if len(matches) == 0 {
				return renderChunk(value, false, true, true)
			}

			ret := ""
			lastIncludedIdx := 0
			for _, match := range re.FindAllStringIndex(value, -1) {
				matchStartIdx := match[0]
				matchEndIdx := match[1]
				beforeMatch := value[lastIncludedIdx:matchStartIdx]
				if beforeMatch != "" {
					ret += renderChunk(beforeMatch, false, lastIncludedIdx == 0, false)
				}
				match := value[matchStartIdx:matchEndIdx]
				ret += renderChunk(match, true, matchStartIdx == 0, matchEndIdx == len(value))
				lastIncludedIdx = matchEndIdx
			}
			if lastIncludedIdx != len(value) {
				ret += renderChunk(value[lastIncludedIdx:], false, false, true)
			}
			hctx.GetLogger().Infof("bolded=%#v, original=%#v", ret, s.Cell.Render(value))
			return ret
		}
	}
	t.SetStyles(s)
	t.Focus()
	return t, nil
}

func deleteHistoryEntry(ctx context.Context, entry data.HistoryEntry) error {
	db := hctx.GetDb(ctx)
	// Delete locally
	r := db.Model(&data.HistoryEntry{}).Where("device_id = ? AND end_time = ?", entry.DeviceId, entry.EndTime).Delete(&data.HistoryEntry{})
	if r.Error != nil {
		return r.Error
	}

	// Delete remotely
	config := hctx.GetConf(ctx)
	if config.IsOffline {
		return nil
	}
	dr := shared.DeletionRequest{
		UserId:   data.UserId(hctx.GetConf(ctx).UserSecret),
		SendTime: time.Now(),
	}
	dr.Messages.Ids = append(dr.Messages.Ids,
		shared.MessageIdentifier{DeviceId: entry.DeviceId, EndTime: entry.EndTime, EntryId: entry.EntryId},
	)
	return lib.SendDeletionRequest(dr)
}

func TuiQuery(ctx context.Context, initialQuery string) error {
	lipgloss.SetColorProfile(termenv.ANSI)
	p := tea.NewProgram(initialModel(ctx, initialQuery), tea.WithOutput(os.Stderr))
	// Async: Get the initial set of rows
	go func() {
		rows, entries, err := getRows(ctx, hctx.GetConf(ctx).DisplayedColumns, initialQuery, PADDED_NUM_ENTRIES)
		if err == nil || initialQuery == "" {
			p.Send(asyncQueryFinishedMsg{rows: rows, entries: entries, searchErr: err, forceUpdateTable: true, maintainCursor: false, overriddenSearchQuery: nil})
		} else {
			// initialQuery is likely invalid in some way, let's just drop it
			emptyQuery := ""
			rows, entries, err := getRows(ctx, hctx.GetConf(ctx).DisplayedColumns, emptyQuery, PADDED_NUM_ENTRIES)
			p.Send(asyncQueryFinishedMsg{rows: rows, entries: entries, searchErr: err, forceUpdateTable: true, maintainCursor: false, overriddenSearchQuery: &emptyQuery})
		}
	}()
	// Async: Retrieve additional entries from the backend
	go func() {
		err := lib.RetrieveAdditionalEntriesFromRemote(ctx)
		if err != nil {
			p.Send(err)
		}
		p.Send(doneDownloadingMsg{})
	}()
	// Async: Process deletion requests
	go func() {
		err := lib.ProcessDeletionRequests(ctx)
		if err != nil {
			p.Send(err)
		}
	}()
	// Async: Check for any banner from the server
	go func() {
		banner, err := lib.GetBanner(ctx)
		if err != nil {
			if lib.IsOfflineError(err) {
				p.Send(offlineMsg{})
			} else {
				p.Send(err)
			}
		}
		p.Send(bannerMsg{banner: string(banner)})
	}()
	// Blocking: Start the TUI
	_, err := p.Run()
	if err != nil {
		return err
	}
	if SELECTED_COMMAND == "" && os.Getenv("HISHTORY_TERM_INTEGRATION") != "" {
		// Print out the initialQuery instead so that we don't clear the terminal
		SELECTED_COMMAND = initialQuery
	}
	fmt.Printf("%s\n", strings.ReplaceAll(SELECTED_COMMAND, "\\n", "\n"))
	return nil
}

// TODO: support custom key bindings
// TODO: make the help page wrap
