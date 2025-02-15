// Forked from https://github.com/charmbracelet/bubbles/blob/master/table/table.go to add horizontal scrolling
// Also includes https://github.com/charmbracelet/bubbles/pull/397/files to support cell styling

package table

import (
	"context"
	"strings"
	"time"

	"github.com/ddworken/hishtory/client/hctx"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/dgraph-io/ristretto"
	"github.com/eko/gocache/lib/v4/cache"
	"github.com/eko/gocache/lib/v4/store"
	ristretto_store "github.com/eko/gocache/store/ristretto/v4"
	"github.com/mattn/go-runewidth"
)

// Model defines a state for the table widget.
type Model struct {
	KeyMap KeyMap

	cols   []Column
	rows   []Row
	cursor int
	focus  bool
	styles Styles

	viewport viewport.Model
	start    int
	end      int

	hcol    int
	hstep   int
	hcursor int
}

// CellPosition holds row and column indexes.
type CellPosition struct {
	RowID         int
	Column        int
	IsRowSelected bool
}

// Row represents one line in the table.
type Row []string

// Column defines the table structure.
type Column struct {
	Title string
	Width int
}

// KeyMap defines keybindings. It satisfies to the help.KeyMap interface, which
// is used to render the menu menu.
type KeyMap struct {
	LineUp       key.Binding
	LineDown     key.Binding
	PageUp       key.Binding
	PageDown     key.Binding
	HalfPageUp   key.Binding
	HalfPageDown key.Binding
	GotoTop      key.Binding
	GotoBottom   key.Binding
	MoveLeft     key.Binding
	MoveRight    key.Binding
}

// DefaultKeyMap returns a default set of keybindings.
func DefaultKeyMap() KeyMap {
	const spacebar = " "
	return KeyMap{
		LineUp: key.NewBinding(
			key.WithKeys("up", "k"),
			key.WithHelp("↑/k", "up"),
		),
		LineDown: key.NewBinding(
			key.WithKeys("down", "j"),
			key.WithHelp("↓/j", "down"),
		),
		PageUp: key.NewBinding(
			key.WithKeys("b", "pgup"),
			key.WithHelp("b/pgup", "page up"),
		),
		PageDown: key.NewBinding(
			key.WithKeys("f", "pgdown", spacebar),
			key.WithHelp("f/pgdn", "page down"),
		),
		HalfPageUp: key.NewBinding(
			key.WithKeys("u", "ctrl+u"),
			key.WithHelp("u", "½ page up"),
		),
		HalfPageDown: key.NewBinding(
			key.WithKeys("d", "ctrl+d"),
			key.WithHelp("d", "½ page down"),
		),
		GotoTop: key.NewBinding(
			key.WithKeys("home", "g"),
			key.WithHelp("g/home", "go to start"),
		),
		GotoBottom: key.NewBinding(
			key.WithKeys("end", "G"),
			key.WithHelp("G/end", "go to end"),
		),
		MoveLeft: key.NewBinding(
			key.WithKeys("shift+left"),
			key.WithHelp("Shift+←", "move left"),
		),
		MoveRight: key.NewBinding(
			key.WithKeys("shift+right"),
			key.WithHelp("Shift+→", "move right"),
		),
	}
}

// Styles contains style definitions for this list component. By default, these
// values are generated by DefaultStyles.
type Styles struct {
	Header   lipgloss.Style
	Cell     lipgloss.Style
	Selected lipgloss.Style

	// RenderCell is a low-level primitive for stylizing cells.
	// It is responsible for rendering the selection style. Styles.Cell is ignored.
	//
	// Example implementation:
	// s.RenderCell = func(model table.Model, value string, position table.CellPosition) string {
	// 	cellStyle := s.Cell.Copy()
	//
	// 	switch {
	// 	case position.IsRowSelected:
	// 		return cellStyle.Background(lipgloss.Color("57")).Render(value)
	// 	case position.Column == 1:
	// 		return cellStyle.Foreground(lipgloss.Color("21")).Render(value)
	// 	default:
	// 		return cellStyle.Render(value)
	// 	}
	// }
	RenderCell func(model Model, value string, position CellPosition) string
}

func (s Styles) renderCell(model Model, value string, position CellPosition) string {
	if s.RenderCell != nil {
		return s.RenderCell(model, value, position)
	}

	return s.Cell.Render(value)
}

// DefaultStyles returns a set of default style definitions for this table.
func DefaultStyles() Styles {
	return Styles{
		Selected: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212")),
		Header:   lipgloss.NewStyle().Bold(true).Padding(0, 1),
		Cell:     lipgloss.NewStyle().Padding(0, 1),
	}
}

