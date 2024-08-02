package tui

import (
	"testing"

	"github.com/ddworken/hishtory/client/table"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/require"
)

func TestHideColumnsKeypressWithNilTable(t *testing.T) {
	// Pressing the hide-columns hotkey before the initial async query has
	// populated m.table must not crash the TUI.
	m := model{}
	require.Nil(t, m.table)
	require.NotPanics(t, func() {
		updated, _ := m.Update(tea.KeyMsg(tea.Key{Type: tea.KeyCtrlT}))
		require.Nil(t, updated.(model).table)
	})
}

func TestCalculateColumnWidthsWithExtraCells(t *testing.T) {
	// Rows built for a previous set of displayed columns can be wider than the
	// current column list (e.g. cached rows from before toggling hide-columns),
	// and must not cause an out-of-range panic.
	require.NotPanics(t, func() {
		widths := calculateColumnWidths([]table.Row{{"aa", "b", "cccc"}}, 1)
		require.Equal(t, []int{2}, widths)
	})
}

func TestCalculateWordBoundaries(t *testing.T) {
	require.Equal(t, []int{0, 3}, calculateWordBoundaries("foo"))
	require.Equal(t, []int{0, 3, 7}, calculateWordBoundaries("foo bar"))
	require.Equal(t, []int{0, 3, 7}, calculateWordBoundaries("foo-bar"))
	require.Equal(t, []int{0, 3, 7, 11}, calculateWordBoundaries("foo-bar baz"))
	require.Equal(t, []int{0, 3, 10, 16}, calculateWordBoundaries("foo-- -bar - baz"))
	require.Equal(t, []int{0, 3}, calculateWordBoundaries("foo    "))
}

func TestSanitizeEscapeCodes(t *testing.T) {
	require.Equal(t, "foo", sanitizeEscapeCodes("foo"))
	require.Equal(t, "foo\x1b[31mbar", sanitizeEscapeCodes("foo\x1b[31mbar"))
	require.Equal(t, "", sanitizeEscapeCodes("11;rgb:1c1c/1c1c/1c1c"))
	require.Equal(t, "foo  bar", sanitizeEscapeCodes("foo 11;rgb:1c1c/1c1c/1c1c bar"))
}
