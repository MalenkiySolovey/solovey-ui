//go:build !linux

package mountevidence

func Observe(string) (Fact, error)          { return Fact{}, ErrUnavailable }
func AvailableBytes(string) (uint64, error) { return 0, ErrUnavailable }
func ObserveOverlayLabel(string) (OverlayLabelFact, error) {
	return OverlayLabelFact{}, ErrUnavailable
}
func ProbeOverlayBacking(string, Fact) (BackingProbeFact, error) {
	return BackingProbeFact{}, ErrUnavailable
}
