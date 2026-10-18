package host

import "testing"

func TestCollect(t *testing.T) {
	info := Collect()
	if info.OS == "" {
		t.Error("OS is empty")
	}
	if info.Arch == "" {
		t.Error("Arch is empty")
	}
	if info.NumCPU < 1 {
		t.Errorf("NumCPU = %d, want >= 1", info.NumCPU)
	}
	if info.GoVersion == "" {
		t.Error("GoVersion is empty")
	}
}