// SetStyles sets the table styles.
func (m *Model) SetStyles(s Styles) {
	m.styles = s
	m.UpdateViewport()
}

// Option is used to set options in New. For example:
//
//	table := New(WithColumns([]Column{{Title: "ID", Width: 10}}))
type Option func(*Model)

// New creates a new model for the table widget.
func New(opts ...Option) Model {
	m := Model{
		cursor:   0,
		viewport: viewport.New(0, 20),

		KeyMap: DefaultKeyMap(),
		styles: DefaultStyles(),

		hcol:    -1,
		hstep:   10,
		hcursor: 0,
	}

	for _, opt := range opts {
		opt(&m)
	}

	m.UpdateViewport()

	return m
}

// WithColumns sets the table columns (headers).
func WithColumns(cols []Column) Option {
	return func(m *Model) {
		m.cols = cols
	}
}

// WithRows sets the table rows (data).
func WithRows(rows []Row) Option {
	return func(m *Model) {
		m.rows = rows
	}
}

// WithHeight sets the height of the table.
func WithHeight(h int) Option {
	return func(m *Model) {
		m.viewport.Height = h
	}
}

// WithWidth sets the width of the table.
func WithWidth(w int) Option {
	return func(m *Model) {
		m.viewport.Width = w
	}
}

// WithFocused sets the focus state of the table.
func WithFocused(f bool) Option {
	return func(m *Model) {
		m.focus = f
	}
}

// WithStyles sets the table styles.
func WithStyles(s Styles) Option {
	return func(m *Model) {
		m.styles = s
	}
}

// WithKeyMap sets the key map.
func WithKeyMap(km KeyMap) Option {
	return func(m *Model) {
		m.KeyMap = km
	}
}

// Update is the Bubble Tea update loop.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	if !m.focus {
		return m, nil
	}

	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, m.KeyMap.LineUp):
			m.MoveUp(1)
		case key.Matches(msg, m.KeyMap.LineDown):
			m.MoveDown(1)
		case key.Matches(msg, m.KeyMap.PageUp):
			m.MoveUp(m.viewport.Height)
		case key.Matches(msg, m.KeyMap.PageDown):
			m.MoveDown(m.viewport.Height)
		case key.Matches(msg, m.KeyMap.HalfPageUp):
			m.MoveUp(m.viewport.Height / 2)
		case key.Matches(msg, m.KeyMap.HalfPageDown):
			m.MoveDown(m.viewport.Height / 2)
		case key.Matches(msg, m.KeyMap.LineDown):
			m.MoveDown(1)
		case key.Matches(msg, m.KeyMap.GotoTop):
			m.GotoTop()
		case key.Matches(msg, m.KeyMap.GotoBottom):
			m.GotoBottom()
		case key.Matches(msg, m.KeyMap.MoveLeft):
			m.MoveLeft(m.hstep)
		case key.Matches(msg, m.KeyMap.MoveRight):
			m.MoveRight(m.hstep)
		}
	}

	return m, tea.Batch(cmds...)
}

// Focused returns the focus state of the table.
func (m Model) Focused() bool {
	return m.focus
}

// Focus focusses the table, allowing the user to move around the rows and
// interact.
func (m *Model) Focus() {
	m.focus = true
	m.UpdateViewport()
}

// Blur blurs the table, preventing selection or movement.
func (m *Model) Blur() {
	m.focus = false
	m.UpdateViewport()
}

// View renders the component.
func (m Model) View() string {
	return m.headersView() + "\n" + m.viewport.View()
}

// UpdateViewport updates the list content based on the previously defined
// columns and rows.
func (m *Model) UpdateViewport() {
	renderedRows := make([]string, 0, len(m.rows))

	// Render only rows from: m.cursor-m.viewport.Height to: m.cursor+m.viewport.Height
	// Constant runtime, independent of number of rows in a table.
	// Limits the number of renderedRows to a maximum of 2*m.viewport.Height
	if m.cursor >= 0 {
		m.start = clamp(m.cursor-m.viewport.Height, 0, m.cursor)
	} else {
		m.start = 0
	}
	m.end = clamp(m.cursor+m.viewport.Height, m.cursor, len(m.rows))
	for i := m.start; i < m.end; i++ {
		renderedRows = append(renderedRows, m.renderRow(i))
	}

	m.viewport.SetContent(
		lipgloss.JoinVertical(lipgloss.Left, renderedRows...),
	)
}

// SelectedRow returns the selected row.
// You can cast it to your own implementation.
func (m Model) SelectedRow() Row {
	return m.rows[m.cursor]
}

// Rows returns the current rows.
func (m Model) Rows() []Row {
	return m.rows
}

