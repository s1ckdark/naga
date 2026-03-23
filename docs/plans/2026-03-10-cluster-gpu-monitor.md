# Cluster GPU Monitor TUI — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a `naga cluster monitor <name>` TUI command that shows real-time GPU metrics for all nodes in a cluster, with table and detail view modes.

**Architecture:** SSH into each cluster node to run `nvidia-smi --query-gpu`, parse CSV output into domain types, feed into a bubbletea TUI that polls on an interval. Two view modes: table (default) and detail panels with sparklines.

**Tech Stack:** bubbletea + lipgloss + bubbles (Go TUI), existing SSH executor, nvidia-smi CLI

---

### Task 1: Add bubbletea dependencies

**Files:**
- Modify: `go.mod`

**Step 1: Add dependencies**

Run:
```bash
cd /Users/dave/iWorks/clusterManager && go get github.com/charmbracelet/bubbletea@latest github.com/charmbracelet/lipgloss@latest github.com/charmbracelet/bubbles@latest
```

**Step 2: Verify**

Run: `go mod tidy`
Expected: no errors

**Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "chore: add bubbletea, lipgloss, bubbles dependencies"
```

---

### Task 2: GPU metrics domain type

**Files:**
- Create: `internal/domain/gpu.go`
- Test: `internal/domain/gpu_test.go`

**Step 1: Write the test**

```go
package domain

import (
	"testing"
)

