package model

import (
	"errors"
	"fmt"
	"time"
)

// SchemaVersion identifies the run-summary contract.  Version 2 removed the
// ambiguous `campus` provider ID and made the ordered `targets` array
// mandatory, so a version-1 summary is not readable by this build.
const SchemaVersion = 2

type Command string

const (
	CommandRun      Command = "run"
	CommandCampus   Command = "campus"
	CommandEdge     Command = "edge"
	CommandDomestic Command = "domestic"
	CommandMLab     Command = "mlab"
	CommandApple    Command = "apple"
	CommandOokla    Command = "ookla"
)

type Provider string

const (
	ProviderMLab Provider = "mlab"

	ProviderNJUCampusIPv4 Provider = "nju-campus-ipv4"
	ProviderNJUCampusIPv6 Provider = "nju-campus-ipv6"
	ProviderNJUEdgeIPv4   Provider = "nju-edge-ipv4"
	ProviderNJUEdgeIPv6   Provider = "nju-edge-ipv6"
	ProviderCERNETIPv4    Provider = "cernet-ipv4"
	ProviderQLUIPv4       Provider = "qlu-ipv4"
	ProviderTongjiIPv4    Provider = "tongji-ipv4"
	ProviderApple         Provider = "apple-networkquality"
	ProviderOokla         Provider = "ookla-speedtest"
)

const (
	MethodLibreSpeedThreeStream = "librespeed-three-stream"
	MethodNDT7SingleStream      = "ndt7-single-stream"
	MethodAppleNetworkQuality   = "apple-networkquality"
	MethodOoklaSpeedtest        = "ookla-speedtest"
)

type RunStatus string

const (
	RunStatusSuccess   RunStatus = "success"
	RunStatusPartial   RunStatus = "partial"
	RunStatusFailed    RunStatus = "failed"
	RunStatusCancelled RunStatus = "cancelled"
)

type ProviderStatus string

const (
	ProviderStatusSuccess   ProviderStatus = "success"
	ProviderStatusFailed    ProviderStatus = "failed"
	ProviderStatusSkipped   ProviderStatus = "skipped"
	ProviderStatusCancelled ProviderStatus = "cancelled"
)

type FailureStage string

const (
	FailureStageHelper    FailureStage = "helper"
	FailureStageDNS       FailureStage = "dns"
	FailureStageConnect   FailureStage = "connect"
	FailureStageDownload  FailureStage = "download"
	FailureStageUpload    FailureStage = "upload"
	FailureStageTimeout   FailureStage = "timeout"
	FailureStageCancelled FailureStage = "cancelled"
)

type Failure struct {
	Stage   FailureStage `json:"stage"`
	Code    string       `json:"code"`
	Message string       `json:"message"`
}

type NetworkContext struct {
	OS              string   `json:"os,omitempty"`
	Architecture    string   `json:"architecture,omitempty"`
	ActiveInterface *string  `json:"activeInterface"`
	InterfaceKind   *string  `json:"interfaceKind"`
	SSID            *string  `json:"ssid"`
	BSSID           *string  `json:"bssid"`
	LocalIPv4       []string `json:"localIPv4,omitempty"`
	LocalIPv6       []string `json:"localIPv6,omitempty"`
	DefaultGateway  *string  `json:"defaultGateway"`
	DNSServers      []string `json:"dnsServers,omitempty"`
}

type Measurement struct {
	Provider                  Provider       `json:"provider"`
	Method                    string         `json:"method"`
	Status                    ProviderStatus `json:"status"`
	IPFamily                  *string        `json:"ipFamily"`
	ServerName                *string        `json:"serverName"`
	ServerFQDN                *string        `json:"serverFqdn"`
	ServerAddress             *string        `json:"serverAddress"`
	ServerID                  *int64         `json:"serverId,omitempty"`
	ServerSponsor             *string        `json:"serverSponsor,omitempty"`
	ClientPublicIP            *string        `json:"clientPublicIp"`
	PingMS                    *float64       `json:"pingMs"`
	JitterMS                  *float64       `json:"jitterMs"`
	ResponsivenessRPM         *float64       `json:"responsivenessRpm,omitempty"`
	UploadResponsivenessRPM   *float64       `json:"uploadResponsivenessRpm,omitempty"`
	DownloadResponsivenessRPM *float64       `json:"downloadResponsivenessRpm,omitempty"`
	DownloadMbps              *float64       `json:"downloadMbps"`
	UploadMbps                *float64       `json:"uploadMbps"`
	DownloadBytes             *int64         `json:"downloadBytes"`
	UploadBytes               *int64         `json:"uploadBytes"`
	DurationMS                *int64         `json:"durationMs"`
	Concurrency               *int           `json:"concurrency"`
	HelperVersion             *string        `json:"helperVersion"`
	Failure                   *Failure       `json:"failure,omitempty"`
}

