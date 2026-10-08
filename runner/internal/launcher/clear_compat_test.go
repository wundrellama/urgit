package launcher

// Recovery ruling A (INTEGRATION.md §8): the operator's clear is two
// explicit operations now — a cleanup retry, then a separate release of
// the exact incarnation at the revision inspected. The accepted tests
// written against the combined clear (and review 02's and 03's probes)
// keep their scenarios through this test seam, which performs the two
// production operations in order and nothing else: it releases only what
// the retry left holding nothing, and a refusal of either is its answer.
// Production has no combined operation.
func (s *Service) ClearQuarantine(id string) error {
	s.mu.Lock()
	e, ok := s.vms[id]
	if !ok {
		s.mu.Unlock()
		return ErrUnknown
	}
	sel := e.rec.Selection()
	s.mu.Unlock()
	in, err := s.RetryCleanup(sel)
	if err != nil {
		return err
	}
	return s.Release(in.Selection)
}
