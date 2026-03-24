// Package components provides reusable UI components for the TUI.
//
// Navigation Components:
//
// - HeaderModel: Displays application title and server status
// - FooterModel: Displays key bindings and status messages
// - TabsModel: Provides tab-based navigation between screens
// - SidebarModel: Provides a list-based sidebar menu
// - LayoutModel: Manages flexible layout sections (vertical/horizontal)
// - MenuModel: Provides dropdown menu functionality
//
// Keyboard Navigation:
//
// Global shortcuts:
// - 1-6: Quick navigation to screens
// - Tab: Navigate to next screen/tab
// - Shift+Tab: Navigate to previous screen/tab
// - Esc: Return to previous screen
// - q/Ctrl+C: Quit application
//
// Component-specific shortcuts:
// - Arrow keys/Up/Down: Navigate in lists
// - Enter: Select item
// - Space: Toggle selection
//
// Usage Example:
//
//	// Create components
//	header := components.NewHeaderModel("My App", "1.0.0")
//	footer := components.NewFooterModel()
//	tabs := components.NewTabsModel()
//
//	// Configure components
//	tabs.SetTabs([]components.Tab{
//		{ID: "dashboard", Title: "Dashboard", Shortcut: "1"},
//		{ID: "settings", Title: "Settings", Shortcut: "2"},
//	})
//
//	footer.SetKeyBindings(components.DefaultKeyBindings())
//
//	// Render in View method
//	func (m *Model) View() string {
//		return lipgloss.JoinVertical(
//			lipgloss.Left,
//			header.View(),
//			tabs.View(),
//			m.renderContent(),
//			footer.View(),
//		)
//	}
package components