func TestParseNvidiaSmiOutput(t *testing.T) {
	// nvidia-smi --query-gpu=index,name,utilization.gpu,memory.used,memory.total,temperature.gpu,power.draw,power.limit --format=csv,noheader,nounits
	raw := "0, NVIDIA GeForce RTX 4090, 85, 12300, 24564, 72, 250.50, 300.00\n1, NVIDIA GeForce RTX 4090, 92, 20100, 24564, 78, 280.30, 300.00\n"

	gpus, err := ParseNvidiaSmiOutput(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(gpus) != 2 {
		t.Fatalf("expected 2 GPUs, got %d", len(gpus))
	}

	g := gpus[0]
	if g.Index != 0 {
		t.Errorf("expected index 0, got %d", g.Index)
	}
	if g.Name != "NVIDIA GeForce RTX 4090" {
		t.Errorf("expected RTX 4090, got %s", g.Name)
	}
	if g.UtilizationPercent != 85 {
		t.Errorf("expected 85%% util, got %v", g.UtilizationPercent)
	}
	if g.MemoryUsedMB != 12300 {
		t.Errorf("expected 12300 MB used, got %d", g.MemoryUsedMB)
	}
	if g.MemoryTotalMB != 24564 {
		t.Errorf("expected 24564 MB total, got %d", g.MemoryTotalMB)
	}
	if g.TemperatureC != 72 {
		t.Errorf("expected 72C, got %d", g.TemperatureC)
	}
	if g.PowerDrawW != 250.50 {
		t.Errorf("expected 250.50W, got %v", g.PowerDrawW)
	}
	if g.PowerLimitW != 300.00 {
		t.Errorf("expected 300W, got %v", g.PowerLimitW)
	}
}

func TestParseNvidiaSmiOutput_EmptyInput(t *testing.T) {
	gpus, err := ParseNvidiaSmiOutput("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(gpus) != 0 {
		t.Errorf("expected 0 GPUs, got %d", len(gpus))
	}
}

func TestParseNvidiaSmiOutput_MalformedLine(t *testing.T) {
	raw := "garbage data\n"
	_, err := ParseNvidiaSmiOutput(raw)
	if err == nil {
		t.Error("expected error for malformed input")
	}
}

func TestGPUNodeMetrics_Summary(t *testing.T) {
	m := &GPUNodeMetrics{
		NodeName: "worker-1",
		GPUs: []GPUInfo{
			{Index: 0, UtilizationPercent: 80, MemoryUsedMB: 12000, MemoryTotalMB: 24000},
			{Index: 1, UtilizationPercent: 60, MemoryUsedMB: 8000, MemoryTotalMB: 24000},
		},
	}

	if m.AvgUtilization() != 70 {
		t.Errorf("expected avg util 70, got %v", m.AvgUtilization())
	}
	if m.TotalMemoryUsedMB() != 20000 {
		t.Errorf("expected 20000 MB, got %d", m.TotalMemoryUsedMB())
	}
	if m.TotalMemoryMB() != 48000 {
		t.Errorf("expected 48000 MB, got %d", m.TotalMemoryMB())
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/domain/ -run TestParseNvidiaSmi -v`
Expected: FAIL — `ParseNvidiaSmiOutput` not defined

**Step 3: Write implementation**

```go
package domain

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// GPUInfo represents a single GPU's metrics from nvidia-smi
type GPUInfo struct {
	Index              int     `json:"index"`
	Name               string  `json:"name"`
	UtilizationPercent float64 `json:"utilizationPercent"`
	MemoryUsedMB       int     `json:"memoryUsedMB"`
	MemoryTotalMB      int     `json:"memoryTotalMB"`
	TemperatureC       int     `json:"temperatureC"`
	PowerDrawW         float64 `json:"powerDrawW"`
	PowerLimitW        float64 `json:"powerLimitW"`
}

// MemoryUsagePercent returns memory usage as a percentage
func (g *GPUInfo) MemoryUsagePercent() float64 {
	if g.MemoryTotalMB == 0 {
		return 0
	}
	return float64(g.MemoryUsedMB) / float64(g.MemoryTotalMB) * 100
}

// GPUNodeMetrics holds GPU metrics for a single node
type GPUNodeMetrics struct {
	NodeName    string    `json:"nodeName"`
	DeviceID    string    `json:"deviceId"`
	GPUs        []GPUInfo `json:"gpus"`
	CollectedAt time.Time `json:"collectedAt"`
	Error       string    `json:"error,omitempty"`
}

// HasGPU returns true if this node has GPU data
func (m *GPUNodeMetrics) HasGPU() bool {
	return len(m.GPUs) > 0
}

// AvgUtilization returns average GPU utilization across all GPUs
func (m *GPUNodeMetrics) AvgUtilization() float64 {
	if len(m.GPUs) == 0 {
		return 0
	}
	var sum float64
	for _, g := range m.GPUs {
		sum += g.UtilizationPercent
	}
	return sum / float64(len(m.GPUs))
}

// TotalMemoryUsedMB returns total GPU memory used across all GPUs
func (m *GPUNodeMetrics) TotalMemoryUsedMB() int {
	var total int
	for _, g := range m.GPUs {
		total += g.MemoryUsedMB
	}
	return total
}

// TotalMemoryMB returns total GPU memory across all GPUs
func (m *GPUNodeMetrics) TotalMemoryMB() int {
	var total int
	for _, g := range m.GPUs {
		total += g.MemoryTotalMB
	}
	return total
}

// ParseNvidiaSmiOutput parses nvidia-smi CSV output into GPUInfo slices
func ParseNvidiaSmiOutput(raw string) ([]GPUInfo, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}

	var gpus []GPUInfo
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		parts := strings.Split(line, ",")
		if len(parts) != 8 {
			return nil, fmt.Errorf("expected 8 fields, got %d: %q", len(parts), line)
		}

		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}

		index, err := strconv.Atoi(parts[0])
		if err != nil {
			return nil, fmt.Errorf("invalid GPU index %q: %w", parts[0], err)
		}

		util, err := strconv.ParseFloat(parts[2], 64)
		if err != nil {
			return nil, fmt.Errorf("invalid utilization %q: %w", parts[2], err)
		}

		memUsed, err := strconv.Atoi(parts[3])
		if err != nil {
			return nil, fmt.Errorf("invalid memory used %q: %w", parts[3], err)
		}

		memTotal, err := strconv.Atoi(parts[4])
		if err != nil {
			return nil, fmt.Errorf("invalid memory total %q: %w", parts[4], err)
		}

		temp, err := strconv.Atoi(parts[5])
		if err != nil {
			return nil, fmt.Errorf("invalid temperature %q: %w", parts[5], err)
		}

		powerDraw, err := strconv.ParseFloat(parts[6], 64)
		if err != nil {
			return nil, fmt.Errorf("invalid power draw %q: %w", parts[6], err)
		}

		powerLimit, err := strconv.ParseFloat(parts[7], 64)
		if err != nil {
			return nil, fmt.Errorf("invalid power limit %q: %w", parts[7], err)
		}

		gpus = append(gpus, GPUInfo{
			Index:              index,
			Name:               parts[1],
			UtilizationPercent: util,
			MemoryUsedMB:       memUsed,
			MemoryTotalMB:      memTotal,
			TemperatureC:       temp,
			PowerDrawW:         powerDraw,
			PowerLimitW:        powerLimit,
		})
	}

	return gpus, nil
}
```

**Step 4: Run tests**

Run: `go test ./internal/domain/ -run TestParseNvidiaSmi -v && go test ./internal/domain/ -run TestGPUNodeMetrics -v`
Expected: all PASS

**Step 5: Commit**

```bash
git add internal/domain/gpu.go internal/domain/gpu_test.go
git commit -m "feat: add GPU metrics domain type with nvidia-smi parser"
```

---

### Task 3: GPU collector via SSH

**Files:**
- Create: `internal/infra/ssh/gpu_collector.go`
- Test: `internal/infra/ssh/gpu_collector_test.go`

**Step 1: Write the test**

```go
package ssh