type RunSummary struct {
	SchemaVersion int            `json:"schemaVersion"`
	RunID         string         `json:"runId"`
	ToolVersion   string         `json:"toolVersion"`
	StartedAt     time.Time      `json:"startedAt"`
	EndedAt       time.Time      `json:"endedAt"`
	Command       Command        `json:"command"`
	Targets       []Provider     `json:"targets"`
	Status        RunStatus      `json:"status"`
	Label         *string        `json:"label"`
	Note          *string        `json:"note"`
	Network       NetworkContext `json:"network"`
	Measurements  []Measurement  `json:"measurements"`
}

func (summary RunSummary) Validate() error {
	if summary.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported schema version %d", summary.SchemaVersion)
	}
	if summary.RunID == "" {
		return errors.New("run ID is required")
	}
	if summary.ToolVersion == "" {
		return errors.New("tool version is required")
	}
	if summary.StartedAt.IsZero() || summary.EndedAt.IsZero() {
		return errors.New("start and end timestamps are required")
	}
	if summary.EndedAt.Before(summary.StartedAt) {
		return errors.New("end timestamp precedes start timestamp")
	}
	if !summary.Command.valid() {
		return fmt.Errorf("invalid command %q", summary.Command)
	}
	if !summary.Status.valid() {
		return fmt.Errorf("invalid run status %q", summary.Status)
	}

	// The ordered target list is the run's plan of record: it is resolved once,
	// with the requested address family already applied, and every measurement
	// must line up with it one-for-one.
	if len(summary.Targets) == 0 {
		return errors.New("at least one target is required")
	}
	seenTargets := make(map[Provider]struct{}, len(summary.Targets))
	for index, measurementTarget := range summary.Targets {
		if !ProviderValid(measurementTarget) {
			return fmt.Errorf("target %d: invalid provider %q", index, measurementTarget)
		}
		if _, exists := seenTargets[measurementTarget]; exists {
			return fmt.Errorf("target %d: duplicate provider %q", index, measurementTarget)
		}
		seenTargets[measurementTarget] = struct{}{}
	}
	if len(summary.Measurements) != len(summary.Targets) {
		return fmt.Errorf("command %q requires %d measurements, got %d", summary.Command, len(summary.Targets), len(summary.Measurements))
	}
	for index, measurement := range summary.Measurements {
		if measurement.Provider != summary.Targets[index] {
			return fmt.Errorf("measurement %d: provider %q does not match ordered target %q", index, measurement.Provider, summary.Targets[index])
		}
		if measurement.Method != ProviderMethod(measurement.Provider) {
			return fmt.Errorf("measurement %d: method %q does not match provider %q", index, measurement.Method, measurement.Provider)
		}
		if !measurement.Status.valid() {
			return fmt.Errorf("measurement %d: invalid status %q", index, measurement.Status)
		}
		if err := measurement.validateValues(); err != nil {
			return fmt.Errorf("measurement %d: %w", index, err)
		}
	}
	if derived := DeriveRunStatus(summary.Measurements); summary.Status != derived {
		return fmt.Errorf("run status %q does not match measurement status %q", summary.Status, derived)
	}
	return nil
}

func (command Command) valid() bool {
	switch command {
	case CommandRun, CommandCampus, CommandEdge, CommandDomestic, CommandMLab, CommandApple, CommandOokla:
		return true
	default:
		return false
	}
}

func ProviderValid(provider Provider) bool {
	switch provider {
	case ProviderMLab,
		ProviderNJUCampusIPv4,
		ProviderNJUCampusIPv6,
		ProviderNJUEdgeIPv4,
		ProviderNJUEdgeIPv6,
		ProviderCERNETIPv4,
		ProviderQLUIPv4,
		ProviderTongjiIPv4,
		ProviderApple,
		ProviderOokla:
		return true
	default:
		return false
	}
}

func ProviderMethod(provider Provider) string {
	switch provider {
	case ProviderNJUCampusIPv4,
		ProviderNJUCampusIPv6,
		ProviderNJUEdgeIPv4,
		ProviderNJUEdgeIPv6,
		ProviderCERNETIPv4,
		ProviderQLUIPv4,
		ProviderTongjiIPv4:
		return MethodLibreSpeedThreeStream
	case ProviderMLab:
		return MethodNDT7SingleStream
	case ProviderApple:
		return MethodAppleNetworkQuality
	case ProviderOokla:
		return MethodOoklaSpeedtest
	default:
		return ""
	}
}

func (status RunStatus) valid() bool {
	switch status {
	case RunStatusSuccess, RunStatusPartial, RunStatusFailed, RunStatusCancelled:
		return true
	default:
		return false
	}
}

