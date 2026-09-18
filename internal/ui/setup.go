package ui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/soundadam/soundprobe/internal/preferences"
	"github.com/soundadam/soundprobe/internal/target"
)

var ErrSetupCancelled = errors.New("setup cancelled")

type setupModel struct {
	version   string
	stations  []target.Station
	cursor    int
	selected  map[string]bool
	done      bool
	cancelled bool
	errorText string
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
	return &setupModel{version: version, stations: stations, selected: selected}
}

func (setup *setupModel) Init() tea.Cmd { return nil }

func (setup *setupModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := message.(tea.KeyPressMsg)
	if !ok {
		return setup, nil
	}
	setup.errorText = ""
	if key.String() == "ctrl+c" || key.String() == "q" || key.String() == "esc" {
		setup.cancelled = true
		return setup, tea.Quit
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
	case "enter":
		if len(setup.selectedIDs()) == 0 {
			setup.errorText = "Select at least one daily station"
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
	lines := []string{
		fmt.Sprintf("soundprobe %s · choose daily stations", setup.version),
		"Only these stations appear in daily use. Run soundprobe setup to change them.",
		"",
	}
	for index, station := range setup.stations {
		cursor := "  "
		if index == setup.cursor {
			cursor = "› "
		}
		check := "[ ]"
		if setup.selected[station.ID] {
			check = "[x]"
		}
		lines = append(lines, fmt.Sprintf("%s%s %-12s %s", cursor, check, station.Label, station.Description), "      "+station.UseCase)
	}
	lines = append(lines, "",
		"Web tests (not daily CLI): NJU http://test.nju.edu.cn · USTC https://test.ustc.edu.cn",
		"",
		"↑/↓ move   Space toggle   Enter save   q cancel",
	)
	if setup.errorText != "" {
		lines = append(lines, setup.errorText)
	}
	return tea.NewView(strings.Join(lines, "\n"))
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
	return preferences.Config{SchemaVersion: preferences.SchemaVersion, DailyStations: setup.selectedIDs()}
}