import (
	"context"
	"testing"
	"time"

	"github.com/dave/naga/internal/domain"
)

// mockExecutor implements command execution for testing
type mockExecutor struct {
	outputs map[string]string
	errors  map[string]error
}

func (m *mockExecutor) execute(ctx context.Context, device *domain.Device, command string) (string, error) {
	if err, ok := m.errors[device.ID]; ok {
		return "", err
	}
	if out, ok := m.outputs[device.ID]; ok {
		return out, nil
	}
	return "", nil
}

func TestGPUCollector_CollectGPUMetrics(t *testing.T) {
	nvidiaSmiOutput := "0, NVIDIA GeForce RTX 4090, 85, 12300, 24564, 72, 250.50, 300.00\n1, NVIDIA GeForce RTX 4090, 92, 20100, 24564, 78, 280.30, 300.00\n"

	device := &domain.Device{
		ID:          "dev1",
		Name:        "worker-1",
		TailscaleIP: "100.64.0.1",
		Status:      domain.DeviceStatusOnline,
		SSHEnabled:  true,
	}

	// Test parsing logic directly (since SSH executor needs real connection)
	gpus, err := domain.ParseNvidiaSmiOutput(nvidiaSmiOutput)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	metrics := &domain.GPUNodeMetrics{
		NodeName:    device.GetDisplayName(),
		DeviceID:    device.ID,
		GPUs:        gpus,
		CollectedAt: time.Now(),
	}

	if len(metrics.GPUs) != 2 {
		t.Errorf("expected 2 GPUs, got %d", len(metrics.GPUs))
	}
	if metrics.AvgUtilization() != 88.5 {
		t.Errorf("expected avg util 88.5, got %v", metrics.AvgUtilization())
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/infra/ssh/ -run TestGPUCollector -v`
Expected: FAIL — file doesn't exist yet

**Step 3: Write implementation**

`internal/infra/ssh/gpu_collector.go`:
```go
package ssh

import (
	"context"
	"sync"
	"time"

	"github.com/dave/naga/internal/domain"
)

const nvidiaSmiCommand = "nvidia-smi --query-gpu=index,name,utilization.gpu,memory.used,memory.total,temperature.gpu,power.draw,power.limit --format=csv,noheader,nounits"

// GPUCollector collects GPU metrics from devices via SSH
type GPUCollector struct {
	executor      *Executor
	maxConcurrent int
}

// NewGPUCollector creates a new GPU collector
func NewGPUCollector(executor *Executor) *GPUCollector {
	return &GPUCollector{
		executor:      executor,
		maxConcurrent: 10,
	}
}

// CollectGPUMetrics collects GPU metrics from a single device
func (c *GPUCollector) CollectGPUMetrics(ctx context.Context, device *domain.Device) *domain.GPUNodeMetrics {
	metrics := &domain.GPUNodeMetrics{
		NodeName:    device.GetDisplayName(),
		DeviceID:    device.ID,
		CollectedAt: time.Now(),
	}

	output, err := c.executor.Execute(ctx, device, nvidiaSmiCommand)
	if err != nil {
		metrics.Error = err.Error()
		return metrics
	}

	gpus, err := domain.ParseNvidiaSmiOutput(output)
	if err != nil {
		metrics.Error = err.Error()
		return metrics
	}

	metrics.GPUs = gpus
	return metrics
}

// CollectClusterGPUMetrics collects GPU metrics from all nodes in a cluster
func (c *GPUCollector) CollectClusterGPUMetrics(ctx context.Context, devices []*domain.Device) []*domain.GPUNodeMetrics {
	results := make([]*domain.GPUNodeMetrics, len(devices))
	var wg sync.WaitGroup
	sem := make(chan struct{}, c.maxConcurrent)

	for i, device := range devices {
		wg.Add(1)
		go func(idx int, d *domain.Device) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[idx] = c.CollectGPUMetrics(ctx, d)
		}(i, device)
	}

	wg.Wait()
	return results
}
```

**Step 4: Run tests**

Run: `go test ./internal/infra/ssh/ -run TestGPUCollector -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/infra/ssh/gpu_collector.go internal/infra/ssh/gpu_collector_test.go
git commit -m "feat: add GPU metrics collector via SSH nvidia-smi"
```

---

### Task 4: TUI model and update logic

**Files:**
- Create: `internal/tui/monitor/model.go`

**Step 1: Write the model**

```go
package monitor

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dave/naga/internal/domain"
	"github.com/dave/naga/internal/infra/ssh"
)

