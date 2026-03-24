// Package components provides reusable UI components for the TUI.
package components

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Layout represents a layout section with flexible sizing.
type Layout struct {
	ID        string
	Content   string
	MinWidth  int
	MinHeight int
	Flex      float64 // Relative size (0 for fixed, >0 for flexible)
	Grow      bool    // Whether to grow to fill available space
}

// LayoutModel manages layout sections and sizing.
type LayoutModel struct {
	sections  []Layout
	width     int
	height    int
	direction lipgloss.Position // Vertical or Horizontal
	border    lipgloss.Border
	padding   int
	spacing   int
}

// NewLayoutModel creates a new layout manager.
func NewLayoutModel(direction lipgloss.Position) *LayoutModel {
	return &LayoutModel{
		sections:  make([]Layout, 0),
		direction: direction,
		border:    lipgloss.NormalBorder(),
		padding:   1,
		spacing:   1,
	}
}

// SetSize sets the overall layout dimensions.
func (m *LayoutModel) SetSize(width, height int) {
	m.width = width
	m.height = height
}

// SetDirection sets the layout direction (vertical or horizontal).
func (m *LayoutModel) SetDirection(direction lipgloss.Position) {
	m.direction = direction
}

// SetBorder sets the border style for sections.
func (m *LayoutModel) SetBorder(border lipgloss.Border) {
	m.border = border
}

// SetPadding sets the padding for sections.
func (m *LayoutModel) SetPadding(padding int) {
	m.padding = padding
}

// SetSpacing sets the spacing between sections.
func (m *LayoutModel) SetSpacing(spacing int) {
	m.spacing = spacing
}

// AddSection adds a new layout section.
func (m *LayoutModel) AddSection(id, content string, flex float64, grow bool) {
	m.sections = append(m.sections, Layout{
		ID:      id,
		Content: content,
		Flex:    flex,
		Grow:    grow,
	})
}

// UpdateSection updates the content of an existing section.
func (m *LayoutModel) UpdateSection(id, content string) bool {
	for i := range m.sections {
		if m.sections[i].ID == id {
			m.sections[i].Content = content
			return true
		}
	}
	return false
}

// GetSection returns a section by ID.
func (m *LayoutModel) GetSection(id string) *Layout {
	for i := range m.sections {
		if m.sections[i].ID == id {
			return &m.sections[i]
		}
	}
	return nil
}

// View renders the layout.
func (m *LayoutModel) View() string {
	if len(m.sections) == 0 {
		return ""
	}

	if m.direction == lipgloss.Top {
		return m.renderVertical()
	}
	return m.renderHorizontal()
}

// renderVertical renders sections vertically.
func (m *LayoutModel) renderVertical() string {
	if len(m.sections) == 0 {
		return ""
	}

	// Calculate heights
	heights := m.calculateVerticalHeights()

	// Render each section
	var renderedSections []string
	for i, section := range m.sections {
		if i >= len(heights) {
			break
		}

		// Apply border and padding
		style := lipgloss.NewStyle().
			Width(m.width - (2*m.padding + 2)).
			Height(heights[i]).
			Border(m.border).
			Padding(m.padding)

		// Truncate content if needed
		content := section.Content
		if lipgloss.Height(content) > heights[i] {
			// Simple truncation - in production, use viewport
			lines := lipgloss.Height(content)
			if lines > heights[i] {
				// Take last heights[i] lines
				// This is simplified - use viewport for proper scrolling
			}
		}

		renderedSections = append(renderedSections, style.Render(content))
	}

	// Join sections with spacing
	result := renderedSections[0]
	for i := 1; i < len(renderedSections); i++ {
		result += "\n" + m.spacingString() + renderedSections[i]
	}

	return result
}

// renderHorizontal renders sections horizontally.
func (m *LayoutModel) renderHorizontal() string {
	if len(m.sections) == 0 {
		return ""
	}

	// Calculate widths
	widths := m.calculateHorizontalWidths()

	// Render each section
	var renderedSections []string
	for i, section := range m.sections {
		if i >= len(widths) {
			break
		}

		// Apply border and padding
		style := lipgloss.NewStyle().
			Width(widths[i]).
			Height(m.height - (2*m.padding + 2)).
			Border(m.border).
			Padding(m.padding)

		renderedSections = append(renderedSections, style.Render(section.Content))
	}

	// Join sections with spacing
	return lipgloss.JoinHorizontal(lipgloss.Left, renderedSections...)
}

