package campus

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"net/url"
	"strings"

	"github.com/soundadam/pace/internal/model"
)

var errNoResult = errors.New("LibreSpeed returned no measurement result")

type libreSpeedReport struct {
	Timestamp string           `json:"timestamp"`
	Server    libreSpeedServer `json:"server"`
	Client    libreSpeedClient `json:"client"`
	BytesSent uint64           `json:"bytes_sent"`
	BytesRecv uint64           `json:"bytes_received"`
	Ping      float64          `json:"ping"`
	Jitter    float64          `json:"jitter"`
	Upload    float64          `json:"upload"`
	Download  float64          `json:"download"`
	Share     string           `json:"share"`
}

type libreSpeedServer struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

type libreSpeedClient struct {
	IP       string `json:"ip"`
	Hostname string `json:"hostname"`
	City     string `json:"city"`
	Region   string `json:"region"`
	Country  string `json:"country"`
	Location string `json:"loc"`
	Org      string `json:"org"`
	Postal   string `json:"postal"`
	Timezone string `json:"timezone"`
}

func parseResult(data []byte, measurementProvider model.Provider, family, helperVersion string, durationMS int64) (model.Measurement, error) {
	var reports []libreSpeedReport
	if err := json.Unmarshal(data, &reports); err != nil {
		return model.Measurement{}, fmt.Errorf("decode LibreSpeed JSON: %w", err)
	}
	if len(reports) == 0 {
		return model.Measurement{}, errNoResult
	}
	if len(reports) != 1 {
		return model.Measurement{}, fmt.Errorf("expected one LibreSpeed result, got %d", len(reports))
	}
	report := reports[0]
	if report.Server.Name == "" {
		return model.Measurement{}, errors.New("LibreSpeed result is missing server name")
	}
	if report.Server.URL == "" {
		return model.Measurement{}, errors.New("LibreSpeed result is missing server URL")
	}
	serverURL, err := url.Parse(report.Server.URL)
	if err != nil || serverURL.Hostname() == "" {
		return model.Measurement{}, fmt.Errorf("LibreSpeed result has invalid server URL %q", report.Server.URL)
	}
	if family != "ipv4" && family != "ipv6" {
		return model.Measurement{}, fmt.Errorf("unsupported result IP family %q", family)
	}
	var clientPublicIP *string
	clientIPText := strings.TrimSpace(report.Client.IP)
	if clientIPText != "" {
		clientIP := net.ParseIP(clientIPText)
		if clientIP == nil {
			return model.Measurement{}, fmt.Errorf("LibreSpeed result has invalid client IP %q", report.Client.IP)
		}
		switch family {
		case "ipv4":
			if clientIP.To4() == nil {
				return model.Measurement{}, errors.New("LibreSpeed IPv4 result contains a non-IPv4 client address")
			}
		case "ipv6":
			if clientIP.To4() != nil {
				return model.Measurement{}, errors.New("LibreSpeed IPv6 result contains an IPv4 client address")
			}
		}
		clientPublicIP = model.Pointer(clientIPText)
	}
	if err := validateMetric("ping", report.Ping); err != nil {
		return model.Measurement{}, err
	}
	if err := validateMetric("jitter", report.Jitter); err != nil {
		return model.Measurement{}, err
	}
	if err := validateMetric("download", report.Download); err != nil {
		return model.Measurement{}, err
	}
	if err := validateMetric("upload", report.Upload); err != nil {
		return model.Measurement{}, err
	}
	if report.BytesRecv > math.MaxInt64 || report.BytesSent > math.MaxInt64 {
		return model.Measurement{}, errors.New("LibreSpeed byte count exceeds supported range")
	}

	downloadBytes := int64(report.BytesRecv)
	uploadBytes := int64(report.BytesSent)
	concurrency := ConcurrentRequests
	fqdn := serverURL.Hostname()
	return model.Measurement{
		Provider:       measurementProvider,
		Method:         model.MethodLibreSpeedThreeStream,
		Status:         model.ProviderStatusSuccess,
		IPFamily:       model.Pointer(family),
		ServerName:     model.Pointer(report.Server.Name),
		ServerFQDN:     model.Pointer(fqdn),
		ServerAddress:  model.Pointer(report.Server.URL),
		ClientPublicIP: clientPublicIP,
		PingMS:         model.Pointer(report.Ping),
		JitterMS:       model.Pointer(report.Jitter),
		DownloadMbps:   model.Pointer(report.Download),
		UploadMbps:     model.Pointer(report.Upload),
		DownloadBytes:  model.Pointer(downloadBytes),
		UploadBytes:    model.Pointer(uploadBytes),
		DurationMS:     model.Pointer(durationMS),
		Concurrency:    model.Pointer(concurrency),
		HelperVersion:  model.Pointer(helperVersion),
	}, nil
}

func validateMetric(name string, value float64) error {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return fmt.Errorf("LibreSpeed %s value is invalid", name)
	}
	return nil
}