// ViewMode determines how metrics are displayed
type ViewMode int

const (
	ViewModeTable  ViewMode = iota
	ViewModeDetail
)

// SortField determines how rows are sorted
type SortField int

const (
	SortByNode SortField = iota
	SortByUtil
	SortByMemory
	SortByTemp
	sortFieldCount // sentinel for cycling
)

// Model is the bubbletea model for GPU monitoring
type Model struct {
	clusterName string
	devices     []*domain.Device
	collector   *ssh.GPUCollector
	interval    time.Duration

	// State
	metrics  []*domain.GPUNodeMetrics
	viewMode ViewMode
	sortBy   SortField
	width    int
	height   int
	loading  bool
	lastErr  error

	// History for sparklines (per GPU, keyed by "deviceID:gpuIndex")
	utilHistory map[string][]float64
}

// NewModel creates a new monitor TUI model
func NewModel(clusterName string, devices []*domain.Device, collector *ssh.GPUCollector, interval time.Duration) Model {
	return Model{
		clusterName: clusterName,
		devices:     devices,
		collector:   collector,
		interval:    interval,
		loading:     true,
		utilHistory: make(map[string][]float64),
	}
}

// Messages

type tickMsg time.Time

type metricsMsg struct {
	metrics []*domain.GPUNodeMetrics
	err     error
}

// Commands

func (m Model) tick() tea.Cmd {
	return tea.Tick(m.interval, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m Model) collectMetrics() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), m.interval)
		defer cancel()
		results := m.collector.CollectClusterGPUMetrics(ctx, m.devices)
		return metricsMsg{metrics: results}
	}
}

// Init starts the first collection and ticker
func (m Model) Init() tea.Cmd {
	return tea.Batch(m.collectMetrics(), m.tick())
}

// Update handles messages
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "d":
			if m.viewMode == ViewModeTable {
				m.viewMode = ViewModeDetail
			} else {
				m.viewMode = ViewModeTable
			}
		case "s":
			m.sortBy = (m.sortBy + 1) % sortFieldCount
		case "r":
			m.loading = true
			return m, m.collectMetrics()
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tickMsg:
		return m, tea.Batch(m.collectMetrics(), m.tick())

	case metricsMsg:
		m.loading = false
		m.lastErr = msg.err
		if msg.metrics != nil {
			m.metrics = msg.metrics
			m.updateHistory()
		}
	}

	return m, nil
}