func (status ProviderStatus) valid() bool {
	switch status {
	case ProviderStatusSuccess, ProviderStatusFailed, ProviderStatusSkipped, ProviderStatusCancelled:
		return true
	default:
		return false
	}
}

func (measurement Measurement) validateValues() error {
	if err := validateNonnegative("download Mbps", measurement.DownloadMbps); err != nil {
		return err
	}
	if err := validateNonnegative("upload Mbps", measurement.UploadMbps); err != nil {
		return err
	}
	if err := validateNonnegative("ping milliseconds", measurement.PingMS); err != nil {
		return err
	}
	if err := validateNonnegative("jitter milliseconds", measurement.JitterMS); err != nil {
		return err
	}
	if err := validateNonnegative("responsiveness RPM", measurement.ResponsivenessRPM); err != nil {
		return err
	}
	if err := validateNonnegative("upload responsiveness RPM", measurement.UploadResponsivenessRPM); err != nil {
		return err
	}
	if err := validateNonnegative("download responsiveness RPM", measurement.DownloadResponsivenessRPM); err != nil {
		return err
	}
	if err := validateNonnegativeInt64("download bytes", measurement.DownloadBytes); err != nil {
		return err
	}
	if err := validateNonnegativeInt64("upload bytes", measurement.UploadBytes); err != nil {
		return err
	}
	if err := validateNonnegativeInt64("duration milliseconds", measurement.DurationMS); err != nil {
		return err
	}
	if measurement.Concurrency != nil && *measurement.Concurrency <= 0 {
		return errors.New("concurrency must be positive")
	}
	if measurement.ServerID != nil && *measurement.ServerID < 0 {
		return errors.New("server ID must be non-negative")
	}
	if measurement.IPFamily != nil && *measurement.IPFamily != "ipv4" && *measurement.IPFamily != "ipv6" {
		return fmt.Errorf("invalid IP family %q", *measurement.IPFamily)
	}
	if measurement.HelperVersion != nil && *measurement.HelperVersion == "" {
		return errors.New("helper version must not be empty")
	}

	switch measurement.Status {
	case ProviderStatusSuccess:
		if measurement.DownloadMbps == nil || measurement.UploadMbps == nil {
			return errors.New("successful measurement speeds must be present")
		}
		if measurement.Failure != nil {
			return errors.New("successful measurement must not contain a failure")
		}
	case ProviderStatusFailed:
		if measurement.DownloadMbps == nil || measurement.UploadMbps == nil {
			return errors.New("failed attempted measurement speeds must be zero or measured, not null")
		}
		if measurement.Failure == nil {
			return errors.New("failed measurement requires a failure object")
		}
	case ProviderStatusSkipped:
		if measurement.DownloadMbps != nil || measurement.UploadMbps != nil {
			return errors.New("skipped measurement speeds must be null")
		}
		if measurement.Failure != nil {
			return errors.New("skipped measurement must not contain a failure")
		}
	case ProviderStatusCancelled:
		if measurement.Failure == nil || measurement.Failure.Stage != FailureStageCancelled {
			return errors.New("cancelled measurement requires a cancelled failure")
		}
	}

	if measurement.Failure != nil {
		if !measurement.Failure.Stage.valid() {
			return fmt.Errorf("invalid failure stage %q", measurement.Failure.Stage)
		}
		if measurement.Failure.Code == "" || measurement.Failure.Message == "" {
			return errors.New("failure code and message are required")
		}
	}
	return nil
}

func (stage FailureStage) valid() bool {
	switch stage {
	case FailureStageHelper, FailureStageDNS, FailureStageConnect, FailureStageDownload,
		FailureStageUpload, FailureStageTimeout, FailureStageCancelled:
		return true
	default:
		return false
	}
}

func validateNonnegative(name string, value *float64) error {
	if value != nil && *value < 0 {
		return fmt.Errorf("%s must be non-negative", name)
	}
	return nil
}

func validateNonnegativeInt64(name string, value *int64) error {
	if value != nil && *value < 0 {
		return fmt.Errorf("%s must be non-negative", name)
	}
	return nil
}

func DeriveRunStatus(measurements []Measurement) RunStatus {
	if len(measurements) == 0 {
		return RunStatusFailed
	}

	successes := 0
	for _, measurement := range measurements {
		switch measurement.Status {
		case ProviderStatusCancelled:
			return RunStatusCancelled
		case ProviderStatusSuccess:
			successes++
		}
	}

	if successes == len(measurements) {
		return RunStatusSuccess
	}
	if successes > 0 {
		return RunStatusPartial
	}
	return RunStatusFailed
}

func (summary RunSummary) ExitCode() int {
	switch summary.Status {
	case RunStatusSuccess:
		return 0
	case RunStatusPartial, RunStatusFailed:
		return 2
	case RunStatusCancelled:
		return 130
	default:
		return 1
	}
}

func Pointer[T any](value T) *T {
	return &value
}
