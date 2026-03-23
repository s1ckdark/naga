package monitor

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/dave/naga/internal/domain"
)

var (
	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("15")).
			Background(lipgloss.Color("55"))

	nodeStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("14"))

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("9"))

	dimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("8"))

	panelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			Padding(0, 1)
)

// View renders the TUI.
func (m Model) View() string {
	var b strings.Builder

	// Header
	title := fmt.Sprintf(" GPU Monitor: %s ", m.clusterName)
	if m.width > 0 {
		title = headerStyle.Width(m.width).Render(title)
	} else {
		title = headerStyle.Render(title)
	}
	b.WriteString(title)
	b.WriteString("\n")

	// Status line
	if m.loading {
		b.WriteString(dimStyle.Render("  Collecting metrics..."))
		b.WriteString("\n")
	}
	if m.lastErr != nil {
		b.WriteString(errorStyle.Render(fmt.Sprintf("  Error: %v", m.lastErr)))
		b.WriteString("\n")
	}

	if len(m.metrics) == 0 {
		if !m.loading {
			b.WriteString(dimStyle.Render("  No metrics available."))
			b.WriteString("\n")
		}
		b.WriteString("\n")
		b.WriteString(m.renderHelp())
		return b.String()
	}

	b.WriteString("\n")

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

func (m Model) renderTable() string {
	rows := m.buildSortedRows()
	if len(rows) == 0 {
		return dimStyle.Render("  No GPU data.\n")
	}

	barWidth := 20

	var b strings.Builder

	// Table header
	header := fmt.Sprintf("  %-14s %3s  %-18s  %-*s  %-*s  %5s  %7s",
		"NODE", "GPU", "NAME", barWidth+7, "UTIL", barWidth+7, "MEMORY", "TEMP", "POWER")
	b.WriteString(dimStyle.Render(header))
	b.WriteString("\n")

	for _, r := range rows {
		utilBar := renderBar(r.gpu.UtilizationPercent, barWidth)
		memPct := r.gpu.MemoryUsagePercent()
		memBar := renderBar(memPct, barWidth)

		utilColor := utilToColor(r.gpu.UtilizationPercent)
		tempColor := tempToColor(r.gpu.TemperatureC)

		nodeName := truncate(r.nodeName, 14)

		line := fmt.Sprintf("  %s %3d  %-18s  %s %s  %s %s  %s  %6.0fW",
			nodeStyle.Render(fmt.Sprintf("%-14s", nodeName)),
			r.gpu.Index,
			truncate(r.gpu.Name, 18),
			utilColor.Render(utilBar),
			dimStyle.Render(fmt.Sprintf("%5.1f%%", r.gpu.UtilizationPercent)),
			utilToColor(memPct).Render(memBar),
			dimStyle.Render(fmt.Sprintf("%5.1f%%", memPct)),
			tempColor.Render(fmt.Sprintf("%3d°C", r.gpu.TemperatureC)),
			r.gpu.PowerDrawW,
		)
		b.WriteString(line)
		b.WriteString("\n")
	}

	// Errors
	for _, nm := range m.metrics {
		if nm.Error != "" {
			b.WriteString(fmt.Sprintf("  %s %s\n",
				nodeStyle.Render(fmt.Sprintf("%-14s", truncate(nm.NodeName, 14))),
				errorStyle.Render(nm.Error)))
		}
	}

	return b.String()
}

func (m Model) renderDetail() string {
	rows := m.buildSortedRows()
	if len(rows) == 0 {
		return dimStyle.Render("  No GPU data.\n")
	}

	panelWidth := 42
	panelsPerRow := 1
	if m.width > 0 {
		panelsPerRow = m.width / (panelWidth + 2)
		if panelsPerRow < 1 {
			panelsPerRow = 1
		}
	}

	var panels []string
	for _, r := range rows {
		panels = append(panels, m.renderPanel(r, panelWidth))
	}

	var b strings.Builder
	for i := 0; i < len(panels); i += panelsPerRow {
		end := i + panelsPerRow
		if end > len(panels) {
			end = len(panels)
		}
		row := lipgloss.JoinHorizontal(lipgloss.Top, panels[i:end]...)
		b.WriteString(row)
		b.WriteString("\n")
	}

	return b.String()
}

func (m Model) renderPanel(r gpuRow, width int) string {
	innerWidth := width - 4 // account for border + padding
	if innerWidth < 10 {
		innerWidth = 10
	}

	var lines []string

	// Title
	title := fmt.Sprintf("%s GPU:%d", truncate(r.nodeName, 20), r.gpu.Index)
	lines = append(lines, nodeStyle.Render(title))
	lines = append(lines, dimStyle.Render(truncate(r.gpu.Name, innerWidth)))

	// Sparkline
	key := fmt.Sprintf("%s:%d", r.deviceID, r.gpu.Index)
	hist := m.utilHistory[key]
	sparkWidth := innerWidth
	if sparkWidth > 30 {
		sparkWidth = 30
	}
	lines = append(lines, fmt.Sprintf("Util: %s %5.1f%%",
		renderSparkline(hist, sparkWidth), r.gpu.UtilizationPercent))

	// Memory bar
	memPct := r.gpu.MemoryUsagePercent()
	memBarW := innerWidth - 18
	if memBarW < 5 {
		memBarW = 5
	}
	lines = append(lines, fmt.Sprintf("Mem:  %s %5.1f%% %dM/%dM",
		utilToColor(memPct).Render(renderBar(memPct, memBarW)),
		memPct, r.gpu.MemoryUsedMB, r.gpu.MemoryTotalMB))

	// Temp & Power
	lines = append(lines, fmt.Sprintf("Temp: %s   Power: %.0fW/%.0fW",
		tempToColor(r.gpu.TemperatureC).Render(fmt.Sprintf("%d°C", r.gpu.TemperatureC)),
		r.gpu.PowerDrawW, r.gpu.PowerLimitW))

	content := strings.Join(lines, "\n")
	return panelStyle.Width(width).Render(content)
}

type gpuRow struct {
	nodeName string
	deviceID string
	gpu      domain.GPUInfo
}

func (m Model) buildSortedRows() []gpuRow {
	var rows []gpuRow
	for _, nm := range m.metrics {
		for _, g := range nm.GPUs {
			rows = append(rows, gpuRow{
				nodeName: nm.NodeName,
				deviceID: nm.DeviceID,
				gpu:      g,
			})
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
			if rows[i].nodeName != rows[j].nodeName {
				return rows[i].nodeName < rows[j].nodeName
			}
			return rows[i].gpu.Index < rows[j].gpu.Index
		}
	})

	return rows
}

