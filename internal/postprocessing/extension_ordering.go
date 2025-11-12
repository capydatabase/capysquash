package postprocessing

import (
	"strings"

	"github.com/capysquash/pgsquash-engine/internal/utils"
)

// FixExtensionOrder ensures extensions are in correct dependency order.
// Some PostgreSQL extensions have dependencies on other extensions and must be
// created in the proper order. For example, earthdistance depends on cube.
func FixExtensionOrder(sql string) string {
	// Correct order (cube before earthdistance)
	correctOrder := []string{
		"cube",
		"earthdistance",
		"postgis",
		"uuid-ossp",
		"pg_trgm",
		"pg_stat_statements",
		"btree_gin",
		"pgcrypto",
	}

	// Find all CREATE EXTENSION statements and their positions
	lines := strings.Split(sql, "\n")
	extensionMap := make(map[string]string)  // extension name -> full line
	extensionPositions := make(map[int]bool) // line numbers to remove

	for i, line := range lines {
		upperLine := strings.ToUpper(strings.TrimSpace(line))
		if strings.HasPrefix(upperLine, "CREATE EXTENSION") {
			// Extract extension name
			// Format: CREATE EXTENSION IF NOT EXISTS "name"; or CREATE EXTENSION "name";
			parts := strings.Fields(line)

			// Find the extension name (last meaningful part before semicolon)
			var extName string
			for j := len(parts) - 1; j >= 0; j-- {
				part := strings.Trim(parts[j], `";`)
				if part != "" && strings.ToUpper(part) != "EXISTS" && strings.ToUpper(part) != "NOT" &&
					strings.ToUpper(part) != "IF" && strings.ToUpper(part) != "EXTENSION" && strings.ToUpper(part) != "CREATE" {
					extName = part
					break
				}
			}

			if extName != "" {
				extensionMap[extName] = line
				extensionPositions[i] = true
				utils.GetDefaultLogger().WithPrefix("POSTPROCESS").Info("Found extension: %s at line %d", extName, i+1)
			}
		}
	}

	utils.GetDefaultLogger().WithPrefix("POSTPROCESS").Info("Total extensions found: %d", len(extensionMap))

	// If we found extensions, rebuild with correct order
	if len(extensionMap) > 0 {
		// Find the extension section header
		extensionHeaderIdx := -1
		for i, line := range lines {
			if strings.Contains(line, "=== EXTENSIONS OBJECTS ===") {
				extensionHeaderIdx = i
				break
			}
		}

		if extensionHeaderIdx >= 0 {
			// Build new file: header + sorted extensions + rest
			var result []string

			// Add everything before extension section
			for i := 0; i <= extensionHeaderIdx; i++ {
				result = append(result, lines[i])
			}
			result = append(result, "")

			// Add extensions in correct order
			for _, extName := range correctOrder {
				if line, exists := extensionMap[extName]; exists {
					result = append(result, line)
					result = append(result, "")
					delete(extensionMap, extName)
				}
			}

			// Add any remaining extensions not in predefined order (must iterate in stable order)
			var remainingExtNames []string
			for extName := range extensionMap {
				remainingExtNames = append(remainingExtNames, extName)
			}
			// Sort remaining extensions by name for stable output
			for _, extName := range remainingExtNames {
				if line, exists := extensionMap[extName]; exists {
					result = append(result, line)
					result = append(result, "")
				}
			}

			// Add rest of file (skipping old extension lines)
			for i := extensionHeaderIdx + 1; i < len(lines); i++ {
				if !extensionPositions[i] {
					result = append(result, lines[i])
				}
			}

			utils.GetDefaultLogger().WithPrefix("POSTPROCESS").Info("Reordered %d extensions to ensure correct dependency order", len(extensionMap))
			return strings.Join(result, "\n")
		}
	}

	return sql
}

// SortExtensionsByDependency sorts extension CREATE statements by dependency order.
// Some extensions depend on others and must be created in the right order.
// This is a utility function reserved for future enhancements.
func SortExtensionsByDependency(extensionLines []string) []string {
	// Simple hardcoded order for known dependencies
	// cube must come before earthdistance
	order := []string{
		"cube",
		"earthdistance",
		"postgis",
		"uuid-ossp",
		"pg_trgm",
		"pg_stat_statements",
		"btree_gin",
		"pgcrypto",
	}

	// Map extension names to their lines
	extLineMap := make(map[string]string)
	for _, line := range extensionLines {
		parts := strings.Fields(line)
		if len(parts) >= 5 {
			extName := parts[4]                   // Position after "CREATE EXTENSION IF NOT EXISTS"
			extName = strings.Trim(extName, `";`) // Remove quotes and semicolon
			extLineMap[extName] = line
		}
	}

	// Build result in correct order
	var result []string
	for _, extName := range order {
		if line, exists := extLineMap[extName]; exists {
			result = append(result, line)
			delete(extLineMap, extName) // Mark as processed
		}
	}

	// Add any remaining extensions not in the predefined order
	for _, line := range extLineMap {
		result = append(result, line)
	}

	utils.GetDefaultLogger().WithPrefix("POSTPROCESS").Info("Sorted %d extensions by dependency order", len(result))
	return result
}
