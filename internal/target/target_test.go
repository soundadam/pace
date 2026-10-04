package target

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/soundadam/pace/internal/model"
)

func TestExpandPreservesStationAndFamilyOrder(t *testing.T) {
	providers, err := Expand([]string{"nju-campus", "mlab", "tongji"}, FamilyDual)
	if err != nil {
		t.Fatal(err)
	}
	want := []model.Provider{
		model.ProviderNJUCampusIPv4,
		model.ProviderNJUCampusIPv6,
		model.ProviderMLab,
		model.ProviderTongjiIPv4,
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

func TestExpandRejectsUnknownRetiredStations(t *testing.T) {
	for _, id := range []string{"nju-edge", "qlu"} {
		if _, err := Expand([]string{id}, FamilyIPv4); err == nil {
			t.Fatalf("Expand() accepted retired target %q", id)
		}
	}
}

func TestExpandRejectsUnsupportedIPv6Station(t *testing.T) {
	if _, err := Expand([]string{"tongji"}, FamilyIPv6); err == nil {
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
	if got := Label(model.ProviderNJUCampusIPv6); got != "NJU Campus · IPv6" {
		t.Fatalf("label = %q", got)
	}
	if got := Label(model.ProviderMLab); got != "M-Lab" {
		t.Fatalf("label = %q", got)
	}
}

func TestCatalogOmitsRetiredStations(t *testing.T) {
	for _, station := range Stations() {
		if station.ID == "nju-edge" || station.ID == "qlu" {
			t.Fatalf("retired station %q remains in catalog", station.ID)
		}
	}
}

func TestProbeSelectedDoesNotProbeUnconfiguredStations(t *testing.T) {
	results := ProbeSelected(context.Background(), []string{"mlab"}, time.Second)
	if len(results) != 1 || results[0].StationID != "mlab" || results[0].Status != ProbeAutomatic {
		t.Fatalf("results = %#v", results)
	}
}
