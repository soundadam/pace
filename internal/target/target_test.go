package target

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/soundadam/soundprobe/internal/model"
)

func TestPlanForCommandAppliesTheRequestedFamily(t *testing.T) {
	tests := []struct {
		command model.Command
		family  Family
		want    []model.Provider
	}{
		{command: model.CommandCampus, family: FamilyIPv4, want: []model.Provider{model.ProviderNJUCampusIPv4}},
		{command: model.CommandCampus, family: FamilyIPv6, want: []model.Provider{model.ProviderNJUCampusIPv6}},
		{command: model.CommandCampus, family: FamilyDual, want: []model.Provider{model.ProviderNJUCampusIPv4, model.ProviderNJUCampusIPv6}},
		{command: model.CommandRun, family: FamilyIPv4, want: []model.Provider{model.ProviderNJUCampusIPv4, model.ProviderMLab, model.ProviderApple}},
		{command: model.CommandDomestic, family: FamilyIPv4, want: []model.Provider{model.ProviderTongjiIPv4, model.ProviderQLUIPv4}},
		{command: model.CommandMLab, family: FamilyIPv4, want: []model.Provider{model.ProviderMLab}},
		{command: model.CommandOokla, family: FamilyIPv4, want: []model.Provider{model.ProviderOokla}},
	}
	for _, test := range tests {
		t.Run(string(test.command)+"/"+string(test.family), func(t *testing.T) {
			plan, err := PlanForCommand(test.command, nil, test.family)
			if err != nil {
				t.Fatal(err)
			}
			if len(plan.Providers) != len(test.want) {
				t.Fatalf("providers = %#v, want %#v", plan.Providers, test.want)
			}
			for index := range test.want {
				if plan.Providers[index] != test.want[index] {
					t.Fatalf("providers = %#v, want %#v", plan.Providers, test.want)
				}
			}
		})
	}
}

func TestPlanForCommandRejectsNonDomesticStation(t *testing.T) {
	if _, err := PlanForCommand(model.CommandDomestic, []string{"nju-campus"}, FamilyIPv4); err == nil {
		t.Fatal("PlanForCommand() accepted a non-domestic station for `domestic`")
	}
	if _, err := PlanForCommand(model.CommandDomestic, []string{"cernet"}, FamilyIPv4); err != nil {
		t.Fatalf("PlanForCommand() rejected an explicit domestic station: %v", err)
	}
	if _, err := PlanForCommand(model.CommandDomestic, nil, FamilyIPv6); err == nil {
		t.Fatal("PlanForCommand() accepted IPv6 for `domestic`")
	}
}

func TestPlanForCommandRejectsUnknownCommand(t *testing.T) {
	if _, err := PlanForCommand(model.Command("nope"), nil, FamilyIPv4); err == nil {
		t.Fatal("PlanForCommand() accepted an unknown command")
	}
}

func TestExpandPreservesStationAndFamilyOrder(t *testing.T) {
	providers, err := Expand([]string{"nju-campus", "mlab", "qlu"}, FamilyDual)
	if err != nil {
		t.Fatal(err)
	}
	want := []model.Provider{
		model.ProviderNJUCampusIPv4,
		model.ProviderNJUCampusIPv6,
		model.ProviderMLab,
		model.ProviderQLUIPv4,
	}
	if len(providers) != len(want) {
		t.Fatalf("providers = %#v", providers)
	}
	for index := range want {
		if providers[index] != want[index] {
			t.Fatalf("providers[%d] = %q, want %q", index, providers[index], want[index])
		}
	}
}

func TestExpandRejectsBrowserProtectedEdge(t *testing.T) {
	if _, err := Expand([]string{"nju-edge"}, FamilyIPv4); err == nil {
		t.Fatal("Expand() accepted the browser-protected NJU Edge target")
	}
}

func TestExpandRejectsUnsupportedIPv6Station(t *testing.T) {
	if _, err := Expand([]string{"qlu"}, FamilyIPv6); err == nil {
		t.Fatal("Expand() succeeded for an IPv4-only station")
	}
}

func TestNewPlanDeduplicatesRepeatedStation(t *testing.T) {
	plan, err := NewPlan([]string{"mlab", "mlab", "nju-campus"}, FamilyIPv4)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Providers) != 2 || plan.Providers[0] != model.ProviderMLab || plan.Providers[1] != model.ProviderNJUCampusIPv4 {
		t.Fatalf("providers = %#v", plan.Providers)
	}
}

func TestAutomaticProvidersRunOnceForDualFamily(t *testing.T) {
	plan, err := NewPlan([]string{"nju-campus", "mlab", "apple", "ookla"}, FamilyDual)
	if err != nil {
		t.Fatal(err)
	}
	want := []model.Provider{model.ProviderNJUCampusIPv4, model.ProviderNJUCampusIPv6, model.ProviderMLab, model.ProviderApple, model.ProviderOokla}
	if len(plan.Providers) != len(want) {
		t.Fatalf("providers = %#v, want %#v", plan.Providers, want)
	}
	for index := range want {
		if plan.Providers[index] != want[index] {
			t.Fatalf("providers[%d] = %q, want %q", index, plan.Providers[index], want[index])
		}
	}
}

func TestProbeUsesConfiguredBackend(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/backend/empty.php" {
			t.Fatalf("path = %q", request.URL.Path)
		}
		response.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	result := probe(context.Background(), Spec{StationID: "test", Family: "ipv4", ServerURL: server.URL}, time.Second)
	if result.Status != ProbeReachable || result.LatencyMS == nil {
		t.Fatalf("result = %#v", result)
	}
}

func TestLabelsExposeStationAndFamily(t *testing.T) {
	if got := Label(model.ProviderNJUEdgeIPv6); got != "NJU Edge · IPv6" {
		t.Fatalf("label = %q", got)
	}
	if got := Label(model.ProviderMLab); got != "M-Lab" {
		t.Fatalf("label = %q", got)
	}
}

func TestProbeSelectedDoesNotProbeUnconfiguredStations(t *testing.T) {
	results := ProbeSelected(context.Background(), []string{"mlab"}, time.Second)
	if len(results) != 1 || results[0].StationID != "mlab" || results[0].Status != ProbeAutomatic {
		t.Fatalf("results = %#v", results)
	}
}
