package ui

import (
	"runtime"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	appkit "github.com/soundadam/appkit/go"

	"github.com/soundadam/pace/internal/model"
	"github.com/soundadam/pace/internal/preferences"
	"github.com/soundadam/pace/internal/target"
)

// testApp is a development build: it never checks for or offers updates.
var testApp = appkit.App{Name: "pace", Version: "test", Repo: "soundadam/pace"}

func TestSelectorRecommendsCampusWhenReachable(t *testing.T) {
	selector := newSelectorModel(testApp, []target.ProbeResult{
		{StationID: "nju-campus", Family: "ipv4", Status: target.ProbeReachable},
	})
	appleExpected := runtime.GOOS == "darwin"
	if !selector.selected["nju-campus"] || !selector.selected["mlab"] || selector.selected["apple"] != appleExpected || selector.selected["ookla"] {
		t.Fatalf("selection = %#v", selector.selected)
	}
	view := selector.View().Content
	for _, expected := range []string{"pace", "Select measurement targets", "NJU Campus", "M-Lab", "ipv4", ">", "✓", "space", "enter"} {
		if !strings.Contains(view, expected) {
			t.Fatalf("view missing %q:\n%s", expected, view)
		}
	}
	for _, banned := range []string{"NJU Edge", "QLU", "nju-edge", "qlu"} {
		if strings.Contains(view, banned) {
			t.Fatalf("removed station still visible %q:\n%s", banned, view)
		}
	}
	if !strings.Contains(view, "\x1b[") {
		t.Fatal("selector should keep ANSI color")
	}
}

func TestSelectorDoesNotRecommendUnreachableCampus(t *testing.T) {
	selector := newSelectorModel(testApp, []target.ProbeResult{
		{StationID: "nju-campus", Family: "ipv4", Status: target.ProbeUnreachable},
	})
	appleExpected := runtime.GOOS == "darwin"
	if selector.selected["nju-campus"] || !selector.selected["mlab"] || selector.selected["apple"] != appleExpected {
		t.Fatalf("selection = %#v", selector.selected)
	}
}

func TestConfiguredSelectorShowsOnlyDailyStationsInPriorityOrder(t *testing.T) {
	selector := newSelectorModelConfigured(testApp, []target.ProbeResult{
		{StationID: "tongji", Family: "ipv4", Status: target.ProbeReachable},
	}, preferences.Config{SchemaVersion: preferences.SchemaVersion, Language: preferences.LanguageChinese, DailyStations: []string{"tongji", "ookla"}})
	if len(selector.stations) != 2 || selector.stations[0].ID != "ookla" || selector.stations[1].ID != "tongji" {
		t.Fatalf("stations = %#v", selector.stations)
	}
	view := selector.View().Content
	if !strings.Contains(view, "选择测速站") || !strings.Contains(view, "Tongji") || strings.Contains(view, "M-Lab") {
		t.Fatalf("configured view:\n%s", view)
	}
}

func TestSelectorBuildsDualStackPlan(t *testing.T) {
	selector := newSelectorModel(testApp, nil)
	selector.selected = map[string]bool{"nju-campus": true, "mlab": true}
	_, _ = selector.Update(key("d"))
	modelValue, command := selector.Update(key("enter"))
	if command == nil {
		t.Fatal("enter did not quit")
	}
	result := modelValue.(*selectorModel)
	want := []model.Provider{model.ProviderNJUCampusIPv4, model.ProviderNJUCampusIPv6, model.ProviderMLab}
	if !result.done || len(result.plan.Providers) != len(want) {
		t.Fatalf("plan = %#v", result.plan)
	}
	for index := range want {
		if result.plan.Providers[index] != want[index] {
			t.Fatalf("provider[%d] = %q, want %q", index, result.plan.Providers[index], want[index])
		}
	}
	if result.View().Content != "" {
		t.Fatalf("completed selector was not cleared: %q", result.View().Content)
	}
}

func TestSelectorDeselectsIPv4OnlyStationForIPv6(t *testing.T) {
	selector := newSelectorModel(testApp, nil)
	selector.selected["tongji"] = true
	_, _ = selector.Update(key("6"))
	if selector.selected["tongji"] {
		t.Fatalf("IPv4-only station remained selected: %#v", selector.selected)
	}
	view := selector.View().Content
	if !strings.Contains(view, "–") {
		t.Fatalf("disabled station missing Teaway-style dash:\n%s", view)
	}
}

func TestSelectorHoldsTheMeasurementWhileUpdating(t *testing.T) {
	selector := newSelectorModel(testApp, nil)
	selector.selected = map[string]bool{"mlab": true}
	selector.updating = true
	if _, command := selector.Update(key("enter")); command != nil || selector.done || selector.errorText == "" {
		t.Fatalf("enter during an update: done=%v error=%q", selector.done, selector.errorText)
	}
	// The updater's last message has no follow-up: the update is over.
	_, _ = selector.Update(updaterMsg{})
	if _, command := selector.Update(key("enter")); command == nil || !selector.done {
		t.Fatalf("enter after the update: done=%v error=%q", selector.done, selector.errorText)
	}
}

func key(value string) tea.KeyPressMsg {
	runeValue := rune(value[0])
	if value == "enter" {
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter})
	}
	return tea.KeyPressMsg(tea.Key{Text: value, Code: runeValue})
}
