package formatting

import (
	"fmt"
	"html"
	"strings"
)

func FormatCurrency(amount float64, currency string, style string) string {
	if strings.EqualFold(style, "aligned") && strings.EqualFold(currency, "usd") {
		return fmt.Sprintf("$%.2f", amount)
	}
	return fmt.Sprintf("%.2f %s", amount, strings.ToLower(currency))
}

func BuildAlignedBlock(rows [][2]string) string {
	maxWidth := 0
	for _, row := range rows {
		if width := len([]rune(row[0])); width > maxWidth {
			maxWidth = width
		}
	}

	lines := make([]string, 0, len(rows))
	for _, row := range rows {
		padding := maxWidth - len([]rune(row[0]))
		lines = append(lines, fmt.Sprintf("%s%s：%s", row[0], strings.Repeat("　", padding), row[1]))
	}

	return "<pre>" + html.EscapeString(strings.Join(lines, "\n")) + "</pre>"
}
