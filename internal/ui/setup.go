package ui

import (
	"context"
	"errors"
	"io"

	tea "charm.land/bubbletea/v2"

	"github.com/soundadam/pace/internal/preferences"
	"github.com/soundadam/pace/internal/target"
)

var ErrSetupCancelled = errors.New("setup cancelled")

type setupModel struct {
	version   string
	language  preferences.Language
	screen    int
	stations  []target.Station
	cursor    int
	selected  map[string]bool
	done      bool
	cancelled bool
	errorText string
	theme     theme
}

func Configure(ctx context.Context, input io.Reader, output io.Writer, version string, current preferences.Config) (preferences.Config, error) {
	model := newSetupModel(version, current)
	program := tea.NewProgram(model, tea.WithContext(ctx), tea.WithInput(input), tea.WithOutput(output))
	finalModel, err := program.Run()
	if err != nil {
		return preferences.Config{}, err
	}
	setup, ok := finalModel.(*setupModel)
	if !ok {
		return preferences.Config{}, errors.New("setup returned an unexpected model")
	}
	if setup.cancelled {
		return preferences.Config{}, ErrSetupCancelled
	}
	if !setup.done {
		return preferences.Config{}, errors.New("setup exited without preferences")
	}
	return setup.config(), nil
}

func newSetupModel(version string, current preferences.Config) *setupModel {
	if current.Validate() != nil {
		current = preferences.DefaultConfig()
	}
	stations := make([]target.Station, 0)
	for _, station := range target.Stations() {
		if station.TerminalSupported && station.DailyEligible && target.PlatformAvailable(station) {
			stations = append(stations, station)
		}
	}
	selected := map[string]bool{}
	for _, id := range current.DailyStations {
		selected[id] = true
	}
	return &setupModel{version: version, language: current.Language, stations: stations, selected: selected, theme: newTheme()}
}

func (setup *setupModel) Init() tea.Cmd { return tea.RequestBackgroundColor }

func (setup *setupModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.BackgroundColorMsg:
		setup.theme.applyBackground(message)
		return setup, nil
	}
	key, ok := message.(tea.KeyPressMsg)
	if !ok {
		return setup, nil
	}
	setup.errorText = ""
	if key.String() == "ctrl+c" || key.String() == "q" || key.String() == "esc" {
		setup.cancelled = true
		return setup, tea.Quit
	}
	if setup.screen == 0 {
		switch key.String() {
		case "left", "right", "h", "l", "tab", "space", "up", "down", "j", "k":
			if setup.language == preferences.LanguageChinese {
				setup.language = preferences.LanguageEnglish
			} else {
				setup.language = preferences.LanguageChinese
			}
		case "enter":
			setup.screen = 1
		}
		return setup, nil
	}
	switch key.String() {
	case "up", "k":
		if setup.cursor > 0 {
			setup.cursor--
		}
	case "down", "j":
		if setup.cursor+1 < len(setup.stations) {
			setup.cursor++
		}
	case "space":
		id := setup.stations[setup.cursor].ID
		setup.selected[id] = !setup.selected[id]
	case "b":
		setup.screen = 0
	case "enter":
		if len(setup.selectedIDs()) == 0 {
			setup.errorText = setup.text("请至少选择一个日常测速站", "Select at least one daily station")
			return setup, nil
		}
		setup.done = true
		return setup, tea.Quit
	}
	return setup, nil
}

func (setup *setupModel) View() tea.View {
	if setup.done || setup.cancelled {
		return tea.NewView("")
	}
	if setup.screen == 0 {
		items := []listItem{
			{label: "中文", selected: setup.language == preferences.LanguageChinese},
			{label: "English", selected: setup.language == preferences.LanguageEnglish},
		}
		cursor := 0
		if setup.language == preferences.LanguageEnglish {
			cursor = 1
		}
		return tea.NewView(joinBlocks(
			setup.theme.appTitle(setup.version),
			setup.theme.Faint().Render("Choose interface language / 选择界面语言"),
			setup.theme.renderList(items, cursor),
			setup.theme.help("← →", "enter", "esc"),
		))
	}
	items := make([]listItem, 0, len(setup.stations))
	for _, station := range setup.stations {
		items = append(items, listItem{label: station.Label, selected: setup.selected[station.ID]})
	}
	header := joinBlocks(
		setup.theme.appTitle(setup.version),
		setup.theme.Faint().Render(setup.text("选择日常测速站", "Choose daily stations")),
	)
	detail := ""
	if setup.cursor >= 0 && setup.cursor < len(setup.stations) {
		station := setup.stations[setup.cursor]
		description, useCase := station.Description, station.UseCase
		if setup.language == preferences.LanguageChinese {
			description, useCase = station.DescriptionZH, station.UseCaseZH
		}
		detail = setup.theme.Faint().Render(description) + "\n" + setup.theme.Faint().Render(useCase)
	}
	help := setup.theme.help("space", "enter", "b") + "\n" + setup.theme.help("esc")
	blocks := []string{header, setup.theme.renderList(items, setup.cursor), detail, help}
	if setup.errorText != "" {
		blocks = append(blocks, setup.theme.Bad().Render(setup.errorText))
	}
	return tea.NewView(joinBlocks(blocks...))
}

func (setup *setupModel) selectedIDs() []string {
	ids := make([]string, 0, len(setup.selected))
	for _, station := range setup.stations {
		if setup.selected[station.ID] {
			ids = append(ids, station.ID)
		}
	}
	return ids
}

func (setup *setupModel) config() preferences.Config {
	return preferences.Config{SchemaVersion: preferences.SchemaVersion, Language: setup.language, DailyStations: setup.selectedIDs()}
}

func (setup *setupModel) text(chinese, english string) string {
	if setup.language == preferences.LanguageChinese {
		return chinese
	}
	return english
}
