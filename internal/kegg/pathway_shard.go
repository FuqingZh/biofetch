package kegg

import (
	"fmt"
	"strings"
)

type pathwayPrefixGroup struct {
	prefix        string
	organismCodes []string
}

func normalizeOrganismPrefixes(values []string) ([]string, error) {
	seen := make(map[string]struct{})
	prefixes := make([]string, 0)
	for _, value := range values {
		for _, token := range strings.Split(value, ",") {
			prefix := strings.ToLower(strings.TrimSpace(token))
			if len(prefix) != 1 || prefix[0] < 'a' || prefix[0] > 'z' {
				return nil, fmt.Errorf("organism-prefix must contain exactly one ASCII letter a-z: %q", token)
			}
			if _, ok := seen[prefix]; ok {
				continue
			}
			seen[prefix] = struct{}{}
			prefixes = append(prefixes, prefix)
		}
	}
	if len(prefixes) == 0 {
		return nil, fmt.Errorf("organism-prefix must not be empty")
	}
	return prefixes, nil
}

func partitionOrganismsByPrefix(codes, prefixes []string, ruleOrder string) ([]pathwayPrefixGroup, error) {
	groups := make([]pathwayPrefixGroup, 0, len(prefixes))
	for _, prefix := range prefixes {
		matches := make([]string, 0)
		for _, code := range codes {
			normalized := normalizeKEGGOrganismCode(code)
			if strings.HasPrefix(normalized, prefix) {
				matches = append(matches, normalized)
			}
		}
		if len(matches) == 0 {
			return nil, fmt.Errorf("organism-prefix %q matched no organisms in KEGG genome catalog", prefix)
		}
		groups = append(groups, pathwayPrefixGroup{prefix: prefix, organismCodes: applyTraversalOrder(matches, ruleOrder)})
	}
	return groups, nil
}
