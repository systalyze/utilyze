package theme

import "charm.land/lipgloss/v2"

type Styles struct {
	Palette         Palette
	Spinner         lipgloss.Style
	Header          lipgloss.Style
	HeaderBold      lipgloss.Style
	HeaderLabel     lipgloss.Style
	HeaderSecondary lipgloss.Style
	ChartPanel      lipgloss.Style
	ChartAxis       lipgloss.Style
	ChartBorder     lipgloss.Style
	Compute         lipgloss.Style
	Memory          lipgloss.Style
	SMActivity      lipgloss.Style
	NVLink          lipgloss.Style
	PCIe            lipgloss.Style
	ComputeCeiling  lipgloss.Style
	DotOffline      string
	DotOnline       string
}

func NewStyles(dark bool) Styles {
	palette := NewPalette(dark)

	header := lipgloss.NewStyle().
		Background(palette.Surface).
		Foreground(palette.Text)
	panel := lipgloss.NewStyle().
		Background(palette.Panel).
		Foreground(palette.Text)

	return Styles{
		Palette:         palette,
		Spinner:         lipgloss.NewStyle().Foreground(palette.Negative),
		Header:          header,
		HeaderBold:      lipgloss.NewStyle().Inherit(header).Bold(true),
		HeaderLabel:     lipgloss.NewStyle().Inherit(header),
		HeaderSecondary: lipgloss.NewStyle().Inherit(header).Foreground(palette.TextMuted),
		ChartPanel:      panel,
		ChartAxis:       lipgloss.NewStyle().Inherit(panel).Foreground(palette.Text),
		ChartBorder: lipgloss.NewStyle().Inherit(panel).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(palette.Text).
			BorderBackground(palette.Panel),
		Compute:        lipgloss.NewStyle().Foreground(palette.Compute),
		Memory:         lipgloss.NewStyle().Foreground(palette.Memory),
		SMActivity:     lipgloss.NewStyle().Foreground(palette.SMActivity),
		NVLink:         lipgloss.NewStyle().Foreground(palette.NVLink),
		PCIe:           lipgloss.NewStyle().Foreground(palette.PCIe),
		ComputeCeiling: lipgloss.NewStyle().Foreground(palette.ComputeCeiling),
		DotOffline:     lipgloss.NewStyle().Inherit(header).Foreground(palette.Negative).Render("●"),
		DotOnline:      lipgloss.NewStyle().Inherit(header).Foreground(palette.Positive).Render("●"),
	}
}