// updateHistory appends current utilization to sparkline history
func (m *Model) updateHistory() {
	const maxHistory = 30

	for _, nm := range m.metrics {
		for _, g := range nm.GPUs {
			key := nm.DeviceID + ":" + string(rune('0'+g.Index))
			h := m.utilHistory[key]
			h = append(h, g.UtilizationPercent)
			if len(h) > maxHistory {
				h = h[len(h)-maxHistory:]
			}
			m.utilHistory[key] = h
		}
	}
}
```

**Step 2: Verify it compiles**

Run: `go build ./internal/tui/monitor/`
Expected: success (no main, just package check)

**Step 3: Commit**

```bash
git add internal/tui/monitor/model.go
git commit -m "feat: add bubbletea model for GPU monitor TUI"
```

---

### Task 5: TUI view rendering

**Files:**
- Create: `internal/tui/monitor/view.go`

**Step 1: Write the view**

```go
package monitor

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/dave/naga/internal/domain"
)

var (
	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("15")).
			Background(lipgloss.Color("62")).
			Padding(0, 1)

	nodeStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("86"))

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196"))

	dimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241"))

	panelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			Padding(0, 1)

	hotGPU  = lipgloss.Color("196") // red
	warmGPU = lipgloss.Color("214") // orange
	coolGPU = lipgloss.Color("46")  // green
)

// View renders the current state
func (m Model) View() string {
	if m.width == 0 {
		return "Initializing..."
	}

	var b strings.Builder

	// Header
	totalGPUs := 0
	totalNodes := 0
	for _, nm := range m.metrics {
		if nm.HasGPU() {
			totalNodes++
			totalGPUs += len(nm.GPUs)
		}
	}

	sortNames := []string{"node", "util", "memory", "temp"}
	header := fmt.Sprintf(" Cluster: %s | %d nodes | %d GPUs | Sort: %s | Refresh: %ds ",
		m.clusterName, totalNodes, totalGPUs, sortNames[m.sortBy], int(m.interval.Seconds()))
	if m.loading {
		header += "| Collecting..."
	}
	b.WriteString(headerStyle.Width(m.width).Render(header))
	b.WriteString("\n\n")

	if m.lastErr != nil {
		b.WriteString(errorStyle.Render(fmt.Sprintf("Error: %v", m.lastErr)))
		b.WriteString("\n\n")
	}

	if len(m.metrics) == 0 {
		b.WriteString("  Waiting for metrics...\n")
		b.WriteString(m.renderHelp())
		return b.String()
	}

	// Render based on view mode
	switch m.viewMode {
	case ViewModeTable:
		b.WriteString(m.renderTable())
	case ViewModeDetail:
		b.WriteString(m.renderDetail())
	}

	b.WriteString("\n")
	b.WriteString(m.renderHelp())

	return b.String()
}

