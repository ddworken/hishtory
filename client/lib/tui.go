package lib

import (
	"context"
	"fmt"
	"os"
	"strings"

	_ "embed" // for embedding config.sh

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/ddworken/hishtory/client/data"
	"github.com/ddworken/hishtory/client/hctx"
	"github.com/muesli/termenv"
)

const TABLE_HEIGHT = 20
const PADDED_NUM_ENTRIES = TABLE_HEIGHT * 3

var selectedRow string = ""

var baseStyle = lipgloss.NewStyle().
	BorderStyle(lipgloss.NormalBorder()).
	BorderForeground(lipgloss.Color("240"))

type errMsg error

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
	// The number of entries in the table.
	numEntries int
	// Whether the user has hit enter to select an entry and the TUI is thus about to quit.
	selected bool

	// The search box for the query
	queryInput textinput.Model
	// The query to run. Reset to nil after it was run.
	runQuery *string
	// The previous query that was run.
	lastQuery string

	// Unrecoverable error.
	err error
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

func initialModel(ctx *context.Context, t table.Model, initialQuery string, numEntries int) model {
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
	return model{ctx: ctx, spinner: s, isLoading: true, table: t, runQuery: &initialQuery, queryInput: queryInput, numEntries: numEntries}
}

func (m model) Init() tea.Cmd {
	return m.spinner.Tick
}

func runQueryAndUpdateTable(m model) model {
	if m.runQuery != nil && *m.runQuery != m.lastQuery {
		rows, numEntries, err := getRows(m.ctx, *m.runQuery)
		if err != nil {
			m.searchErr = err
			return m
		} else {
			m.searchErr = nil
		}
		m.numEntries = numEntries
		m.table.SetRows(rows)
		m.table.SetCursor(0)
		m.lastQuery = *m.runQuery
		m.runQuery = nil
	}
	if m.table.Cursor() >= m.numEntries {
		// Ensure that we can't scroll past the end of the table
		m.table.SetCursor(m.numEntries - 1)
	}
	return m
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "enter":
			if m.numEntries != 0 {
				m.selected = true
			}
			return m, tea.Quit
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
			m = runQueryAndUpdateTable(m)
			return m, tea.Batch(cmd1, cmd2)
		}
	case errMsg:
		m.err = msg
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
	if m.err != nil {
		return fmt.Sprintf("An unrecoverable error occured: %v\n", m.err)
	}
	if m.selected {
		selectedRow = m.table.SelectedRow()[4]
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

func getRows(ctx *context.Context, query string) ([]table.Row, int, error) {
	db := hctx.GetDb(ctx)
	data, err := data.Search(db, query, PADDED_NUM_ENTRIES)
	if err != nil {
		return nil, 0, err
	}
	var rows []table.Row
	for i := 0; i < PADDED_NUM_ENTRIES; i++ {
		if i < len(data) {
			entry := data[i]
			entry.Command = strings.ReplaceAll(entry.Command, "\n", " ") // TODO: handle multi-line commands better here
			row := table.Row{entry.Hostname, entry.CurrentWorkingDirectory, entry.StartTime.Format("Jan 2 2006 15:04:05 MST"), fmt.Sprintf("%d", entry.ExitCode), entry.Command}
			rows = append(rows, row)
		} else {
			rows = append(rows, table.Row{})
		}
	}
	return rows, len(data), nil
}

func TuiQuery(ctx *context.Context, gitCommit, initialQuery string) error {
	lipgloss.SetColorProfile(termenv.ANSI)
	columns := []table.Column{
		{Title: "Hostname", Width: 25},
		{Title: "CWD", Width: 40},
		{Title: "Timestamp", Width: 25},
		{Title: "Exit Code", Width: 9},
		{Title: "Command", Width: 70},
	}
	rows, numEntries, err := getRows(ctx, initialQuery)
	if err != nil {
		return err
	}
	km := table.KeyMap{
		LineUp: key.NewBinding(
			key.WithKeys("up", "alt+OA"),
			key.WithHelp("↑", "scroll up"),
		),
		LineDown: key.NewBinding(
			key.WithKeys("down", "alt+OB"),
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
	t := table.New(
		table.WithColumns(columns),
		table.WithRows(rows),
		table.WithFocused(true),
		table.WithHeight(TABLE_HEIGHT),
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

	p := tea.NewProgram(initialModel(ctx, t, initialQuery, numEntries), tea.WithOutput(os.Stderr))
	go func() {
		err := RetrieveAdditionalEntriesFromRemote(ctx)
		if err != nil {
			p.Send(err)
		}
		p.Send(doneDownloadingMsg{})
	}()
	go func() {
		banner, err := GetBanner(ctx, gitCommit)
		if err != nil {
			if IsOfflineError(err) {
				p.Send(offlineMsg{})
			} else {
				p.Send(err)
			}
		}
		p.Send(bannerMsg{banner: string(banner)})
	}()
	err = p.Start()
	if err != nil {
		return err
	}
	fmt.Printf("%s\n", selectedRow)
	return nil
}