// calculateVerticalHeights calculates section heights for vertical layout.
func (m *LayoutModel) calculateVerticalHeights() []int {
	if len(m.sections) == 0 {
		return []int{}
	}

	heights := make([]int, len(m.sections))

	// Total spacing between sections
	totalSpacing := (len(m.sections) - 1) * m.spacing

	// Available height after spacing
	availableHeight := m.height - totalSpacing

	// Count flexible sections
	flexCount := 0
	totalFixedHeight := 0

	for i, section := range m.sections {
		if section.Flex > 0 {
			flexCount++
		} else {
			// Fixed height - use content height or minimum
			contentHeight := lipgloss.Height(section.Content)
			if section.MinHeight > 0 && contentHeight < section.MinHeight {
				heights[i] = section.MinHeight
			} else {
				heights[i] = contentHeight
			}
			totalFixedHeight += heights[i]
		}
	}

	// Distribute remaining height to flexible sections
	if flexCount > 0 {
		flexHeight := (availableHeight - totalFixedHeight) / flexCount

		for i, section := range m.sections {
			if section.Flex > 0 {
				heights[i] = int(float64(flexHeight) * section.Flex)
				if heights[i] < section.MinHeight {
					heights[i] = section.MinHeight
				}
			}
		}
	}

	return heights
}

// calculateHorizontalWidths calculates section widths for horizontal layout.
func (m *LayoutModel) calculateHorizontalWidths() []int {
	if len(m.sections) == 0 {
		return []int{}
	}

	widths := make([]int, len(m.sections))

	// Total spacing between sections
	totalSpacing := (len(m.sections) - 1) * m.spacing

	// Available width after spacing
	availableWidth := m.width - totalSpacing

	// Count flexible sections
	flexCount := 0
	totalFixedWidth := 0

	for i, section := range m.sections {
		if section.Flex > 0 {
			flexCount++
		} else {
			// Fixed width - use content width or minimum
			contentWidth := lipgloss.Width(section.Content)
			if section.MinWidth > 0 && contentWidth < section.MinWidth {
				widths[i] = section.MinWidth
			} else {
				widths[i] = contentWidth
			}
			totalFixedWidth += widths[i]
		}
	}

	// Distribute remaining width to flexible sections
	if flexCount > 0 {
		flexWidth := (availableWidth - totalFixedWidth) / flexCount

		for i, section := range m.sections {
			if section.Flex > 0 {
				widths[i] = int(float64(flexWidth) * section.Flex)
				if widths[i] < section.MinWidth {
					widths[i] = section.MinWidth
				}
			}
		}
	}

	return widths
}

// spacingString returns a spacing string between sections.
func (m *LayoutModel) spacingString() string {
	if m.spacing <= 0 {
		return ""
	}
	return strings.Repeat("\n", m.spacing)
}

// GridLayout represents a grid layout manager.
type GridLayout struct {
	rows     int
	cols     int
	sections [][]Layout
	width    int
	height   int
	border   lipgloss.Border
	padding  int
	gap      int
}

// NewGridLayout creates a new grid layout.
func NewGridLayout(rows, cols int) *GridLayout {
	sections := make([][]Layout, rows)
	for i := range sections {
		sections[i] = make([]Layout, cols)
	}

	return &GridLayout{
		rows:     rows,
		cols:     cols,
		sections: sections,
		border:   lipgloss.NormalBorder(),
		padding:  1,
		gap:      1,
	}
}

// SetSize sets the overall grid dimensions.
func (g *GridLayout) SetSize(width, height int) {
	g.width = width
	g.height = height
}

// SetCell sets the content of a specific cell.
func (g *GridLayout) SetCell(row, col int, id, content string) {
	if row >= 0 && row < g.rows && col >= 0 && col < g.cols {
		g.sections[row][col] = Layout{
			ID:      id,
			Content: content,
		}
	}
}

// View renders the grid layout.
func (g *GridLayout) View() string {
	if g.rows == 0 || g.cols == 0 {
		return ""
	}

	// Calculate cell dimensions
	cellWidth := (g.width - (g.cols+1)*g.gap) / g.cols
	cellHeight := (g.height - (g.rows+1)*g.gap) / g.rows

	// Render each row
	var rows []string
	for r := 0; r < g.rows; r++ {
		var cells []string
		for c := 0; c < g.cols; c++ {
			style := lipgloss.NewStyle().
				Width(cellWidth).
				Height(cellHeight).
				Border(g.border).
				Padding(g.padding)

			cells = append(cells, style.Render(g.sections[r][c].Content))
		}

		rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Left, cells...))
	}

	// Join rows with gap
	return lipgloss.JoinVertical(lipgloss.Top, rows...)
}