func renderBar(percent float64, width int) string {
	if width <= 0 {
		return ""
	}
	filled := int(math.Round(percent / 100 * float64(width)))
	if filled > width {
		filled = width
	}
	if filled < 0 {
		filled = 0
	}
	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
}

func renderSparkline(data []float64, width int) string {
	if width <= 0 {
		return ""
	}
	blocks := []rune("▁▂▃▄▅▆▇█")

	// Use the last `width` data points
	start := 0
	if len(data) > width {
		start = len(data) - width
	}
	visible := data[start:]

	var sb strings.Builder
	// Pad with spaces if data is shorter than width
	for i := 0; i < width-len(visible); i++ {
		sb.WriteRune(' ')
	}
	for _, v := range visible {
		idx := int(v / 100 * float64(len(blocks)-1))
		if idx < 0 {
			idx = 0
		}
		if idx >= len(blocks) {
			idx = len(blocks) - 1
		}
		sb.WriteRune(blocks[idx])
	}
	return sb.String()
}

func utilToColor(pct float64) lipgloss.Style {
	switch {
	case pct >= 80:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("9")) // red
	case pct >= 50:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("208")) // orange
	default:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("10")) // green
	}
}

func tempToColor(temp int) lipgloss.Style {
	switch {
	case temp >= 80:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("9")) // red
	case temp >= 50:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("208")) // orange
	default:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("10")) // green
	}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	if max <= 3 {
		return s[:max]
	}
	return s[:max-3] + "..."
}

func (m Model) renderHelp() string {
	sortNames := map[SortField]string{
		SortByNode:   "node",
		SortByUtil:   "util",
		SortByMemory: "memory",
		SortByTemp:   "temp",
	}
	viewName := "table"
	if m.viewMode == ViewModeDetail {
		viewName = "detail"
	}
	help := fmt.Sprintf("  q: quit  d: toggle view (%s)  s: sort (%s)  r: refresh",
		viewName, sortNames[m.sortBy])
	return dimStyle.Render(help)
}
