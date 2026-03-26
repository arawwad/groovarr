package toolspec

import "strings"

func RenderPromptCategorySummary(categories []CategorySpec) string {
	var builder strings.Builder
	builder.WriteString("Tool groups:\n")
	builder.WriteString("Detailed tool names and args are injected separately for the relevant groups on each turn.\n")
	for _, category := range categories {
		builder.WriteString("- ")
		builder.WriteString(category.Name)
		builder.WriteString(": ")
		builder.WriteString(category.Description)
		builder.WriteString("\n")
	}
	return strings.TrimSpace(builder.String())
}
