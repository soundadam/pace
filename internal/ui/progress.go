package ui

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/soundadam/pace/internal/model"
	"github.com/soundadam/pace/internal/provider"
	"github.com/soundadam/pace/internal/target"
)

const (
	refreshInterval = 250 * time.Millisecond
	// Bubble Tea flushes views on a renderer loop independent of Update.
	// Keep the terminal state visible for several default renderer frames
	// before replacing it with the blank frame used to clear the inline block.
	finalFrameDelay = 50 * time.Millisecond
)

const (
	activityWidth   = 24
	detailRuneLimit = 58
)

type ProgressRenderer struct {
	program *tea.Program
	ready   chan struct{}
	done    chan struct{}
	stop    sync.Once
	errMu   sync.Mutex
	runErr  error
}

func NewProgressRenderer(output io.Writer, version string, targets []model.Provider) (*ProgressRenderer, error) {
	return newProgressRenderer(output, version, targets)
}

func newProgressRenderer(output io.Writer, version string, targets []model.Provider, options ...tea.ProgramOption) (*ProgressRenderer, error) {
	ready := make(chan struct{})
	progressModel := newProgressModel(version, targets, ready)
	programOptions := []tea.ProgramOption{
		tea.WithInput(nil),
		tea.WithOutput(output),
	}
	programOptions = append(programOptions, options...)
	program := tea.NewProgram(progressModel, programOptions...)
	renderer := &ProgressRenderer{
		program: program,
		ready:   ready,
		done:    make(chan struct{}),
	}
	go func() {
		_, err := program.Run()
		renderer.errMu.Lock()
		renderer.runErr = err
		renderer.errMu.Unlock()
		close(renderer.done)
	}()
	select {
	case <-ready:
		return renderer, nil
	case <-renderer.done:
		err := renderer.err()
		if err == nil {
			err = fmt.Errorf("progress renderer exited before initialization")
		}
		return nil, err
	}
}

func (renderer *ProgressRenderer) Update(event provider.ProgressEvent) {
	if renderer != nil && renderer.program != nil {
		renderer.program.Send(progressMessage(event))
	}
}

func (renderer *ProgressRenderer) Close() error {
	if renderer == nil || renderer.program == nil {
		return nil
	}
	renderer.stop.Do(func() {
		renderer.program.Send(stopMessage{})
	})
	<-renderer.done
	return renderer.err()
}

func (renderer *ProgressRenderer) err() error {
	renderer.errMu.Lock()
	defer renderer.errMu.Unlock()
	return renderer.runErr
}

type providerState struct {
	phase        provider.ProgressPhase
	test         string
	server       string
	downloadMbps *float64
	uploadMbps   *float64
	message      string
	startedAt    time.Time
	endedAt      time.Time
}

type progressModel struct {
	version   string
	startedAt time.Time
	now       time.Time
	network   model.NetworkContext
	providers map[model.Provider]providerState
	order     []model.Provider
	ready     chan struct{}
	blank     bool
	theme     theme
}

type progressMessage provider.ProgressEvent
type tickMessage time.Time
type stopMessage struct{}
type clearMessage struct{}

func newProgressModel(version string, targets []model.Provider, ready chan struct{}) *progressModel {
	order := append([]model.Provider(nil), targets...)
	if len(order) == 0 {
		order = []model.Provider{model.ProviderNJUCampusIPv4, model.ProviderMLab, model.ProviderApple}
	}
	states := make(map[model.Provider]providerState, len(order))
	for _, name := range order {
		states[name] = providerState{phase: provider.ProgressWaiting}
	}
	now := time.Now()
	return &progressModel{
		version:   version,
		startedAt: now,
		now:       now,
		providers: states,
		order:     order,
		ready:     ready,
		theme:     newTheme(),
	}
}

func (progress *progressModel) Init() tea.Cmd {
	close(progress.ready)
	return tea.Batch(tick(), tea.RequestBackgroundColor)
}

func tick() tea.Cmd {
	return tea.Tick(refreshInterval, func(now time.Time) tea.Msg {
		return tickMessage(now)
	})
}