// renderTable renders the compact table view
func (m Model) renderTable() string {
	var b strings.Builder

	// Column header
	header := fmt.Sprintf("  %-16s %-4s %-22s %6s  %-14s %5s  %s",
		"NODE", "GPU", "NAME", "UTIL", "MEMORY", "TEMP", "POWER")
	b.WriteString(dimStyle.Render(header))
	b.WriteString("\n")
	b.WriteString(dimStyle.Render("  " + strings.Repeat("─", m.width-4)))
	b.WriteString("\n")

	rows := m.buildSortedRows()

	for _, r := range rows {
		utilColor := utilToColor(r.gpu.UtilizationPercent)
		utilBar := renderBar(r.gpu.UtilizationPercent, 10)
		memBar := renderBar(r.gpu.MemoryUsagePercent(), 8)
		tempColor := tempToColor(r.gpu.TemperatureC)

		line := fmt.Sprintf("  %s %s %s %s %s  %s %s  %s",
			nodeStyle.Width(16).Render(truncate(r.nodeName, 16)),
			dimStyle.Width(4).Render(fmt.Sprintf("%d", r.gpu.Index)),
			dimStyle.Width(22).Render(truncate(r.gpu.Name, 22)),
			lipgloss.NewStyle().Foreground(utilColor).Render(fmt.Sprintf("%3.0f%%", r.gpu.UtilizationPercent)),
			lipgloss.NewStyle().Foreground(utilColor).Render(utilBar),
			fmt.Sprintf("%5d/%5dMB", r.gpu.MemoryUsedMB, r.gpu.MemoryTotalMB),
			dimStyle.Render(memBar),
			lipgloss.NewStyle().Foreground(tempColor).Render(fmt.Sprintf("%3d°C", r.gpu.TemperatureC)),
		)

		if r.gpu.PowerLimitW > 0 {
			line += fmt.Sprintf("  %3.0f/%3.0fW", r.gpu.PowerDrawW, r.gpu.PowerLimitW)
		}

		b.WriteString(line)
		b.WriteString("\n")
	}

	// Show nodes with errors
	for _, nm := range m.metrics {
		if nm.Error != "" {
			b.WriteString(fmt.Sprintf("  %s %s\n",
				nodeStyle.Render(truncate(nm.NodeName, 16)),
				errorStyle.Render("error: "+truncate(nm.Error, 60)),
			))
		}
	}

	return b.String()
}

// renderDetail renders the panel detail view with sparklines
func (m Model) renderDetail() string {
	var panels []string

	for _, nm := range m.metrics {
		if !nm.HasGPU() {
			continue
		}
		for _, g := range nm.GPUs {
			key := nm.DeviceID + ":" + string(rune('0'+g.Index))
			history := m.utilHistory[key]

			title := fmt.Sprintf("%s GPU%d (%s)", nm.NodeName, g.Index, truncate(g.Name, 18))
			utilColor := utilToColor(g.UtilizationPercent)

			var content strings.Builder
			content.WriteString(lipgloss.NewStyle().Bold(true).Render(title))
			content.WriteString("\n")

			// Sparkline
			spark := renderSparkline(history, 24)
			content.WriteString(fmt.Sprintf(" %s  %s",
				spark,
				lipgloss.NewStyle().Foreground(utilColor).Bold(true).Render(fmt.Sprintf("UTIL: %3.0f%%", g.UtilizationPercent)),
			))
			content.WriteString("\n")

			// Memory bar
			memPct := g.MemoryUsagePercent()
			memBar := renderBar(memPct, 20)
			content.WriteString(fmt.Sprintf(" MEM: %s %5d/%5dMB", memBar, g.MemoryUsedMB, g.MemoryTotalMB))
			content.WriteString("\n")

			// Temp + Power
			tempColor := tempToColor(g.TemperatureC)
			content.WriteString(fmt.Sprintf(" %s",
				lipgloss.NewStyle().Foreground(tempColor).Render(fmt.Sprintf("%d°C", g.TemperatureC)),
			))
			if g.PowerLimitW > 0 {
				content.WriteString(fmt.Sprintf("  %.0f/%.0fW", g.PowerDrawW, g.PowerLimitW))
			}

			panelWidth := 42
			if m.width > 0 {
				cols := m.width / 44
				if cols < 1 {
					cols = 1
				}
				panelWidth = (m.width / cols) - 2
			}

			panels = append(panels, panelStyle.Width(panelWidth).Render(content.String()))
		}
	}

	// Arrange panels in a grid
	if len(panels) == 0 {
		return "  No GPU data available\n"
	}

	cols := m.width / 44
	if cols < 1 {
		cols = 1
	}

	var rows []string
	for i := 0; i < len(panels); i += cols {
		end := i + cols
		if end > len(panels) {
			end = len(panels)
		}
		rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, panels[i:end]...))
	}

	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

func (m Model) renderHelp() string {
	mode := "table"
	if m.viewMode == ViewModeDetail {
		mode = "detail"
	}
	help := fmt.Sprintf(" [d] %s → %s | [s] sort | [r] refresh | [q] quit",
		mode, map[bool]string{true: "table", false: "detail"}[m.viewMode == ViewModeDetail])
	return dimStyle.Render(help)
}

