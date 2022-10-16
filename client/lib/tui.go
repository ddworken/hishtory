package lib

import (
	"context"
	"fmt"

	_ "embed" // for embedding config.sh

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/ddworken/hishtory/client/data"
	"github.com/ddworken/hishtory/client/hctx"
)

var baseStyle = lipgloss.NewStyle().
	BorderStyle(lipgloss.NormalBorder()).
	BorderForeground(lipgloss.Color("240"))

type errMsg error

type model struct {
	ctx        *context.Context
	spinner    spinner.Model
	quitting   bool
	isLoading  bool
	selected   bool
	table      table.Model
	runQuery   string
	err        error
	queryInput textinput.Model
	banner     string
	isOffline  bool
}

type doneDownloadingMsg struct{}
type offlineMsg struct{}
type bannerMsg struct {
	banner string
}

func initialModel(ctx *context.Context, t table.Model) model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))
	queryInput := textinput.New()
	queryInput.Placeholder = "ls"
	queryInput.Focus()
	queryInput.CharLimit = 156
	queryInput.Width = 50
	return model{ctx: ctx, spinner: s, isLoading: true, table: t, runQuery: "", queryInput: queryInput}
}

func (m model) Init() tea.Cmd {
	return m.spinner.Tick
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.runQuery != "" {
		rows, err := getRows(m.ctx, m.runQuery)
		if err != nil {
			m.err = err
			return m, nil
		}
		m.table.SetRows(rows)
		m.table.SetCursor(0)
		m.runQuery = ""
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "enter":
			m.selected = true
			return m, tea.Quit
		default:
			t, cmd1 := m.table.Update(msg)
			m.table = t
			i, cmd2 := m.queryInput.Update(msg)
			m.queryInput = i
			m.runQuery = m.queryInput.Value()
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
	if m.isLoading {
		return fmt.Sprintf("\n\n   %s Loading hishtory entries from other devices... press q to quit\n\n", m.spinner.View())
	}
	if m.selected {
		return fmt.Sprintf("\n%s\n", m.table.SelectedRow()[4])
	}
	offlineWarning := ""
	if m.isOffline {
		offlineWarning = "Warning: failed to contact the hishtory backend (are you offline?), so some results may be stale\n\n"
	}
	return fmt.Sprintf("\n%s%s\nSearch Query: %s\n\n%s\n", offlineWarning, m.banner, m.queryInput.View(), baseStyle.Render(m.table.View()))
}

func getRows(ctx *context.Context, query string) ([]table.Row, error) {
	db := hctx.GetDb(ctx)
	data, err := data.Search(db, query, 25)
	if err != nil {
		return nil, err
	}
	var rows []table.Row
	for _, entry := range data {
		row := table.Row{entry.Hostname, entry.CurrentWorkingDirectory, entry.StartTime.Format("Jan 2 2006 15:04:05 MST"), fmt.Sprintf("%d", entry.ExitCode), entry.Command}
		rows = append(rows, row)
	}
	return rows, nil
}

func TuiQuery(ctx *context.Context) error {
	columns := []table.Column{
		{Title: "Hostname", Width: 25},
		{Title: "CWD", Width: 40},
		{Title: "Timestamp", Width: 25},
		{Title: "Exit Code", Width: 9},
		{Title: "Command", Width: 70},
	}
	rows, err := getRows(ctx, "")
	if err != nil {
		return err
	}
	t := table.New(
		table.WithColumns(columns),
		table.WithRows(rows),
		table.WithFocused(true),
		table.WithHeight(20),
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

	p := tea.NewProgram(initialModel(ctx, t))
	go func() {
		err := RetrieveAdditionalEntriesFromRemote(ctx)
		if err != nil {
			p.Send(err)
		}
		p.Send(doneDownloadingMsg{})
	}()
	go func() {
		banner, err := GetBanner(ctx, "TODO_WIRE_GIT_COMMIT_HERE")
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
	return nil
}
