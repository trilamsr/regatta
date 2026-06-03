package scheduler

func (s *Scheduler) laneHasCapacity(lane string, occupancy map[string]int) bool {
	limit, gated := s.cfg.LaneCaps[lane]
	if !gated {
		return true
	}
	return occupancy[lane] < limit
}
