package supervisor

import (
	"context"
	"time"

	"github.com/zeroflow/esphome-sil/internal/apperr"
	"github.com/zeroflow/esphome-sil/internal/control"
)

// History implements control.Control (D-12): the broker's sample ring, cut to the last r.WindowS
// seconds of sim time before its newest sample. The ring lives in the broker, which outlives every
// engine, so the history survives a simulator restart (the restart break is marked on the first
// sample of the new engine).
func (s *supervisor) History(_ context.Context, r control.HistoryRequest) (control.HistoryResult, error) {
	if err := r.Validate(); err != nil {
		return control.HistoryResult{}, err
	}
	if s.broker == nil {
		return control.HistoryResult{}, apperr.Infra("internal", "this instance has no event broker", nil)
	}
	seq, samples := s.broker.History(time.Duration(r.WindowS * float64(time.Second)))
	res := control.HistoryResult{Seq: seq, WindowS: r.WindowS, Samples: samples}
	if n := len(samples); n > 0 {
		res.T = samples[n-1].T
	}
	return res, nil
}
