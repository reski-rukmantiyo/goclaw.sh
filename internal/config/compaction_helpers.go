package config

// EffectiveAutoCompactThreshold returns the primary compaction threshold from config,
// falling back to DefaultAutoCompactThreshold when not set.
func EffectiveAutoCompactThreshold(cfg *CompactionConfig) float64 {
	if cfg != nil && cfg.AutoCompactThreshold > 0 {
		return cfg.AutoCompactThreshold
	}
	return DefaultAutoCompactThreshold
}

// EffectiveHistoryShare returns the compaction threshold for history budget checks.
// It prefers MaxHistoryShare when explicitly set (backward compat), otherwise derives
// from AutoCompactThreshold for unified behavior.
func EffectiveHistoryShare(cfg *CompactionConfig) float64 {
	if cfg != nil && cfg.MaxHistoryShare > 0 {
		return cfg.MaxHistoryShare
	}
	return EffectiveAutoCompactThreshold(cfg)
}
