package tui

import "github.com/roeyazroel/linear-tui/internal/config"

// ThemeColor stores a Lip Gloss-compatible CSS color string.
type ThemeColor string

// Theme defines the color palette for the Charm TUI.
type Theme struct {
	Background    ThemeColor
	Foreground    ThemeColor
	Border        ThemeColor
	BorderFocus   ThemeColor
	SelectionText ThemeColor
	SelectionBg   ThemeColor
	HeaderBg      ThemeColor
	HeaderText    ThemeColor
	SecondaryText ThemeColor
	Accent        ThemeColor
	InputBg       ThemeColor

	StatusTodo       ThemeColor
	StatusInProgress ThemeColor
	StatusDone       ThemeColor
	StatusCanceled   ThemeColor
}

// LinearTheme is the default dark theme inspired by Linear.
var LinearTheme = Theme{
	Background:       "#121212",
	Foreground:       "#ebebf5",
	Border:           "#3c3c3c",
	BorderFocus:      "#5e6ad2",
	SelectionText:    "#ffffff",
	SelectionBg:      "#30324f",
	HeaderBg:         "#1e1e1e",
	HeaderText:       "#a0a0a0",
	SecondaryText:    "#787878",
	Accent:           "#5e6ad2",
	InputBg:          "#2a2a2a",
	StatusTodo:       "#8c8c8c",
	StatusInProgress: "#f2c94c",
	StatusDone:       "#5e6ad2",
	StatusCanceled:   "#ff5050",
}

// HighContrastTheme is a high contrast theme for improved legibility.
var HighContrastTheme = Theme{
	Background:       "#000000",
	Foreground:       "#ffffff",
	Border:           "#ffffff",
	BorderFocus:      "#ffff00",
	SelectionText:    "#000000",
	SelectionBg:      "#ffffff",
	HeaderBg:         "#000000",
	HeaderText:       "#ffffff",
	SecondaryText:    "#c8c8c8",
	Accent:           "#ffff00",
	InputBg:          "#1e1e1e",
	StatusTodo:       "#ffffff",
	StatusInProgress: "#ffff00",
	StatusDone:       "#00ff00",
	StatusCanceled:   "#ff0000",
}

// ColorBlindTheme is a color-blind friendly palette.
var ColorBlindTheme = Theme{
	Background:       "#101010",
	Foreground:       "#e6e6e6",
	Border:           "#4a4a4a",
	BorderFocus:      "#0072b2",
	SelectionText:    "#ffffff",
	SelectionBg:      "#263656",
	HeaderBg:         "#1c1c1c",
	HeaderText:       "#cfcfcf",
	SecondaryText:    "#9a9a9a",
	Accent:           "#0072b2",
	InputBg:          "#2a2a2a",
	StatusTodo:       "#999999",
	StatusInProgress: "#56b4e9",
	StatusDone:       "#009e73",
	StatusCanceled:   "#d55e00",
}

// ThemeRegistry maps theme identifiers to theme palettes.
var ThemeRegistry = map[string]Theme{
	config.ThemeLinear:       LinearTheme,
	config.ThemeHighContrast: HighContrastTheme,
	config.ThemeColorBlind:   ColorBlindTheme,
}

// ResolveTheme returns the theme for a given name, or the default theme.
func ResolveTheme(name string) Theme {
	if theme, ok := ThemeRegistry[name]; ok {
		return theme
	}
	return LinearTheme
}
