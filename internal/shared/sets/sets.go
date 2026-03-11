package sets

import "sort"

func SortedKeys(setText map[string]struct{}) []string {
	values := make([]string, 0, len(setText))
	for key := range setText {
		values = append(values, key)
	}
	sort.Strings(values)
	return values
}