// Helper types and functions

type gpuRow struct {
	nodeName string
	gpu      domain.GPUInfo
}

func (m Model) buildSortedRows() []gpuRow {
	var rows []gpuRow
	for _, nm := range m.metrics {
		for _, g := range nm.GPUs {
			rows = append(rows, gpuRow{nodeName: nm.NodeName, gpu: g})
		}
	}

	sort.Slice(rows, func(i, j int) bool {
		switch m.sortBy {
		case SortByUtil:
			return rows[i].gpu.UtilizationPercent > rows[j].gpu.UtilizationPercent
		case SortByMemory:
			return rows[i].gpu.MemoryUsagePercent() > rows[j].gpu.MemoryUsagePercent()
		case SortByTemp:
			return rows[i].gpu.TemperatureC > rows[j].gpu.TemperatureC
		default: // SortByNode
			if rows[i].nodeName == rows[j].nodeName {
				return rows[i].gpu.Index < rows[j].gpu.Index
			}
			return rows[i].nodeName < rows[j].nodeName
		}
	})

	return rows
}

func renderBar(percent float64, width int) string {
	filled := int(percent / 100 * float64(width))
	if filled > width {
		filled = width
	}
	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
}

func renderSparkline(data []float64, width int) string {
	if len(data) == 0 {
		return strings.Repeat(" ", width)
	}

	blocks := []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

	// Use last `width` points
	start := 0
	if len(data) > width {
		start = len(data) - width
	}
	visible := data[start:]

	var b strings.Builder
	// Pad with spaces if not enough data
	for i := 0; i < width-len(visible); i++ {
		b.WriteRune(' ')
	}
	for _, v := range visible {
		idx := int(v / 100 * float64(len(blocks)-1))
		if idx >= len(blocks) {
			idx = len(blocks) - 1
		}
		if idx < 0 {
			idx = 0
		}
		b.WriteRune(blocks[idx])
	}
	return b.String()
}

func utilToColor(pct float64) lipgloss.Color {
	switch {
	case pct >= 80:
		return hotGPU
	case pct >= 50:
		return warmGPU
	default:
		return coolGPU
	}
}

func tempToColor(temp int) lipgloss.Color {
	switch {
	case temp >= 80:
		return hotGPU
	case temp >= 60:
		return warmGPU
	default:
		return coolGPU
	}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}