func (progress *progressModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.BackgroundColorMsg:
		progress.theme.applyBackground(message)
		return progress, nil
	case progressMessage:
		progress.applyEvent(provider.ProgressEvent(message))
		return progress, nil
	case tickMessage:
		progress.now = time.Time(message)
		return progress, tick()
	case stopMessage:
		return progress, tea.Tick(finalFrameDelay, func(time.Time) tea.Msg {
			return clearMessage{}
		})
	case clearMessage:
		progress.blank = true
		return progress, tea.Quit
	}
	return progress, nil
}

func (progress *progressModel) applyEvent(event provider.ProgressEvent) {
	if event.Network != nil {
		progress.network = *event.Network
		return
	}
	state := progress.providers[event.Provider]
	if state.startedAt.IsZero() && event.Phase != provider.ProgressWaiting {
		state.startedAt = progress.now
	}
	state.phase = event.Phase
	if event.Test != "" {
		state.test = event.Test
	}
	if event.Server != "" {
		state.server = event.Server
	}
	if event.LiveMbps != nil {
		switch event.Test {
		case "download":
			state.downloadMbps = cloneFloat(event.LiveMbps)
		case "upload":
			state.uploadMbps = cloneFloat(event.LiveMbps)
		}
	}
	if event.DownloadMbps != nil {
		state.downloadMbps = cloneFloat(event.DownloadMbps)
	}
	if event.UploadMbps != nil {
		state.uploadMbps = cloneFloat(event.UploadMbps)
	}
	if event.Message != "" {
		state.message = event.Message
	} else if event.Phase == provider.ProgressComplete {
		state.message = ""
	}
	if isTerminalPhase(event.Phase) && state.endedAt.IsZero() {
		state.endedAt = progress.now
	}
	progress.providers[event.Provider] = state
}

func (progress *progressModel) View() tea.View {
	if progress.blank {
		return tea.NewView("")
	}
	header := joinBlocks(
		progress.theme.appTitle(progress.version),
		progress.theme.Faint().Render("Network  "+renderNetwork(progress.network))+"\n"+
			progress.theme.Faint().Render("Order    "+renderOrder(progress.order)),
	)
	panels := make([]string, 0, len(progress.order))
	for _, name := range progress.order {
		panels = append(panels, strings.Join(progress.renderProvider(name, progress.providers[name], progress.now), "\n"))
	}
	footer := progress.theme.Faint().Render(formatElapsed(progress.now.Sub(progress.startedAt))) + "\n" +
		progress.theme.help("ctrl-c")
	return tea.NewView(joinBlocks(header, strings.Join(panels, "\n\n"), footer))
}

func renderOrder(providers []model.Provider) string {
	labels := make([]string, 0, len(providers))
	for _, provider := range providers {
		labels = append(labels, target.Label(provider))
	}
	return strings.Join(labels, " → ") + " · sequential"
}

func renderNetwork(network model.NetworkContext) string {
	parts := make([]string, 0, 3)
	if network.ActiveInterface != nil && *network.ActiveInterface != "" {
		parts = append(parts, *network.ActiveInterface)
	}
	if network.InterfaceKind != nil && *network.InterfaceKind != "" {
		parts = append(parts, *network.InterfaceKind)
	}
	if network.SSID != nil && *network.SSID != "" {
		parts = append(parts, *network.SSID)
	}
	if len(parts) == 0 {
		return "detecting"
	}
	return strings.Join(parts, " · ")
}

func (progress *progressModel) renderProvider(name model.Provider, state providerState, now time.Time) []string {
	status := phaseLabel(state.phase, state.test)
	if elapsed, ok := providerElapsed(state, now); ok {
		status += " · " + formatElapsed(elapsed)
	}
	label := progress.theme.Option().Render(target.Label(name))
	switch state.phase {
	case provider.ProgressComplete:
		label = progress.theme.OK().Render(target.Label(name))
		status = progress.theme.OK().Render(status)
	case provider.ProgressFailed:
		label = progress.theme.Bad().Render(target.Label(name))
		status = progress.theme.Bad().Render(status)
	case provider.ProgressCancelled:
		label = progress.theme.Faint().Render(target.Label(name))
		status = progress.theme.Faint().Render(status)
	case provider.ProgressWaiting:
		status = progress.theme.Faint().Render(status)
	default:
		label = progress.theme.Accent().Render(target.Label(name))
		status = progress.theme.Accent().Render(status)
	}
	rates := progress.theme.Option().Render(fmt.Sprintf("↓ %s   ↑ %s", formatMbps(state.downloadMbps), formatMbps(state.uploadMbps)))
	return []string{
		label + "  " + status,
		"  " + progress.renderActivity(state.phase, now),
		"  " + rates,
		"  " + progress.theme.Faint().Render(providerDetail(state)),
	}
}

