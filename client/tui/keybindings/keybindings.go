package keybindings

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
)

type SerializableKeyMap struct {
	Up                      []string
	Down                    []string
	PageUp                  []string
	PageDown                []string
	SelectEntry             []string
	SelectEntryAndChangeDir []string
	Left                    []string
	Right                   []string
	TableLeft               []string
	TableRight              []string
	DeleteEntry             []string
	Help                    []string
	Quit                    []string
	JumpStartOfInput        []string
	JumpEndOfInput          []string
	WordLeft                []string
	WordRight               []string
	HideColumns             []string
}

func prettifyKeyBinding(kb string) string {
	if kb == "up" {
		return "↑ "
	}
	if kb == "down" {
		return "↓ "
	}
	if kb == "left" {
		return "←"
	}
	if kb == "right" {
		return "→"
	}
	subs := [][]string{
		{"+left", "+← "},
		{"+right", "+→ "},
		{"+down", "+↓ "},
		{"+up", "+↑ "},
		{"pgdown", "pgdn"},
	}
	for _, sub := range subs {
		kb = strings.ReplaceAll(kb, sub[0], sub[1])
	}
	return kb
}

func (s SerializableKeyMap) ToKeyMap() KeyMap {
	if len(s.Up) == 0 {
		panic(fmt.Sprintf("%#v", s))
	}
	return KeyMap{
		Up: key.NewBinding(
			key.WithKeys(s.Up...),
			key.WithHelp(prettifyKeyBinding(s.Up[0]), "scroll up "),
		),
		Down: key.NewBinding(
			key.WithKeys(s.Down...),
			key.WithHelp(prettifyKeyBinding(s.Down[0]), "scroll down "),
		),
		PageUp: key.NewBinding(
			key.WithKeys(s.PageUp...),
			key.WithHelp(prettifyKeyBinding(s.PageUp[0]), "page up "),
		),
		PageDown: key.NewBinding(
			key.WithKeys(s.PageDown...),
			key.WithHelp(prettifyKeyBinding(s.PageDown[0]), "page down "),
		),
		SelectEntry: key.NewBinding(
			key.WithKeys(s.SelectEntry...),
			key.WithHelp(prettifyKeyBinding(s.SelectEntry[0]), "select an entry "),
		),
		SelectEntryAndChangeDir: key.NewBinding(
			key.WithKeys(s.SelectEntryAndChangeDir...),
			key.WithHelp(prettifyKeyBinding(s.SelectEntryAndChangeDir[0]), "select an entry and cd into that directory"),
		),
		Left: key.NewBinding(
			key.WithKeys(s.Left...),
			key.WithHelp(prettifyKeyBinding(s.Left[0]), "move left "),
		),
		Right: key.NewBinding(
			key.WithKeys(s.Right...),
			key.WithHelp(prettifyKeyBinding(s.Right[0]), "move right "),
		),
		TableLeft: key.NewBinding(
			key.WithKeys(s.TableLeft...),
			key.WithHelp(prettifyKeyBinding(s.TableLeft[0]), "scroll the table left "),
		),
		TableRight: key.NewBinding(
			key.WithKeys(s.TableRight...),
			key.WithHelp(prettifyKeyBinding(s.TableRight[0]), "scroll the table right "),
		),
		DeleteEntry: key.NewBinding(
			key.WithKeys(s.DeleteEntry...),
			key.WithHelp(prettifyKeyBinding(s.DeleteEntry[0]), "delete the highlighted entry "),
		),
		Help: key.NewBinding(
			key.WithKeys(s.Help...),
			key.WithHelp(prettifyKeyBinding(s.Help[0]), "help "),
		),
		Quit: key.NewBinding(
			key.WithKeys(s.Quit...),
			key.WithHelp(prettifyKeyBinding(s.Quit[0]), "exit hiSHtory "),
		),
		JumpStartOfInput: key.NewBinding(
			key.WithKeys(s.JumpStartOfInput...),
			key.WithHelp(prettifyKeyBinding(s.JumpStartOfInput[0]), "jump to the start of the input "),
		),
		JumpEndOfInput: key.NewBinding(
			key.WithKeys(s.JumpEndOfInput...),
			key.WithHelp(prettifyKeyBinding(s.JumpEndOfInput[0]), "jump to the end of the input "),
		),
		WordLeft: key.NewBinding(
			key.WithKeys(s.WordLeft...),
			key.WithHelp(prettifyKeyBinding(s.WordLeft[0]), "jump left one word "),
		),
		WordRight: key.NewBinding(
			key.WithKeys(s.WordRight...),
			key.WithHelp(prettifyKeyBinding(s.WordRight[0]), "jump right one word "),
		),
		HideColumns: key.NewBinding(
			key.WithKeys(s.HideColumns...),
			key.WithHelp(prettifyKeyBinding(s.HideColumns[0]), "hide all columns but the 'Command' one"),
		),
	}
}

