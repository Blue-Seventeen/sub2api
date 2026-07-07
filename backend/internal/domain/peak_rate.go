package domain

// PeakRateWindow describes one same-day peak billing window.
type PeakRateWindow struct {
	Start      string  `json:"start"`
	End        string  `json:"end"`
	Multiplier float64 `json:"multiplier"`
}
