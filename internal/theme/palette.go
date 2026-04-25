package theme

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

type Palette struct {
	Text           color.Color
	TextMuted      color.Color
	Surface        color.Color
	Panel          color.Color
	Subtle         color.Color
	Positive       color.Color
	Negative       color.Color
	Compute        color.Color
	Memory         color.Color
	SMActivity     color.Color
	NVLink         color.Color
	PCIe           color.Color
	ComputeCeiling color.Color
}

func NewPalette(dark bool) Palette {
	if dark {
		return Palette{
			Text:           lipgloss.Color("#E5EEF7"),
			TextMuted:      lipgloss.Color("250"),
			Surface:        lipgloss.Color("#09121C"),
			Panel:          lipgloss.Color("#00060B"),
			Subtle:         lipgloss.Color("240"),
			Positive:       lipgloss.Color("46"),
			Negative:       lipgloss.Color("196"),
			Compute:        lipgloss.Color("#00D7FF"),
			Memory:         lipgloss.Color("#FF5F00"),
			SMActivity:     lipgloss.Color("#D75FFF"),
			NVLink:         lipgloss.Color("#EF4444"),
			PCIe:           lipgloss.Color("77"),
			ComputeCeiling: lipgloss.Color("#9B8BC7"),
		}
	}

	return Palette{
		Text:           lipgloss.Color("#111827"),
		TextMuted:      lipgloss.Color("#374151"),
		Surface:        lipgloss.Color("#E9EEF3"),
		Panel:          lipgloss.Color("#FFFFFF"),
		Subtle:         lipgloss.Color("#4B5563"),
		Positive:       lipgloss.Color("#15803D"),
		Negative:       lipgloss.Color("#C62828"),
		Compute:        lipgloss.Color("#00D7FF"),
		Memory:         lipgloss.Color("#FF5F00"),
		SMActivity:     lipgloss.Color("#D75FFF"),
		NVLink:         lipgloss.Color("#DC2626"),
		PCIe:           lipgloss.Color("#0F766E"),
		ComputeCeiling: lipgloss.Color("#7A6AA8"),
	}
}
