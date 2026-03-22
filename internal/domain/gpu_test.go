package domain

import (
	"testing"
	"time"
)

func TestParseNvidiaSmiOutput(t *testing.T) {
	raw := "0, NVIDIA GeForce RTX 3090, 45, 8192, 24576, 65, 150.50, 350.00\n" +
		"1, NVIDIA GeForce RTX 3090, 78, 12000, 24576, 72, 280.30, 350.00"

	gpus, err := ParseNvidiaSmiOutput(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(gpus) != 2 {
		t.Fatalf("expected 2 GPUs, got %d", len(gpus))
	}

	// Verify first GPU
	g0 := gpus[0]
	if g0.Index != 0 {
		t.Errorf("gpu0 index: got %d, want 0", g0.Index)
	}
	if g0.Name != "NVIDIA GeForce RTX 3090" {
		t.Errorf("gpu0 name: got %q, want %q", g0.Name, "NVIDIA GeForce RTX 3090")
	}
	if g0.UtilizationPercent != 45 {
		t.Errorf("gpu0 utilization: got %f, want 45", g0.UtilizationPercent)
	}
	if g0.MemoryUsedMB != 8192 {
		t.Errorf("gpu0 memUsed: got %d, want 8192", g0.MemoryUsedMB)
	}
	if g0.MemoryTotalMB != 24576 {
		t.Errorf("gpu0 memTotal: got %d, want 24576", g0.MemoryTotalMB)
	}
	if g0.TemperatureC != 65 {
		t.Errorf("gpu0 temp: got %d, want 65", g0.TemperatureC)
	}
	if g0.PowerDrawW != 150.50 {
		t.Errorf("gpu0 powerDraw: got %f, want 150.50", g0.PowerDrawW)
	}
	if g0.PowerLimitW != 350.00 {
		t.Errorf("gpu0 powerLimit: got %f, want 350.00", g0.PowerLimitW)
	}

	// Verify second GPU
	g1 := gpus[1]
	if g1.Index != 1 {
		t.Errorf("gpu1 index: got %d, want 1", g1.Index)
	}
	if g1.UtilizationPercent != 78 {
		t.Errorf("gpu1 utilization: got %f, want 78", g1.UtilizationPercent)
	}
	if g1.MemoryUsedMB != 12000 {
		t.Errorf("gpu1 memUsed: got %d, want 12000", g1.MemoryUsedMB)
	}
	if g1.PowerDrawW != 280.30 {
		t.Errorf("gpu1 powerDraw: got %f, want 280.30", g1.PowerDrawW)
	}

	// Verify MemoryUsagePercent
	pct := g0.MemoryUsagePercent()
	expected := float64(8192) / float64(24576) * 100
	if pct != expected {
		t.Errorf("gpu0 MemoryUsagePercent: got %f, want %f", pct, expected)
	}
}

func TestParseNvidiaSmiOutput_EmptyInput(t *testing.T) {
	gpus, err := ParseNvidiaSmiOutput("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gpus != nil {
		t.Errorf("expected nil slice, got %v", gpus)
	}

	// Also test whitespace-only
	gpus, err = ParseNvidiaSmiOutput("   \n  \n  ")
	if err != nil {
		t.Fatalf("unexpected error for whitespace: %v", err)
	}
	if gpus != nil {
		t.Errorf("expected nil slice for whitespace, got %v", gpus)
	}
}

func TestParseNvidiaSmiOutput_MalformedLine(t *testing.T) {
	_, err := ParseNvidiaSmiOutput("garbage data here")
	if err == nil {
		t.Fatal("expected error for malformed input, got nil")
	}

	// Wrong number of fields
	_, err = ParseNvidiaSmiOutput("0, NVIDIA, 45, 8192")
	if err == nil {
		t.Fatal("expected error for wrong field count, got nil")
	}

	// Non-numeric index
	_, err = ParseNvidiaSmiOutput("abc, NVIDIA, 45, 8192, 24576, 65, 150.50, 350.00")
	if err == nil {
		t.Fatal("expected error for non-numeric index, got nil")
	}
}

func TestGPUNodeMetrics_Summary(t *testing.T) {
	m := &GPUNodeMetrics{
		NodeName: "node-1",
		DeviceID: "dev-1",
		GPUs: []GPUInfo{
			{Index: 0, UtilizationPercent: 40, MemoryUsedMB: 8000, MemoryTotalMB: 24576},
			{Index: 1, UtilizationPercent: 80, MemoryUsedMB: 12000, MemoryTotalMB: 24576},
		},
		CollectedAt: time.Now(),
	}

	if !m.HasGPU() {
		t.Error("HasGPU: expected true")
	}

	avgUtil := m.AvgUtilization()
	if avgUtil != 60 {
		t.Errorf("AvgUtilization: got %f, want 60", avgUtil)
	}

	totalUsed := m.TotalMemoryUsedMB()
	if totalUsed != 20000 {
		t.Errorf("TotalMemoryUsedMB: got %d, want 20000", totalUsed)
	}

	totalMem := m.TotalMemoryMB()
	if totalMem != 49152 {
		t.Errorf("TotalMemoryMB: got %d, want 49152", totalMem)
	}

	// Test empty metrics
	empty := &GPUNodeMetrics{}
	if empty.HasGPU() {
		t.Error("empty HasGPU: expected false")
	}
	if empty.AvgUtilization() != 0 {
		t.Errorf("empty AvgUtilization: got %f, want 0", empty.AvgUtilization())
	}
	if empty.TotalMemoryUsedMB() != 0 {
		t.Errorf("empty TotalMemoryUsedMB: got %d, want 0", empty.TotalMemoryUsedMB())
	}
	if empty.TotalMemoryMB() != 0 {
		t.Errorf("empty TotalMemoryMB: got %d, want 0", empty.TotalMemoryMB())
	}
}