// SetRows set a new rows state.
func (m *Model) SetRows(r []Row) {
	m.rows = r
	m.UpdateViewport()
}

// SetColumns set a new columns state.
func (m *Model) SetColumns(c []Column) {
	m.cols = c
	m.UpdateViewport()
}

// ColIndex gets the index of a column n, where if n is positive it returns n clamped, and if n is negative it reutrns the column index counting from the right
func (m *Model) ColIndex(n int) int {
	if n < 0 {
		return clamp(len(m.cols)-n, 0, len(m.cols)-1)
	} else {
		return clamp(n, 0, len(m.cols)-1)
	}
}

var RUNE_WIDTH_CACHE *cache.LoadableCache[int]

func RuneWidthWithCache(s string) int {
	if RUNE_WIDTH_CACHE == nil {
		loadFunction := func(ctx context.Context, key any) (int, []store.Option, error) {
			s := key.(string)
			r := runewidth.StringWidth(s)
			return r, []store.Option{store.WithCost(1), store.WithExpiration(time.Second * 3)}, nil
		}

		ristrettoCache, err := ristretto.NewCache(&ristretto.Config{
			NumCounters: 1000,
			MaxCost:     1000,
			BufferItems: 64,
		})
		if err != nil {
			hctx.GetLogger().Warnf("unexpected error: failed to create cache for rune width: %v", err)
			return runewidth.StringWidth(s)
		}
		ristrettoStore := ristretto_store.NewRistretto(ristrettoCache)

		cacheManager := cache.NewLoadable[int](
			loadFunction,
			cache.New[int](ristrettoStore),
		)
		RUNE_WIDTH_CACHE = cacheManager
	}
	r, err := RUNE_WIDTH_CACHE.Get(context.Background(), s)
	if err != nil {
		hctx.GetLogger().Warnf("unexpected error: failed to query cache for rune width: %v", err)
		return runewidth.StringWidth(s)
	}
	return r
}

var RUNE_TRUNCATE_CACHE *cache.LoadableCache[string]

type runeTruncateRequest struct {
	s    string
	w    int
	tail string
}

func RuneTruncateWithCache(s string, w int, tail string) string {
	if RUNE_TRUNCATE_CACHE == nil {
		loadFunction := func(ctx context.Context, key any) (string, []store.Option, error) {
			r := key.(runeTruncateRequest)
			t := runewidth.Truncate(r.s, r.w, r.tail)
			return t, []store.Option{store.WithCost(1), store.WithExpiration(time.Second * 3)}, nil
		}

		ristrettoCache, err := ristretto.NewCache(&ristretto.Config{
			NumCounters: 1000,
			MaxCost:     1000,
			BufferItems: 64,
		})
		if err != nil {
			hctx.GetLogger().Warnf("unexpected error: failed to create cache for rune truncate: %v", err)
			return runewidth.Truncate(s, w, tail)
		}
		ristrettoStore := ristretto_store.NewRistretto(ristrettoCache)

		cacheManager := cache.NewLoadable[string](
			loadFunction,
			cache.New[string](ristrettoStore),
		)
		RUNE_TRUNCATE_CACHE = cacheManager
	}
	r, err := RUNE_TRUNCATE_CACHE.Get(context.Background(), runeTruncateRequest{s, w, tail})
	if err != nil {
		hctx.GetLogger().Warnf("unexpected error: failed to query cache for rune truncate: %v", err)
		return runewidth.Truncate(s, w, tail)
	}
	return r
}

// Gets the maximum useful horizontal scroll
func (m *Model) MaxHScroll() int {
	maxWidth := 0
	index := m.ColIndex(m.hcol)
	for _, row := range m.rows {
		for _, value := range row {
			maxWidth = max(RuneWidthWithCache(value), maxWidth)
		}
	}
	return max(maxWidth-m.cols[index].Width+2, 0)
}

// SetWidth sets the width of the viewport of the table.
func (m *Model) SetWidth(w int) {
	m.viewport.Width = w
	m.UpdateViewport()
}

// SetHeight sets the height of the viewport of the table.
func (m *Model) SetHeight(h int) {
	m.viewport.Height = h
	m.UpdateViewport()
}

// Height returns the viewport height of the table.
func (m Model) Height() int {
	return m.viewport.Height
}

// Width returns the viewport width of the table.
func (m Model) Width() int {
	return m.viewport.Width
}

// Cursor returns the index of the selected row.
func (m Model) Cursor() int {
	return m.cursor
}

// SetCursor sets the cursor position in the table.
func (m *Model) SetCursor(n int) {
	m.cursor = clamp(n, 0, len(m.rows)-1)
	m.UpdateViewport()
}