func phaseLabel(phase provider.ProgressPhase, test string) string {
	switch phase {
	case provider.ProgressWaiting:
		return "waiting"
	case provider.ProgressStarting:
		return "starting"
	case provider.ProgressConnecting:
		return "connecting"
	case provider.ProgressMeasuring:
		return "measuring"
	case provider.ProgressDownloading:
		return "downloading"
	case provider.ProgressUploading:
		return "uploading"
	case provider.ProgressComplete:
		return "complete"
	case provider.ProgressFailed:
		if test != "" {
			return "failed · " + test
		}
		return "failed"
	case provider.ProgressCancelled:
		return "cancelled"
	default:
		return string(phase)
	}
}

func (progress *progressModel) renderActivity(phase provider.ProgressPhase, now time.Time) string {
	cells := make([]rune, activityWidth)
	for index := range cells {
		cells[index] = '─'
	}
	cells[0] = '├'
	cells[activityWidth-1] = '┤'
	switch phase {
	case provider.ProgressComplete:
		cells[activityWidth-2] = '●'
		return progress.theme.OK().Render(string(cells))
	case provider.ProgressWaiting:
		return progress.theme.Faint().Render(string(cells))
	case provider.ProgressFailed, provider.ProgressCancelled:
		return progress.theme.Bad().Render(string(cells))
	default:
		span := activityWidth - 2
		if span < 1 {
			span = 1
		}
		position := int(now.UnixNano()/int64(refreshInterval))%span + 1
		cells[position] = '●'
		return progress.theme.Accent().Render(string(cells))
	}
}

func providerDetail(state providerState) string {
	if state.message != "" && (state.phase == provider.ProgressFailed || state.phase == provider.ProgressCancelled) {
		return truncateRunes("error: "+state.message, detailRuneLimit)
	}
	if state.server != "" {
		return truncateRunes("server "+state.server, detailRuneLimit)
	}
	switch state.phase {
	case provider.ProgressWaiting:
		return "not started"
	case provider.ProgressStarting:
		return "preparing provider"
	case provider.ProgressConnecting:
		return "selecting and connecting to server"
	case provider.ProgressMeasuring:
		return "download and upload measurement active"
	case provider.ProgressDownloading:
		return "download measurement active"
	case provider.ProgressUploading:
		return "upload measurement active"
	case provider.ProgressComplete:
		return "measurement complete"
	case provider.ProgressFailed:
		return "measurement failed"
	case provider.ProgressCancelled:
		return "measurement cancelled"
	default:
		return "—"
	}
}

func providerElapsed(state providerState, now time.Time) (time.Duration, bool) {
	if state.startedAt.IsZero() {
		return 0, false
	}
	end := now
	if !state.endedAt.IsZero() {
		end = state.endedAt
	}
	return end.Sub(state.startedAt), true
}

func isTerminalPhase(phase provider.ProgressPhase) bool {
	return phase == provider.ProgressComplete || phase == provider.ProgressFailed || phase == provider.ProgressCancelled
}

func truncateRunes(value string, limit int) string {
	if limit <= 0 || utf8.RuneCountInString(value) <= limit {
		return value
	}
	runes := []rune(value)
	if limit == 1 {
		return "…"
	}
	return string(runes[:limit-1]) + "…"
}

func formatMbps(value *float64) string {
	if value == nil {
		return "—"
	}
	return fmt.Sprintf("%.2f Mbps", *value)
}

func formatElapsed(duration time.Duration) string {
	if duration < 0 {
		duration = 0
	}
	seconds := int(duration.Round(time.Second).Seconds())
	return fmt.Sprintf("%02d:%02d", seconds/60, seconds%60)
}

func cloneFloat(value *float64) *float64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
