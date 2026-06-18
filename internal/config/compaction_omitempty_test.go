package config

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestCompactionConfig_ZeroValueRoundTrips verifies RC3 (SRS 010): the zero
// values of AutoCompactThreshold (the documented "0 = disabled" sentinel) and
// KeepLastMessages must survive json.Marshal/Unmarshal. Both previously carried
// `omitempty`, so 0 was dropped on every marshal — config.Save wrote nothing
// and Config.Hash()/config.get returned the field absent, so the Sessions
// Settings menu could not persist "disable auto-compaction" (it silently
// reverted to the 0.75 default). The tags are now non-omitempty; this test
// pins that.
func TestCompactionConfig_ZeroValueRoundTrips(t *testing.T) {
	src := &CompactionConfig{
		AutoCompactThreshold: 0,
		KeepLastMessages:     0,
		ReserveTokensFloor:   20000,
		MaxHistoryShare:      0.85,
	}
	data, err := json.Marshal(src)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(data)
	// omitempty removed -> the zero values must be present in the JSON.
	for _, want := range []string{`"autoCompactThreshold":0`, `"keepLastMessages":0`} {
		if !strings.Contains(s, want) {
			t.Errorf("expected %s in marshal output (omitempty regression?), got %s", want, s)
		}
	}
	// Nil pointer field must still be omitted (its zero value is unset, not a
	// meaningful sentinel) — only the two numeric fields changed.
	if strings.Contains(s, `"memoryFlush"`) {
		t.Errorf("nil *MemoryFlushConfig must stay omitted, got %s", s)
	}

	var dst CompactionConfig
	if err := json.Unmarshal(data, &dst); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if dst.AutoCompactThreshold != 0 || dst.KeepLastMessages != 0 {
		t.Errorf("zero values did not round-trip: threshold=%v keepLast=%v", dst.AutoCompactThreshold, dst.KeepLastMessages)
	}
	if dst.ReserveTokensFloor != 20000 || dst.MaxHistoryShare != 0.85 {
		t.Errorf("non-zero siblings lost in round-trip: floor=%v share=%v", dst.ReserveTokensFloor, dst.MaxHistoryShare)
	}
}

// TestCompactionConfig_NonZeroStillSerializes guards the inverse regression.
func TestCompactionConfig_NonZeroStillSerializes(t *testing.T) {
	data, err := json.Marshal(&CompactionConfig{AutoCompactThreshold: 0.75, KeepLastMessages: 4})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, `"autoCompactThreshold":0.75`) || !strings.Contains(s, `"keepLastMessages":4`) {
		t.Errorf("non-zero values missing from JSON: %s", s)
	}
}
