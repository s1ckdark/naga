package monitor

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dave/naga/internal/domain"
	"github.com/dave/naga/internal/infra/ssh"
)

// ViewMode controls which display mode the TUI uses.
type ViewMode int

const (
	ViewModeTable  ViewMode = iota
	ViewModeDetail
)

// SortField controls which column the table is sorted by.
type SortField int

const (
	SortByNode SortField = iota
	SortByUtil
	SortByMemory
	SortByTemp
	sortFieldCount
)

// Model is the Bubble Tea model for the GPU monitor TUI.
type Model struct {
	clusterName string
	devices     []*domain.Device
	collector   *ssh.GPUCollector
	interval    time.Duration

	metrics     []*domain.GPUNodeMetrics
	viewMode    ViewMode
	sortBy      SortField
	width       int
	height      int
	loading     bool
	lastErr     error
	utilHistory map[string][]float64
}

// NewModel creates a new GPU monitor TUI model.
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

type tickMsg time.Time
type metricsMsg struct {
	metrics []*domain.GPUNodeMetrics
	err     error
}

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

// Init returns the initial commands for the TUI.
func (m Model) Init() tea.Cmd {
	return tea.Batch(m.collectMetrics(), m.tick())
}

// Update handles messages and updates the model.
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

func (m *Model) updateHistory() {
	const maxHistory = 30
	for _, nm := range m.metrics {
		for _, g := range nm.GPUs {
			key := fmt.Sprintf("%s:%d", nm.DeviceID, g.Index)
			h := m.utilHistory[key]
			h = append(h, g.UtilizationPercent)
			if len(h) > maxHistory {
				h = h[len(h)-maxHistory:]
			}
			m.utilHistory[key] = h
		}
	}
}
