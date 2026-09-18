package provider

import (
	"testing"
	"time"
)

func TestElapsedMS(t *testing.T) {
	start := time.Date(2024, time.March, 1, 12, 0, 0, 0, time.UTC)
	for _, testCase := range []struct {
		name string
		end  time.Time
		want int64
	}{
		{name: "same instant", end: start, want: 0},
		{name: "sub-millisecond truncates", end: start.Add(999 * time.Microsecond), want: 0},
		{name: "whole milliseconds", end: start.Add(1500 * time.Millisecond), want: 1500},
		{name: "clock moved backwards", end: start.Add(-5 * time.Second), want: 0},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if got := ElapsedMS(start, testCase.end); got != testCase.want {
				t.Fatalf("ElapsedMS = %d, want %d", got, testCase.want)
			}
		})
	}
}
