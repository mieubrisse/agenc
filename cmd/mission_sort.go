package cmd

import (
	"sort"

	"github.com/odyssey/agenc/internal/database"
)

// sortMissionsForPicker sorts missions in-place using two tiers:
//  1. Missions with claude_state "needs_attention" float to the top
//  2. Sorted by COALESCE(last_user_prompt_at, created_at) DESC so brand-new
//     unprompted missions interleave with prompted ones by user-interaction time
func sortMissionsForPicker(missions []*database.Mission) {
	sort.SliceStable(missions, func(i, j int) bool {
		mi, mj := missions[i], missions[j]

		// Tier 1: needs_attention first
		iAttn := mi.ClaudeState != nil && *mi.ClaudeState == "needs_attention"
		jAttn := mj.ClaudeState != nil && *mj.ClaudeState == "needs_attention"
		if iAttn != jAttn {
			return iAttn
		}

		// Tier 2: COALESCE(last_user_prompt_at, created_at) DESC
		iTime := mi.CreatedAt
		if mi.LastUserPromptAt != nil {
			iTime = *mi.LastUserPromptAt
		}
		jTime := mj.CreatedAt
		if mj.LastUserPromptAt != nil {
			jTime = *mj.LastUserPromptAt
		}
		return iTime.After(jTime)
	})
}
