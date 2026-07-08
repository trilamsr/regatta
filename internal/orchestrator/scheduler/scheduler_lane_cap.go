package scheduler

// snapshotLaneCaps clones s.cfg.LaneCaps so a single Tick sees one
// consistent cap map even if the config is mutated mid-tick (R31-I5,
// #1362).
func (s *Scheduler) snapshotLaneCaps() map[string]int {
	out := make(map[string]int, len(s.cfg.LaneCaps))
	for lane, limit := range s.cfg.LaneCaps {
		out[lane] = limit
	}
	return out
}

func (s *Scheduler) laneHasCapacity(lane string, caps, occupancy map[string]int) bool {
	limit, gated := caps[lane]
	if !gated {
		return true
	}
	return occupancy[lane] < limit
}
