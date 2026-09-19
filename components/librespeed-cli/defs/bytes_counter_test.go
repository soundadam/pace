package defs

import (
	"bytes"
	"crypto/rand"
	"io"
	"math"
	"sync"
	"testing"
	"time"
)

func TestBytesCounterTotalAndAverages(t *testing.T) {
	counter := NewCounter()
	counter.Start()
	counter.start = time.Now().Add(-4 * time.Second)

	for i := 0; i < 4; i++ {
		n, err := counter.Write(make([]byte, 125000))
		if err != nil {
			t.Fatalf("Write: %v", err)
		}
		if n != 125000 {
			t.Fatalf("Write returned %d, want 125000", n)
		}
	}

	if got := counter.Total(); got != 500000 {
		t.Errorf("Total() = %d, want 500000", got)
	}
	if got := counter.AvgBytes(); math.Abs(got-125000) > 125000*0.05 {
		t.Errorf("AvgBytes() = %v, want ~125000", got)
	}
	if got := counter.AvgMbps(); math.Abs(got-1) > 0.05 {
		t.Errorf("AvgMbps() = %v, want ~1", got)
	}

	counter.SetMebi(true)
	if got := counter.AvgMbps(); math.Abs(got-125000.0/131072.0) > 0.05 {
		t.Errorf("AvgMbps() with mebi = %v, want ~%v", got, 125000.0/131072.0)
	}
}

// TestBytesCounterConcurrentAccess is the regression test for the data race
// between the request goroutines writing to the counter and Download/Upload
// (and the spinner callback) reading the running average from another
// goroutine. It only fails under -race.
func TestBytesCounterConcurrentAccess(t *testing.T) {
	counter := NewCounter()
	counter.Start()

	stop := make(chan struct{})
	var writers sync.WaitGroup
	for i := 0; i < 4; i++ {
		writers.Add(1)
		go func() {
			defer writers.Done()
			block := make([]byte, 4096)
			for {
				select {
				case <-stop:
					return
				default:
					_, _ = counter.Write(block)
				}
			}
		}()
	}

	var readers sync.WaitGroup
	for i := 0; i < 2; i++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for j := 0; j < 2000; j++ {
				_ = counter.AvgBytes()
				_ = counter.AvgMbps()
				_ = counter.AvgHumanize()
				_ = counter.CurrentSpeed()
				_ = counter.Total()
			}
		}()
	}

	readers.Wait()
	close(stop)
	writers.Wait()

	if counter.Total() == 0 {
		t.Error("Total() = 0 after concurrent writes")
	}
}

func TestBytesCounterAvgHumanize(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mebi    bool
		bytes   uint64
		elapsed time.Duration
		want    string
	}{
		{"bytes per second", false, 500, time.Second, "500.00 bytes/s"},
		{"kilobytes per second", false, 5000, time.Second, "5.00 KB/s"},
		{"megabytes per second", false, 5000000, time.Second, "5.00 MB/s"},
		{"gigabytes per second", false, 5000000000, time.Second, "5.00 GB/s"},
		{"kibibytes per second", true, 5120, time.Second, "5.00 KB/s"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			counter := NewCounter()
			counter.SetMebi(tc.mebi)
			// Set the byte total directly rather than writing gigabytes
			// through the counter, and start the clock last so the wall
			// clock barely advances before the unit is chosen.
			counter.total = tc.bytes
			counter.start = time.Now().Add(-tc.elapsed)

			if got := counter.AvgHumanize(); got != tc.want {
				t.Errorf("AvgHumanize() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBytesCounterPayloadAndReaderRecycling(t *testing.T) {
	counter := NewCounter()
	counter.SetUploadSize(4) // KiB
	counter.GenerateBlob()
	counter.Start()

	if got := len(counter.Payload()); got != 4*1024 {
		t.Fatalf("len(Payload()) = %d, want %d", got, 4*1024)
	}
	if bytes.Equal(counter.Payload(), make([]byte, 4*1024)) {
		t.Error("Payload() is all zeroes, want random data")
	}

	// Reading past the end of the blob rewinds the reader so the upload body
	// can be replayed indefinitely.
	buf := make([]byte, 4*1024)
	for i := 0; i < 3; i++ {
		n, err := io.ReadFull(counter, buf)
		if err != nil {
			t.Fatalf("read %d: %v", i, err)
		}
		if n != len(buf) {
			t.Fatalf("read %d returned %d bytes, want %d", i, n, len(buf))
		}
	}
	if got := counter.Total(); got != 3*4*1024 {
		t.Errorf("Total() = %d, want %d", got, 3*4*1024)
	}
}

func TestSeekWrapperIsANoopSeeker(t *testing.T) {
	wrapper := &SeekWrapper{rand.Reader}

	offset, err := wrapper.Seek(42, io.SeekStart)
	if err != nil {
		t.Fatalf("Seek: %v", err)
	}
	if offset != 42 {
		t.Errorf("Seek returned %d, want the offset it was given (42)", offset)
	}

	buf := make([]byte, 64)
	if _, err := io.ReadFull(wrapper, buf); err != nil {
		t.Fatalf("Read: %v", err)
	}
}

func TestGetRandomData(t *testing.T) {
	for _, length := range []int{0, 1, 7, 8, 9, 4096} {
		data := getRandomData(length)
		if len(data) != length {
			t.Errorf("getRandomData(%d) returned %d bytes", length, len(data))
		}
	}
	if a, b := getRandomData(1024), getRandomData(1024); bytes.Equal(a, b) {
		t.Error("two calls to getRandomData returned identical data")
	}
}

func TestGetAvg(t *testing.T) {
	if got := getAvg([]float64{1, 2, 3, 4}); got != 2.5 {
		t.Errorf("getAvg = %v, want 2.5", got)
	}
	if got := getAvg(nil); !math.IsNaN(got) {
		t.Errorf("getAvg(nil) = %v, want NaN (0/0)", got)
	}
}