func (s SerializableKeyMap) WithDefaults() SerializableKeyMap {
	if len(s.Up) == 0 {
		s.Up = DefaultKeyMap.Up.Keys()
	}
	if len(s.Down) == 0 {
		s.Down = DefaultKeyMap.Down.Keys()
	}
	if len(s.PageUp) == 0 {
		s.PageUp = DefaultKeyMap.PageUp.Keys()
	}
	if len(s.PageDown) == 0 {
		s.PageDown = DefaultKeyMap.PageDown.Keys()
	}
	if len(s.SelectEntry) == 0 {
		s.SelectEntry = DefaultKeyMap.SelectEntry.Keys()
	}
	if len(s.SelectEntryAndChangeDir) == 0 {
		s.SelectEntryAndChangeDir = DefaultKeyMap.SelectEntryAndChangeDir.Keys()
	}
	if len(s.Left) == 0 {
		s.Left = DefaultKeyMap.Left.Keys()
	}
	if len(s.Right) == 0 {
		s.Right = DefaultKeyMap.Right.Keys()
	}
	if len(s.TableLeft) == 0 {
		s.TableLeft = DefaultKeyMap.TableLeft.Keys()
	}
	if len(s.TableRight) == 0 {
		s.TableRight = DefaultKeyMap.TableRight.Keys()
	}
	if len(s.DeleteEntry) == 0 {
		s.DeleteEntry = DefaultKeyMap.DeleteEntry.Keys()
	}
	if len(s.Help) == 0 {
		s.Help = DefaultKeyMap.Help.Keys()
	}
	if len(s.Quit) == 0 {
		s.Quit = DefaultKeyMap.Quit.Keys()
	}
	if len(s.JumpStartOfInput) == 0 {
		s.JumpStartOfInput = DefaultKeyMap.JumpStartOfInput.Keys()
	}
	if len(s.JumpEndOfInput) == 0 {
		s.JumpEndOfInput = DefaultKeyMap.JumpEndOfInput.Keys()
	}
	if len(s.WordLeft) == 0 {
		s.WordLeft = DefaultKeyMap.WordLeft.Keys()
	}
	if len(s.WordRight) == 0 {
		s.WordRight = DefaultKeyMap.WordRight.Keys()
	}
	if len(s.HideColumns) == 0 {
		s.HideColumns = DefaultKeyMap.HideColumns.Keys()
	}
	return s
}

type KeyMap struct {
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
	JumpStartOfInput        key.Binding
	JumpEndOfInput          key.Binding
	WordLeft                key.Binding
	WordRight               key.Binding
	HideColumns             key.Binding
}

func (k KeyMap) ToSerializable() SerializableKeyMap {
	return SerializableKeyMap{
		Up:                      k.Up.Keys(),
		Down:                    k.Down.Keys(),
		PageUp:                  k.PageUp.Keys(),
		PageDown:                k.PageDown.Keys(),
		SelectEntry:             k.SelectEntry.Keys(),
		SelectEntryAndChangeDir: k.SelectEntryAndChangeDir.Keys(),
		Left:                    k.Left.Keys(),
		Right:                   k.Right.Keys(),
		TableLeft:               k.TableLeft.Keys(),
		TableRight:              k.TableRight.Keys(),
		DeleteEntry:             k.DeleteEntry.Keys(),
		Help:                    k.Help.Keys(),
		Quit:                    k.Quit.Keys(),
		JumpStartOfInput:        k.JumpStartOfInput.Keys(),
		JumpEndOfInput:          k.JumpEndOfInput.Keys(),
		WordLeft:                k.WordLeft.Keys(),
		WordRight:               k.WordRight.Keys(),
		HideColumns:             k.HideColumns.Keys(),
	}
}

var fakeTitleKeyBinding key.Binding = key.NewBinding(
	key.WithKeys(""),
	key.WithHelp("hiSHtory: Search your shell history", ""),
)

var fakeEmptyKeyBinding key.Binding = key.NewBinding(
	key.WithKeys(""),
	key.WithHelp("", ""),
)

func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{fakeTitleKeyBinding, k.Help}
}

func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{fakeTitleKeyBinding, k.Up, k.Left, k.SelectEntry, k.SelectEntryAndChangeDir},
		{fakeEmptyKeyBinding, k.Down, k.Right, k.DeleteEntry},
		{fakeEmptyKeyBinding, k.PageUp, k.TableLeft, k.Quit},
		{fakeEmptyKeyBinding, k.PageDown, k.TableRight, k.Help},
	}
}

type Binding struct {
	Keys []string `json:"keys"`
	Help key.Help `json:"help"`
}

var DefaultKeyMap = KeyMap{
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
	JumpStartOfInput: key.NewBinding(
		key.WithKeys("ctrl+a"),
		key.WithHelp("ctrl+a", "jump to the start of the input "),
	),
	JumpEndOfInput: key.NewBinding(
		key.WithKeys("ctrl+e"),
		key.WithHelp("ctrl+e", "jump to the end of the input "),
	),
	WordLeft: key.NewBinding(
		key.WithKeys("ctrl+left"),
		key.WithHelp("ctrl+left", "jump left one word "),
	),
	WordRight: key.NewBinding(
		key.WithKeys("ctrl+right"),
		key.WithHelp("ctrl+right", "jump right one word "),
	),
	HideColumns: key.NewBinding(
		key.WithKeys("ctrl+t"),
		key.WithHelp("ctrl+t", "hide all columns but the 'Command' one"),
	),
}
