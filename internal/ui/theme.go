package ui

import (
	"image/color"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// Charm / Teaway palette. Values match github.com/charmbracelet/huh ThemeCharm
// and Teaway's custom duration picker (title indigo, accent fuchsia, faint 243).
const (
	colorIndigoLight = "#5A56E0"
	colorIndigoDark  = "#7571F9"
	colorFuchsia     = "#F780E2"
	colorGreenLight  = "#02BA84"
	colorGreenDark   = "#02BF87"
	colorRedLight    = "#FF4672"
	colorRedDark     = "#ED567A"
	colorFaint       = "243"
	colorNormalLight = "235"
	colorNormalDark  = "252"
	colorBarLight    = "250"
	colorBarDark     = "238"
)

type theme struct {
	dark bool
}

func newTheme() theme {
	return theme{dark: true}
}

func (theme *theme) applyBackground(message tea.BackgroundColorMsg) {
	theme.dark = message.IsDark()
}

func (theme theme) titleColor() color.Color {
	if theme.dark {
		return lipgloss.Color(colorIndigoDark)
	}
	return lipgloss.Color(colorIndigoLight)
}

func (theme theme) accentColor() color.Color { return lipgloss.Color(colorFuchsia) }

func (theme theme) faintColor() color.Color { return lipgloss.Color(colorFaint) }

func (theme theme) optionColor() color.Color {
	if theme.dark {
		return lipgloss.Color(colorNormalDark)
	}
	return lipgloss.Color(colorNormalLight)
}

func (theme theme) barColor() color.Color {
	if theme.dark {
		return lipgloss.Color(colorBarDark)
	}
	return lipgloss.Color(colorBarLight)
}

func (theme theme) okColor() color.Color {
	if theme.dark {
		return lipgloss.Color(colorGreenDark)
	}
	return lipgloss.Color(colorGreenLight)
}

func (theme theme) badColor() color.Color {
	if theme.dark {
		return lipgloss.Color(colorRedDark)
	}
	return lipgloss.Color(colorRedLight)
}

func (theme theme) Title() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(theme.titleColor()).Bold(true)
}

func (theme theme) Accent() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(theme.accentColor())
}

func (theme theme) AccentBold() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(theme.accentColor()).Bold(true)
}

func (theme theme) Faint() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(theme.faintColor())
}

func (theme theme) Option() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(theme.optionColor())
}

func (theme theme) Bar() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(theme.barColor())
}

func (theme theme) OK() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(theme.okColor())
}

func (theme theme) Bad() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(theme.badColor())
}

func (theme theme) appTitle(version string) string {
	title := theme.Title().Render("pace")
	if strings.TrimSpace(version) == "" {
		return title
	}
	return title + "  " + theme.Faint().Render(version)
}

func (theme theme) help(keys ...string) string {
	return theme.Faint().Render(strings.Join(keys, "   "))
}

type listItem struct {
	label    string
	selected bool
	disabled bool
}

func (theme theme) renderList(items []listItem, cursor int) string {
	lines := make([]string, 0, len(items))
	for index, item := range items {
		lines = append(lines, theme.renderListRow(item, index == cursor))
	}
	return strings.Join(lines, "\n")
}

func (theme theme) renderListRow(item listItem, cursor bool) string {
	selector := "  "
	if cursor {
		selector = theme.Accent().Render("> ")
	}
	prefix := theme.Faint().Render("• ")
	label := theme.Option()
	switch {
	case item.disabled:
		prefix = theme.Faint().Render("– ")
		label = theme.Faint()
	case item.selected:
		prefix = theme.OK().Render("✓ ")
	}
	if cursor && !item.disabled {
		label = theme.Accent()
	}
	return theme.Bar().Render("│") + " " + selector + prefix + label.Render(item.label)
}

func joinBlocks(blocks ...string) string {
	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		block = strings.TrimRight(block, "\n")
		if block == "" {
			continue
		}
		parts = append(parts, block)
	}
	return strings.Join(parts, "\n\n")
}
