package ui

import (
	"strings"
	"testing"

	"github.com/soundadam/pace/internal/preferences"
)

func TestSetupChoosesLanguageAndDailyStations(t *testing.T) {
	setup := newSetupModel("test", preferences.DefaultConfig())
	view := setup.View().Content
	if !strings.Contains(view, "pace") {
		t.Fatal("language screen does not show lowercase pace title")
	}
	if !strings.Contains(view, "中文") || !strings.Contains(view, "English") || !strings.Contains(view, ">") {
		t.Fatalf("language screen missing Teaway select chrome:\n%s", view)
	}
	if !strings.Contains(view, "\x1b[") {
		t.Fatal("setup should keep ANSI color")
	}
	_, _ = setup.Update(key("enter"))
	view = setup.View().Content
	for _, expected := range []string{"南京大学校内测速服务", "M-Lab", "Tongji", "✓"} {
		if !strings.Contains(view, expected) {
			t.Fatalf("setup view missing %q:\n%s", expected, view)
		}
	}
	for _, banned := range []string{"齐鲁工业大学", "QLU", "test.nju.edu.cn", "NJU Edge", "CERNET"} {
		if strings.Contains(view, banned) {
			t.Fatalf("setup view includes removed or unavailable station %q:\n%s", banned, view)
		}
	}
	setup.selected = map[string]bool{"tongji": true}
	modelValue, command := setup.Update(key("enter"))
	result := modelValue.(*setupModel)
	if command == nil || !result.done || result.config().DailyStations[0] != "tongji" {
		t.Fatalf("result = %#v", result.config())
	}
}
