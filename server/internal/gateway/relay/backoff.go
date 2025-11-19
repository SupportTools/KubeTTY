package relay

import "time"

// FixedBackoff always returns the same delay.
type FixedBackoff struct{ Delay time.Duration }

func (f FixedBackoff) Next(int) time.Duration {
	if f.Delay <= 0 {
		return time.Second
	}
	return f.Delay
}
