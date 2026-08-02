package handler

import (
	"regexp"
)

var lineField = regexp.MustCompile(`^lines\[(\d+)\]\.(description|quantity|unit_price|account_id)$`)

// transportFormFields maps module field paths onto legacy template field keys.
func transportFormFields(fields map[string]string) map[string]string {
	result := make(map[string]string, len(fields))
	for key, value := range fields {
		match := lineField.FindStringSubmatch(key)
		if match == nil {
			result[key] = value
			continue
		}
		if _, ok := result["lines"]; !ok {
			result["lines"] = value
		}
		if suffix, ok := map[string]string{"description": "desc", "quantity": "quantity", "unit_price": "price", "account_id": "account"}[match[2]]; ok {
			result["line_"+match[1]+"_"+suffix] = value
		}
	}
	return result
}
