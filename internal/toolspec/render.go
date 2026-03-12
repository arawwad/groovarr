package toolspec

import (
	"fmt"
	"strings"
)

func RenderPromptCatalog(specs []ToolSpec) string {
	var builder strings.Builder
	builder.WriteString("Tool manifest:\n")
	builder.WriteString("Use only the tools listed below. Pick the tool that best matches the user's intent.\n")
	for _, spec := range specs {
		builder.WriteString("- ")
		builder.WriteString(spec.Name)
		builder.WriteString(": ")
		builder.WriteString(spec.Description)
		parts := []string{"when: " + spec.UseWhen}
		if len(spec.Args) == 0 {
			parts = append(parts, "args: none")
		} else {
			parts = append(parts, "args: "+renderPromptArgs(spec.Args))
		}
		if spec.Schema != "" {
			parts = append(parts, "schema: "+spec.Schema)
		}
		if spec.Example != "" {
			parts = append(parts, "example: "+spec.Example)
		}
		builder.WriteString(" | ")
		builder.WriteString(strings.Join(parts, " | "))
		builder.WriteString("\n")
	}
	return strings.TrimSpace(builder.String())
}

func renderPromptArgs(args []ToolArgSpec) string {
	parts := make([]string, 0, len(args))
	for _, arg := range args {
		label := fmt.Sprintf("%s:%s", arg.Name, arg.Type)
		if arg.Required {
			label += "*"
		}
		parts = append(parts, label)
	}
	return strings.Join(parts, "; ")
}
