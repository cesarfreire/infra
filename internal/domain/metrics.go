package domain

import "time"

// Unit tells how a metric value is measured and displayed.
type Unit string

const (
	UnitPercent        Unit = "percent"
	UnitBytes          Unit = "bytes"
	UnitBytesPerSecond Unit = "bytes/s"
	UnitCount          Unit = "count"
	UnitPerSecond      Unit = "per-second"
	UnitSeconds        Unit = "seconds"
)

// Point is a metric value at a time.
type Point struct {
	Time  time.Time
	Value float64
}

// Series is a metric over time, oldest point first.
type Series struct {
	Label  string
	Unit   Unit
	Points []Point
}

// MetricWindow is how far back to look and the resolution of each point.
type MetricWindow struct {
	Label  string
	Range  time.Duration
	Period time.Duration
}