// MoveUp moves the selection up by any number of row.
// It can not go above the first row.
func (m *Model) MoveUp(n int) {
	m.cursor = clamp(m.cursor-n, 0, len(m.rows)-1)
	switch {
	case m.start == 0:
		m.viewport.SetYOffset(clamp(m.viewport.YOffset, 0, m.cursor))
	case m.start < m.viewport.Height:
		m.viewport.SetYOffset(clamp(m.viewport.YOffset+n, 0, m.cursor))
	case m.viewport.YOffset >= 1:
		m.viewport.YOffset = clamp(m.viewport.YOffset+n, 1, m.viewport.Height)
	}
	m.UpdateViewport()
}

// MoveDown moves the selection down by any number of row.
// It can not go below the last row.
func (m *Model) MoveDown(n int) {
	m.cursor = clamp(m.cursor+n, 0, len(m.rows)-1)
	m.UpdateViewport()

	switch {
	case m.end == len(m.rows):
		m.viewport.SetYOffset(clamp(m.viewport.YOffset-n, 1, m.viewport.Height))
	case m.cursor > (m.end-m.start)/2:
		m.viewport.SetYOffset(clamp(m.viewport.YOffset-n, 1, m.cursor))
	case m.viewport.YOffset > 1:
	case m.cursor > m.viewport.YOffset+m.viewport.Height-1:
		m.viewport.SetYOffset(clamp(m.viewport.YOffset+1, 0, 1))
	}
}

// GotoTop moves the selection to the first row.
func (m *Model) GotoTop() {
	m.MoveUp(m.cursor)
}

// GotoBottom moves the selection to the last row.
func (m *Model) GotoBottom() {
	m.MoveDown(len(m.rows))
}

// MoveLeft scrolls left
func (m *Model) MoveLeft(n int) {
	m.hcursor = clamp(m.hcursor-n, 0, m.MaxHScroll())
	m.UpdateViewport()
}

// MoveRight scrolls right
func (m *Model) MoveRight(n int) {
	m.hcursor = clamp(m.hcursor+n, 0, m.MaxHScroll())
	m.UpdateViewport()
}

// FromValues create the table rows from a simple string. It uses `\n` by
// default for getting all the rows and the given separator for the fields on
// each row.
func (m *Model) FromValues(value, separator string) {
	rows := []Row{}
	for _, line := range strings.Split(value, "\n") {
		r := Row{}
		for _, field := range strings.Split(line, separator) {
			r = append(r, field)
		}
		rows = append(rows, r)
	}

	m.SetRows(rows)
}

func (m Model) headersView() string {
	s := make([]string, 0, len(m.cols))
	for _, col := range m.cols {
		style := lipgloss.NewStyle().Width(col.Width).MaxWidth(col.Width).Inline(true)
		renderedCell := style.Render(RuneTruncateWithCache(col.Title, col.Width, "…"))
		s = append(s, m.styles.Header.Render(renderedCell))
	}
	return lipgloss.JoinHorizontal(lipgloss.Left, s...)
}

func (m *Model) columnNeedsScrolling(columnIdxToCheck int) bool {
	for rowIdx := m.start; rowIdx < m.end; rowIdx++ {
		for columnIdx, value := range m.rows[rowIdx] {
			if columnIdx == columnIdxToCheck && RuneWidthWithCache(value) > m.cols[columnIdx].Width {
				return true
			}
		}
	}
	return false
}

func (m *Model) renderRow(rowID int) string {
	isRowSelected := rowID == m.cursor
	s := make([]string, 0, len(m.cols))
	for i, value := range m.rows[rowID] {
		style := lipgloss.NewStyle().Width(m.cols[i].Width).MaxWidth(m.cols[i].Width).Inline(true)

		position := CellPosition{
			RowID:         rowID,
			Column:        i,
			IsRowSelected: isRowSelected,
		}

		var renderedCell string
		if m.columnNeedsScrolling(i) && m.hcursor > 0 {
			renderedCell = style.Render(RuneTruncateWithCache(runewidth.TruncateLeft(value, m.hcursor, "…"), m.cols[i].Width, "…"))
		} else {
			renderedCell = style.Render(RuneTruncateWithCache(value, m.cols[i].Width, "…"))
		}
		renderedCell = m.styles.renderCell(*m, renderedCell, position)
		s = append(s, renderedCell)
	}

	row := lipgloss.JoinHorizontal(lipgloss.Left, s...)

	if isRowSelected {
		return m.styles.Selected.Render(row)
	}

	return row
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

func clamp(v, low, high int) int {
	return min(max(v, low), high)
}
