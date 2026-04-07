// Copyright 2026 ICAP Mock

package tui

import (
	"github.com/charmbracelet/lipgloss"
)

// Color variables for the theme — adaptive for light and dark terminals.
var (
	// Primary colors.
	ColorPrimary   = lipgloss.AdaptiveColor{Light: "125", Dark: "205"} // Hot pink/magenta
	ColorSecondary = lipgloss.AdaptiveColor{Light: "26", Dark: "75"}   // Light cyan
	ColorAccent    = lipgloss.AdaptiveColor{Light: "130", Dark: "208"} // Orange

	// Status colors.
	ColorSuccess = lipgloss.AdaptiveColor{Light: "28", Dark: "46"}   // Green
	ColorWarning = lipgloss.AdaptiveColor{Light: "130", Dark: "208"} // Orange
	ColorError   = lipgloss.AdaptiveColor{Light: "124", Dark: "196"} // Red
	ColorInfo    = lipgloss.AdaptiveColor{Light: "26", Dark: "75"}   // Cyan

	// Neutral colors.
	ColorForeground = lipgloss.AdaptiveColor{Light: "16", Dark: "250"}  // Foreground
	ColorBackground = lipgloss.AdaptiveColor{Light: "255", Dark: "235"} // Background
	ColorMuted      = lipgloss.AdaptiveColor{Light: "243", Dark: "240"} // Muted gray
	ColorBorder     = lipgloss.AdaptiveColor{Light: "250", Dark: "245"} // Border gray

	// Log level colors.
	ColorLogDebug = lipgloss.AdaptiveColor{Light: "243", Dark: "240"} // Dim gray
	ColorLogInfo  = lipgloss.AdaptiveColor{Light: "26", Dark: "75"}   // Cyan
	ColorLogWarn  = lipgloss.AdaptiveColor{Light: "130", Dark: "208"} // Orange
	ColorLogError = lipgloss.AdaptiveColor{Light: "124", Dark: "196"} // Red
)

// Style variables for UI elements.
var (
	// Title style - bold, primary color with padding.
	TitleStyle = lipgloss.NewStyle().
			Foreground(ColorPrimary).
			Bold(true).
			Padding(0, 1)

	// Subtitle style - muted color with padding.
	SubtitleStyle = lipgloss.NewStyle().
			Foreground(ColorMuted).
			Padding(0, 1)

	// Panel style - bordered with padding.
	PanelStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(ColorBorder).
			Padding(1)

	// Content style - main content area.
	ContentStyle = lipgloss.NewStyle().
			Foreground(ColorForeground).
			Padding(0, 2)

	// Header styles.
	HeaderStyle = lipgloss.NewStyle().
			Background(ColorBackground).
			Foreground(ColorForeground).
			Bold(true).
			Padding(0, 1)

	HeaderTitleStyle = lipgloss.NewStyle().
				Foreground(ColorPrimary).
				Bold(true)

	HeaderStatusStyle = lipgloss.NewStyle().
				Foreground(ColorSuccess)

	// Tab styles.
	TabActiveStyle = lipgloss.NewStyle().
			Foreground(ColorPrimary).
			Bold(true).
			Underline(true).
			Padding(0, 2)

	TabInactiveStyle = lipgloss.NewStyle().
				Foreground(ColorMuted).
				Padding(0, 2)

	TabShortcutStyle = lipgloss.NewStyle().
				Foreground(ColorAccent).
				Bold(true)

	// Footer styles.
	FooterStyle = lipgloss.NewStyle().
			Background(ColorBackground).
			Foreground(ColorMuted).
			Padding(0, 1)

	FooterKeyStyle = lipgloss.NewStyle().
			Foreground(ColorSecondary).
			Bold(true)

	FooterDescStyle = lipgloss.NewStyle().
			Foreground(ColorForeground)

	// Log level styles.
	LogDebugStyle = lipgloss.NewStyle().
			Foreground(ColorLogDebug)

	LogInfoStyle = lipgloss.NewStyle().
			Foreground(ColorLogInfo)

	LogWarnStyle = lipgloss.NewStyle().
			Foreground(ColorLogWarn)

	LogErrorStyle = lipgloss.NewStyle().
			Foreground(ColorLogError).
			Bold(true)

	// Status indicator styles.
	StatusRunningStyle = lipgloss.NewStyle().
				Foreground(ColorSuccess).
				Bold(true)

	StatusStoppedStyle = lipgloss.NewStyle().
				Foreground(ColorError).
				Bold(true)

	StatusWarningStyle = lipgloss.NewStyle().
				Foreground(ColorWarning).
				Bold(true)

	// Metric styles.
	MetricLabelStyle = lipgloss.NewStyle().
				Foreground(ColorMuted).
				Padding(0, 1)

	MetricValueStyle = lipgloss.NewStyle().
				Foreground(ColorPrimary).
				Bold(true)

	// Button styles.
	ButtonStyle = lipgloss.NewStyle().
			Foreground(ColorForeground).
			Background(ColorBorder).
			Padding(0, 2)

	ButtonActiveStyle = lipgloss.NewStyle().
				Foreground(ColorBackground).
				Background(ColorPrimary).
				Bold(true).
				Padding(0, 2)

	// Help styles.
	HelpKeyStyle = lipgloss.NewStyle().
			Foreground(ColorSecondary).
			Bold(true).
			Padding(0, 1)

	HelpDescStyle = lipgloss.NewStyle().
			Foreground(ColorMuted)
)

// Theme represents the current UI theme.
type Theme struct {
	Primary    lipgloss.AdaptiveColor
	Secondary  lipgloss.AdaptiveColor
	Accent     lipgloss.AdaptiveColor
	Success    lipgloss.AdaptiveColor
	Warning    lipgloss.AdaptiveColor
	Error      lipgloss.AdaptiveColor
	Info       lipgloss.AdaptiveColor
	Foreground lipgloss.AdaptiveColor
	Background lipgloss.AdaptiveColor
	Muted      lipgloss.AdaptiveColor
	Border     lipgloss.AdaptiveColor
}

// DefaultTheme returns the default color theme.
func DefaultTheme() Theme {
	return Theme{
		Primary:    ColorPrimary,
		Secondary:  ColorSecondary,
		Accent:     ColorAccent,
		Success:    ColorSuccess,
		Warning:    ColorWarning,
		Error:      ColorError,
		Info:       ColorInfo,
		Foreground: ColorForeground,
		Background: ColorBackground,
		Muted:      ColorMuted,
		Border:     ColorBorder,
	}
}

// GetLogLevelStyle returns the appropriate style for a log level.
func GetLogLevelStyle(level string) lipgloss.Style {
	switch level {
	case "DEBUG":
		return LogDebugStyle
	case "INFO":
		return LogInfoStyle
	case "WARN":
		return LogWarnStyle
	case "ERROR":
		return LogErrorStyle
	default:
		return SubtitleStyle
	}
}

// GetStatusStyle returns the appropriate style for a server status.
func GetStatusStyle(status string) lipgloss.Style {
	switch status {
	case "running":
		return StatusRunningStyle
	case "stopped":
		return StatusStoppedStyle
	default:
		return StatusWarningStyle
	}
}
