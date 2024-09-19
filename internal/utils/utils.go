package utils

// MatchesTags checks if the deployment's tags match the rollout's tags
func MatchesTags(deploymentTags, rolloutTags []string) bool {
	if len(rolloutTags) == 0 {
		return true
	}
	for _, rt := range rolloutTags {
		found := false
		for _, dt := range deploymentTags {
			if rt == dt {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// MatchesCategory checks if the deployment's category matches the rollout's category
func MatchesCategory(deploymentCategory, rolloutCategory string) bool {
	if rolloutCategory == "" {
		return true
	}
	return deploymentCategory == rolloutCategory
}
