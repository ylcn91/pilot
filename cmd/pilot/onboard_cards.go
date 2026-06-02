package main

import (
	"fmt"
	"strings"
)

func printCardRow(cards []SummaryCard) {
	// Print each card line by line
	// Line 1: top borders
	for i, card := range cards {
		fmt.Print(renderCardTopBorder(card.Title))
		if i < len(cards)-1 {
			fmt.Print(" ")
		}
	}
	fmt.Println()

	// Line 2: empty
	for i := range cards {
		fmt.Print(renderCardEmptyLine())
		if i < len(cards)-1 {
			fmt.Print(" ")
		}
	}
	fmt.Println()

	// Line 3: title and value
	for i, card := range cards {
		fmt.Print(renderCardTitleLine(card.Title, card.Value))
		if i < len(cards)-1 {
			fmt.Print(" ")
		}
	}
	fmt.Println()

	// Line 4: empty
	for i := range cards {
		fmt.Print(renderCardEmptyLine())
		if i < len(cards)-1 {
			fmt.Print(" ")
		}
	}
	fmt.Println()

	// Line 5: line1
	for i, card := range cards {
		fmt.Print(renderCardContentLine(card.Line1))
		if i < len(cards)-1 {
			fmt.Print(" ")
		}
	}
	fmt.Println()

	// Line 6: line2
	for i, card := range cards {
		fmt.Print(renderCardContentLine(card.Line2))
		if i < len(cards)-1 {
			fmt.Print(" ")
		}
	}
	fmt.Println()

	// Line 7: empty
	for i := range cards {
		fmt.Print(renderCardEmptyLine())
		if i < len(cards)-1 {
			fmt.Print(" ")
		}
	}
	fmt.Println()

	// Line 8: bottom border
	for i := range cards {
		fmt.Print(renderCardBottomBorder())
		if i < len(cards)-1 {
			fmt.Print(" ")
		}
	}
	fmt.Println()
}

func renderCardTopBorder(title string) string {
	// ╭───────────────────╮ (21 chars)
	dashCount := summaryCardWidth - 2
	return onboardBorderStyle.Render("╭" + strings.Repeat("─", dashCount) + "╮")
}

func renderCardBottomBorder() string {
	// ╰───────────────────╯ (21 chars)
	dashCount := summaryCardWidth - 2
	return onboardBorderStyle.Render("╰" + strings.Repeat("─", dashCount) + "╯")
}

func renderCardEmptyLine() string {
	// │                   │ (21 chars)
	spaceCount := summaryCardWidth - 2
	return onboardBorderStyle.Render("│") +
		strings.Repeat(" ", spaceCount) +
		onboardBorderStyle.Render("│")
}

func renderCardTitleLine(title, value string) string {
	// │  TITLE    value  │
	return onboardBorderStyle.Render("│") + " " +
		onboardLabelStyle.Render(title) + "  " +
		onboardValueStyle.Render(padRight(value, summaryCardInnerWidth-len(title)-4)) + " " +
		onboardBorderStyle.Render("│")
}

func renderCardContentLine(content string) string {
	// │  content...       │
	if content == "" {
		return renderCardEmptyLine()
	}
	truncated := truncate(content, summaryCardInnerWidth)
	padded := padRight("  "+truncated, summaryCardInnerWidth)
	return onboardBorderStyle.Render("│") + " " +
		onboardDimStyle.Render(padded) + " " +
		onboardBorderStyle.Render("│")
}

func padRight(s string, width int) string {
	if len(s) >= width {
		return s[:width]
	}
	return s + strings.Repeat(" ", width-len(s))
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}
