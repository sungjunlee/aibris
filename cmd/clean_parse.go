package cmd

import (
	"fmt"
	"strings"

	"github.com/sungjunlee/aibris/internal/types"
)

var validCleanCategories = []types.Category{
	types.CategoryWorktree,
	types.CategoryNodeModules,
	types.CategoryBuildCache,
	types.CategoryOtherCache,
	types.CategoryAgentState,
	types.CategoryAILogs,
}

var validCleanTools = []types.Tool{
	types.ToolCodex,
	types.ToolClaude,
	types.ToolCursor,
	types.ToolWindsurf,
	types.ToolNodeModules,
	types.ToolUnknown,
	types.ToolBuildCache,
	types.ToolPipCache,
	types.ToolAILogs,
}

func parseCleanCategories(raw string) ([]types.Category, error) {
	values, err := parseCleanSelector(raw, "category", categoryStrings(validCleanCategories))
	if err != nil {
		return nil, err
	}
	categories := make([]types.Category, len(values))
	for i, value := range values {
		categories[i] = types.Category(value)
	}
	return categories, nil
}

func parseCleanTools(raw string) ([]types.Tool, error) {
	values, err := parseCleanSelector(raw, "tool", toolStrings(validCleanTools))
	if err != nil {
		return nil, err
	}
	tools := make([]types.Tool, len(values))
	for i, value := range values {
		tools[i] = types.Tool(value)
	}
	return tools, nil
}

func parseCleanSelector(raw, flag string, valid []string) ([]string, error) {
	if raw == "" {
		return nil, nil
	}
	allowed := make(map[string]bool, len(valid))
	for _, value := range valid {
		allowed[value] = true
	}
	seen := make(map[string]bool)
	var values []string
	for _, value := range strings.Split(raw, ",") {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if !allowed[value] {
			return nil, fmt.Errorf("invalid --%s value %q; valid values: %s", flag, value, strings.Join(valid, ", "))
		}
		if !seen[value] {
			seen[value] = true
			values = append(values, value)
		}
	}
	if len(values) == 0 {
		return nil, fmt.Errorf("--%s requires at least one value; valid values: %s", flag, strings.Join(valid, ", "))
	}
	return values, nil
}

func categoryStrings(values []types.Category) []string {
	result := make([]string, len(values))
	for i, value := range values {
		result[i] = string(value)
	}
	return result
}

func toolStrings(values []types.Tool) []string {
	result := make([]string, len(values))
	for i, value := range values {
		result[i] = string(value)
	}
	return result
}
