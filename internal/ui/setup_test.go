package ui

import (
	"strings"
	"testing"

	"github.com/soundadam/soundprobe/internal/preferences"
)

func TestSetupChoosesDailyStations(t *testing.T) {
	setup := newSetupModel("test", preferences.DefaultConfig())
	view := setup.View().Content
	if !strings.Contains(view, "soundprobe test") {
		t.Fatal("setup screen does not show lowercase brand")
	}
	for _, expected := range []string{
		"NJU campus-internal service",
		"public Internet NDT7 measurement",
		"Tongji University · Shanghai",
		"Qilu University of Technology · Jinan, Shandong",
		"test.ustc.edu.cn",
	} {
		if !strings.Contains(view, expected) {
			t.Fatalf("setup view missing %q:\n%s", expected, view)
		}
	}
	if strings.Contains(view, "CERNET") {
		t.Fatalf("setup view includes unavailable CERNET station:\n%s", view)
	}
	setup.selected = map[string]bool{"tongji": true}
	modelValue, command := setup.Update(key("enter"))
	result := modelValue.(*setupModel)
	if command == nil || !result.done || result.config().DailyStations[0] != "tongji" {
		t.Fatalf("result = %#v", result.config())
	}
}

func TestSetupRejectsEmptySelection(t *testing.T) {
	setup := newSetupModel("test", preferences.DefaultConfig())
	setup.selected = map[string]bool{}
	modelValue, command := setup.Update(key("enter"))
	result := modelValue.(*setupModel)
	if command != nil || result.done {
		t.Fatal("enter accepted an empty daily station selection")
	}
	if !strings.Contains(result.View().Content, "Select at least one daily station") {
		t.Fatalf("missing empty-selection error:\n%s", result.View().Content)
	}
}
