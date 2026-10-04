package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/soundadam/pace/internal/buildinfo"
	"github.com/soundadam/pace/internal/cli"
	"github.com/soundadam/pace/internal/consent"
	"github.com/soundadam/pace/internal/helper"
	"github.com/soundadam/pace/internal/model"
	"github.com/soundadam/pace/internal/network"
	"github.com/soundadam/pace/internal/preferences"
	"github.com/soundadam/pace/internal/provider"
	"github.com/soundadam/pace/internal/provider/campus"
	"github.com/soundadam/pace/internal/provider/mlab"
	"github.com/soundadam/pace/internal/provider/networkquality"
	"github.com/soundadam/pace/internal/provider/ookla"
	"github.com/soundadam/pace/internal/storage"
	"github.com/soundadam/pace/internal/target"
)

func main() {
	historyDir, err := storage.DefaultHistoryDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "pace: determine history directory: %v\n", err)
		os.Exit(1)
	}

	consentPath, err := consent.DefaultPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "pace: determine consent path: %v\n", err)
		os.Exit(1)
	}

	preferencesPath, err := preferences.DefaultPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "pace: determine preferences path: %v\n", err)
		os.Exit(1)
	}

	helperResolver := helper.NewResolver()
	providers := map[model.Provider]provider.MeasurementProvider{}
	for _, station := range target.Stations() {
		if !station.TerminalSupported {
			continue
		}
		for _, spec := range []*target.Spec{station.IPv4, station.IPv6} {
			if spec == nil {
				continue
			}
			providers[spec.Provider] = campus.NewTarget(helperResolver, campus.Config{
				Provider:   spec.Provider,
				Label:      spec.Label,
				Family:     spec.Family,
				ServerName: spec.ServerName,
				ServerURL:  spec.ServerURL,
			})
		}
	}
	mlabRunner := mlab.New(helperResolver)
	providers[model.ProviderMLab] = mlabRunner
	appleRunner := networkquality.New()
	ooklaRunner := ookla.New()
	providers[model.ProviderApple] = appleRunner
	providers[model.ProviderOokla] = ooklaRunner
	measurementRunner := provider.SummaryRunner{
		ToolVersion: buildinfo.Version,
		Campus:      campus.New(helperResolver),
		MLab:        mlabRunner,
		Apple:       appleRunner,
		Ookla:       ooklaRunner,
		Providers:   providers,
		Snapshot:    network.Snapshot,
	}

	app := &cli.App{
		In:          os.Stdin,
		Out:         os.Stdout,
		Err:         os.Stderr,
		StdinTTY:    isTerminal(os.Stdin),
		StdoutTTY:   isTerminal(os.Stdout),
		Version:     buildinfo.Version,
		Runner:      measurementRunner,
		History:     storage.New(historyDir),
		Consent:     consent.New(consentPath),
		Preferences: preferences.New(preferencesPath),
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	exitCode := app.Execute(ctx, os.Args[1:])
	stop()
	os.Exit(exitCode)
}

func isTerminal(file *os.File) bool {
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
