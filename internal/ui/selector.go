package ui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/soundadam/pace/internal/model"
	"github.com/soundadam/pace/internal/preferences"
	"github.com/soundadam/pace/internal/target"
)

var ErrSelectionCancelled = errors.New("measurement selection cancelled")

type selectorModel struct {
	version   string
	stations  []target.Station
	probes    map[string]target.ProbeResult
	family    target.Family
	cursor    int
	selected  map[string]bool
	plan      target.Plan
	done      bool
	cancelled bool
	errorText string
	language  preferences.Language
	theme     theme
}

func SelectPlan(ctx context.Context, input io.Reader, output io.Writer, version string) (target.Plan, error) {
	return selectPlan(ctx, input, output, version, preferences.Config{})
}

func SelectPlanConfigured(ctx context.Context, input io.Reader, output io.Writer, version string, config preferences.Config) (target.Plan, error) {
	return selectPlan(ctx, input, output, version, config)
}

func selectPlan(ctx context.Context, input io.Reader, output io.Writer, version string, config preferences.Config) (target.Plan, error) {
	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	var probes []target.ProbeResult
	if config.Validate() == nil {
		probes = target.ProbeSelected(probeCtx, config.DailyStations, 1500*time.Millisecond)
	} else {
		probes = target.ProbeAll(probeCtx, 1500*time.Millisecond)
	}
	cancel()
	model := newSelectorModelConfigured(version, probes, config)
	program := tea.NewProgram(
		model,
		tea.WithContext(ctx),
		tea.WithInput(input),
		tea.WithOutput(output),
	)
	finalModel, err := program.Run()
	if err != nil {
		return target.Plan{}, err
	}
	selector, ok := finalModel.(*selectorModel)
	if !ok {
		return target.Plan{}, errors.New("selector returned an unexpected model")
	}
	if selector.cancelled {
		return target.Plan{}, ErrSelectionCancelled
	}
	if !selector.done {
		return target.Plan{}, errors.New("selector exited without a plan")
	}
	return selector.plan, nil
}

func newSelectorModel(version string, probeResults []target.ProbeResult) *selectorModel {
	return newSelectorModelConfigured(version, probeResults, preferences.Config{})
}

func newSelectorModelConfigured(version string, probeResults []target.ProbeResult, config preferences.Config) *selectorModel {
	probes := make(map[string]target.ProbeResult, len(probeResults))
	for _, result := range probeResults {
		probes[probeKey(result.StationID, result.Family)] = result
	}
	stations := target.Stations()
	language := preferences.LanguageEnglish
	if config.Validate() == nil {
		language = config.Language
		allowed := map[string]bool{}
		for _, id := range config.DailyStations {
			allowed[id] = true
		}
		filtered := make([]target.Station, 0, len(allowed))
		for _, station := range stations {
			if allowed[station.ID] {
				filtered = append(filtered, station)
			}
		}
		stations = filtered
	}
	model := &selectorModel{
		version:  version,
		stations: stations,
		probes:   probes,
		family:   target.FamilyIPv4,
		selected: map[string]bool{},
		language: language,
		theme:    newTheme(),
	}
	model.applyRecommendation()
	return model
}

func (selector *selectorModel) Init() tea.Cmd {
	return tea.RequestBackgroundColor
}

func (selector *selectorModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.BackgroundColorMsg:
		selector.theme.applyBackground(message)
		return selector, nil
	}
	key, ok := message.(tea.KeyPressMsg)
	if !ok {
		return selector, nil
	}
	selector.errorText = ""
	switch key.String() {
	case "up", "k":
		if selector.cursor > 0 {
			selector.cursor--
		}
	case "down", "j":
		if selector.cursor+1 < len(selector.stations) {
			selector.cursor++
		}
	case "space":
		station := selector.stations[selector.cursor]
		if selector.stationSupported(station, selector.family) {
			selector.selected[station.ID] = !selector.selected[station.ID]
		} else {
			selector.errorText = selector.text(station.Label+" 不支持 "+string(selector.family), station.Label+" does not support "+string(selector.family))
		}
	case "a":
		selector.applyRecommendation()
	case "4":
		selector.setFamily(target.FamilyIPv4)
	case "6":
		selector.setFamily(target.FamilyIPv6)
	case "d":
		selector.setFamily(target.FamilyDual)
	case "enter":
		ids := selector.selectedIDs()
		if len(ids) == 0 {
			selector.errorText = selector.text("请至少选择一个测速站", "select at least one measurement target")
			return selector, nil
		}
		plan, err := target.NewPlan(ids, selector.family)
		if err != nil {
			selector.errorText = err.Error()
			return selector, nil
		}
		selector.plan = plan
		selector.done = true
		return selector, tea.Quit
	case "esc", "q", "ctrl+c":
		selector.cancelled = true
		return selector, tea.Quit
	}
	return selector, nil
}

