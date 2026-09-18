package ui

import (
	"runtime"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/soundadam/soundprobe/internal/model"
	"github.com/soundadam/soundprobe/internal/preferences"
	"github.com/soundadam/soundprobe/internal/target"
)

func TestSelectorRecommendsCampusWhenReachable(t *testing.T) {
	selector := newSelectorModel("test", []target.ProbeResult{
		{StationID: "nju-campus", Family: "ipv4", Status: target.ProbeReachable},
		{StationID: "nju-edge", Family: "ipv4", Status: target.ProbeReachable},
	})
	appleExpected := runtime.GOOS == "darwin"
	if !selector.selected["nju-campus"] || !selector.selected["mlab"] || selector.selected["apple"] != appleExpected || selector.selected["ookla"] || selector.selected["nju-edge"] {
		t.Fatalf("selection = %#v", selector.selected)
	}
	view := selector.View().Content
	for _, expected := range []string{"soundprobe", "select measurement targets", "NJU Campus", "NJU Edge", "M-Lab", "[4] IPv4", "Space toggle"} {
		if !strings.Contains(view, expected) {
			t.Fatalf("view missing %q:\n%s", expected, view)
		}
	}
}

func TestSelectorDoesNotRecommendUnsupportedEdge(t *testing.T) {
	selector := newSelectorModel("test", []target.ProbeResult{
		{StationID: "nju-campus", Family: "ipv4", Status: target.ProbeUnreachable},
		{StationID: "nju-edge", Family: "ipv4", Status: target.ProbeReachable},
	})
	appleExpected := runtime.GOOS == "darwin"
	if selector.selected["nju-campus"] || selector.selected["nju-edge"] || !selector.selected["mlab"] || selector.selected["apple"] != appleExpected {
		t.Fatalf("selection = %#v", selector.selected)
	}
}

func TestConfiguredSelectorShowsOnlyDailyStationsInPriorityOrder(t *testing.T) {
	selector := newSelectorModelConfigured("test", []target.ProbeResult{
		{StationID: "tongji", Family: "ipv4", Status: target.ProbeReachable},
	}, preferences.Config{SchemaVersion: preferences.SchemaVersion, DailyStations: []string{"tongji", "qlu"}})
	if len(selector.stations) != 2 || selector.stations[0].ID != "tongji" || selector.stations[1].ID != "qlu" {
		t.Fatalf("stations = %#v", selector.stations)
	}
	view := selector.View().Content
	if !strings.Contains(view, "select measurement targets") || !strings.Contains(view, "Tongji University · Shanghai") || strings.Contains(view, "M-Lab") {
		t.Fatalf("configured view:\n%s", view)
	}
}

func TestSelectorBuildsDualStackPlan(t *testing.T) {
	selector := newSelectorModel("test", nil)
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
	selector := newSelectorModel("test", nil)
	selector.selected["qlu"] = true
	_, _ = selector.Update(key("6"))
	if selector.selected["qlu"] {
		t.Fatalf("IPv4-only station remained selected: %#v", selector.selected)
	}
}

func key(value string) tea.KeyPressMsg {
	runeValue := rune(value[0])
	if value == "enter" {
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter})
	}
	return tea.KeyPressMsg(tea.Key{Text: value, Code: runeValue})
}