```

**Step 2: Verify it compiles**

Run: `go build ./internal/tui/monitor/`
Expected: success

**Step 3: Commit**

```bash
git add internal/tui/monitor/view.go
git commit -m "feat: add table and detail view rendering for GPU monitor TUI"
```

---

### Task 6: Wire CLI command

**Files:**
- Modify: `internal/cli/cluster.go` — add `newClusterMonitorCmd()`

**Step 1: Add the monitor subcommand**

Add to `newClusterCmd()` after line 31 (after the last `cmd.AddCommand`):

```go
cmd.AddCommand(newClusterMonitorCmd())
```

Then add the function:

```go
func newClusterMonitorCmd() *cobra.Command {
	var interval int

	cmd := &cobra.Command{
		Use:   "monitor <cluster-name>",
		Short: "Monitor GPU usage across cluster nodes",
		Long: `Monitor GPU utilization, memory, temperature, and power for all nodes
in a cluster in real-time. Requires nvidia-smi on worker nodes.

Keys:
  d  Toggle table/detail view
  s  Cycle sort order
  r  Force refresh
  q  Quit`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clusterName := args[0]

			cfg, err := getConfig()
			if err != nil {
				return err
			}

			client := tailscale.NewClient(cfg.Tailscale.APIKey, cfg.Tailscale.Tailnet)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			// Get all devices to build device map
			allDevices, err := client.ListDevices(ctx)
			if err != nil {
				return fmt.Errorf("failed to list devices: %w", err)
			}

			deviceMap := make(map[string]*domain.Device)
			for _, d := range allDevices {
				deviceMap[d.ID] = d
			}

			// Find the cluster (search by name in all devices' context)
			// For now, we use the cluster name to find a stored cluster
			// TODO: Load cluster from repository when persistence is wired
			// Fallback: treat args as a list of device names
			cluster := findClusterByName(allDevices, clusterName)
			if cluster == nil {
				return fmt.Errorf("cluster '%s' not found", clusterName)
			}

			// Resolve cluster node devices
			var clusterDevices []*domain.Device
			for _, nodeID := range cluster.AllNodeIDs() {
				if d, ok := deviceMap[nodeID]; ok && d.CanSSH() {
					clusterDevices = append(clusterDevices, d)
				}
			}

			if len(clusterDevices) == 0 {
				return fmt.Errorf("no reachable nodes in cluster '%s'", clusterName)
			}

			// Create SSH executor and GPU collector
			sshExecutor := ssh.NewExecutor(ssh.Config{
				User:            cfg.SSH.User,
				PrivateKeyPath:  cfg.SSH.PrivateKeyPath,
				Port:            cfg.SSH.Port,
				UseTailscaleSSH: cfg.SSH.UseTailscaleSSH,
			})
			defer sshExecutor.Close()

			gpuCollector := ssh.NewGPUCollector(sshExecutor)

			// Launch TUI
			duration := time.Duration(interval) * time.Second
			model := monitor.NewModel(clusterName, clusterDevices, gpuCollector, duration)
			p := tea.NewProgram(model, tea.WithAltScreen())
			_, err = p.Run()
			return err
		},
	}

	cmd.Flags().IntVarP(&interval, "interval", "i", 3, "Refresh interval in seconds")

	return cmd
}
```

Note: also add these imports to the file:
- `"github.com/dave/naga/internal/infra/ssh"`
- `"github.com/dave/naga/internal/tui/monitor"`
- `tea "github.com/charmbracelet/bubbletea"`

And add a temporary helper (until repository is wired):
```go
// findClusterByName is a temporary helper until cluster persistence is wired.
// It returns nil — callers should load from repository instead.
func findClusterByName(_ []*domain.Device, _ string) *domain.Cluster {
	// TODO: Replace with repository lookup
	return nil
}
```

**Step 2: Verify it compiles**

Run: `go build ./cmd/naga/`
Expected: success

**Step 3: Commit**

```bash
git add internal/cli/cluster.go
git commit -m "feat: wire cluster monitor CLI command with bubbletea TUI"
```

---

### Task 7: Integration — connect to cluster repository

**Files:**
- Modify: `internal/cli/cluster.go` — replace `findClusterByName` with actual repository lookup

**Step 1: Replace the temporary helper**

Replace `findClusterByName` usage in `newClusterMonitorCmd` with repository-backed lookup. The pattern already exists in `cluster_usecase.go` — we need to initialize the SQLite repo and cluster usecase in the CLI, similar to how the web handler does it.

Look at `cmd/server/main.go` to see how repos are initialized, then replicate in the monitor command. The key change is:

```go
// Inside RunE, replace the findClusterByName call:
repos, err := sqlite.NewRepositories(cfg.Database.Path)
if err != nil {
    return fmt.Errorf("failed to open database: %w", err)
}
clusterUC := usecase.NewClusterUseCase(repos, nil)
cluster, err := clusterUC.GetCluster(ctx, clusterName)
if err != nil {
    return fmt.Errorf("cluster '%s' not found: %w", clusterName, err)
}
```

Remove the `findClusterByName` function.

**Step 2: Verify**

Run: `go build ./cmd/naga/`
Expected: success

**Step 3: Commit**

```bash
git add internal/cli/cluster.go
git commit -m "feat: connect cluster monitor to repository for cluster lookup"
```

---

### Task 8: End-to-end test and polish

**Step 1: Run all existing tests**

Run: `go test ./... -v`
Expected: all pass

**Step 2: Manual smoke test**

Run: `go build -o naga ./cmd/naga/ && ./naga cluster monitor --help`
Expected: shows help text with usage, flags, and key descriptions

**Step 3: Commit any fixes**

```bash
git add -A
git commit -m "test: verify GPU monitor builds and passes all tests"
```