func (selector *selectorModel) View() tea.View {
	if selector.done || selector.cancelled {
		return tea.NewView("")
	}
	items := make([]listItem, 0, len(selector.stations))
	for _, station := range selector.stations {
		items = append(items, listItem{
			label:    station.Label,
			selected: selector.selected[station.ID],
			disabled: !selector.stationSupported(station, selector.family),
		})
	}
	family := string(selector.family)
	if selector.family == target.FamilyDual {
		family = "dual"
	}
	header := joinBlocks(
		selector.theme.appTitle(selector.version),
		selector.theme.Faint().Render(selector.text("选择测速站", "Select measurement targets")),
		selector.theme.Option().Render(family),
	)
	body := selector.theme.renderList(items, selector.cursor)
	detail := selector.cursorDetail()
	help := selector.theme.help("space", "enter", "a", "4 6 d") + "\n" + selector.theme.help("esc")
	blocks := []string{header, body, detail, help}
	if selector.errorText != "" {
		blocks = append(blocks, selector.theme.Bad().Render(selector.errorText))
	}
	return tea.NewView(joinBlocks(blocks...))
}

func (selector *selectorModel) cursorDetail() string {
	if selector.cursor < 0 || selector.cursor >= len(selector.stations) {
		return ""
	}
	station := selector.stations[selector.cursor]
	description := station.Description
	if selector.language == preferences.LanguageChinese {
		description = station.DescriptionZH
	}
	status := selector.stationStatus(station)
	return selector.theme.Faint().Render(description) + "\n" + selector.theme.Faint().Render(status)
}

func (selector *selectorModel) selectedIDs() []string {
	ids := make([]string, 0, len(selector.selected))
	for _, station := range selector.stations {
		if selector.selected[station.ID] {
			ids = append(ids, station.ID)
		}
	}
	return ids
}

func (selector *selectorModel) setFamily(family target.Family) {
	selector.family = family
	for _, station := range selector.stations {
		if !selector.stationSupported(station, family) {
			delete(selector.selected, station.ID)
		}
	}
}

func (selector *selectorModel) applyRecommendation() {
	selector.selected = map[string]bool{}
	for _, station := range selector.stations {
		if !selector.stationSupported(station, selector.family) {
			continue
		}
		// Keep the daily recommendation education-network first: NJU Campus
		// when reachable, plus the automatic public references M-Lab and Apple.
		if station.ID == "nju-campus" && selector.stationReachable(station) {
			selector.selected[station.ID] = true
			continue
		}
		if station.MLab || station.AutoProvider == model.ProviderApple {
			selector.selected[station.ID] = true
		}
	}
	selector.setFamily(selector.family)
}

func (selector *selectorModel) stationReachable(station target.Station) bool {
	if station.MLab || station.AutoProvider != "" {
		return true
	}
	if selector.family == target.FamilyDual {
		return selector.reachable(station.ID, "ipv4") || selector.reachable(station.ID, "ipv6")
	}
	return selector.reachable(station.ID, string(selector.family))
}

func (selector *selectorModel) reachable(stationID, family string) bool {
	result, ok := selector.probes[probeKey(stationID, family)]
	return ok && result.Status == target.ProbeReachable
}

func (selector *selectorModel) stationSupported(station target.Station, family target.Family) bool {
	if !station.TerminalSupported || !target.PlatformAvailable(station) {
		return false
	}
	if station.MLab || station.AutoProvider != "" {
		return true
	}
	switch family {
	case target.FamilyIPv4:
		return station.IPv4 != nil
	case target.FamilyIPv6:
		return station.IPv6 != nil
	case target.FamilyDual:
		return station.IPv4 != nil || station.IPv6 != nil
	default:
		return false
	}
}

func (selector *selectorModel) stationStatus(station target.Station) string {
	if !station.TerminalSupported {
		return selector.text("终端不支持 · ", "terminal unsupported · ") + station.UnsupportedReason
	}
	if !target.PlatformAvailable(station) {
		return selector.text("当前系统不可用 · 仅 macOS", "unavailable on this OS · macOS only")
	}
	if station.MLab || station.AutoProvider != "" {
		switch station.AutoProvider {
		case model.ProviderApple:
			return selector.text("macOS 内置 networkQuality", "macOS built-in networkQuality")
		case model.ProviderOokla:
			return selector.text("需要官方 Ookla CLI", "requires official Ookla CLI")
		default:
			return selector.text("自动选择节点", "automatic node")
		}
	}
	families := []string{}
	switch selector.family {
	case target.FamilyIPv4:
		families = []string{"ipv4"}
	case target.FamilyIPv6:
		families = []string{"ipv6"}
	case target.FamilyDual:
		if station.IPv4 != nil {
			families = append(families, "ipv4")
		}
		if station.IPv6 != nil {
			families = append(families, "ipv6")
		}
	}
	parts := make([]string, 0, len(families))
	for _, family := range families {
		result, ok := selector.probes[probeKey(station.ID, family)]
		if !ok {
			parts = append(parts, family+" unknown")
			continue
		}
		status := string(result.Status)
		if result.LatencyMS != nil {
			status = fmt.Sprintf("%s %.0fms", status, *result.LatencyMS)
		}
		parts = append(parts, family+" "+status)
	}
	if len(parts) == 0 {
		return "unsupported"
	}
	sort.Strings(parts)
	return strings.Join(parts, " · ")
}

func (selector *selectorModel) text(chinese, english string) string {
	if selector.language == preferences.LanguageChinese {
		return chinese
	}
	return english
}

func probeKey(stationID, family string) string {
	return stationID + "|" + family
}
