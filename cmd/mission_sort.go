package cmd

import (
	"sort"

	"github.com/odyssey/agenc/internal/database"
)

// sortMissionsForPicker sorts missions in-place using three tiers:
//  1. Missions with claude_state "needs_attention" float to the top
//  2. Sorted by last_user_prompt_at DESC (nil sorts after non-nil)
//  3. Fallback to COALESCE(last_heartbeat, created_at) DESC
func sortMissionsForPicker(missions []*database.Mission) {
	sort.SliceStable(missions, func(i, j int) bool {
		mi, mj := missions[i], missions[j]

		// Tier 1: needs_attention first
		iAttn := mi.ClaudeState != nil && *mi.ClaudeState == "needs_attention"
		jAttn := mj.ClaudeState != nil && *mj.ClaudeState == "needs_attention"
		if iAttn != jAttn {
			return iAttn
		}

		// Tier 2: sort by last_user_prompt_at DESC (non-nil before nil)
		iPrompt := mi.LastUserPromptAt
		jPrompt := mj.LastUserPromptAt
		if (iPrompt != nil) != (jPrompt != nil) {
			return iPrompt != nil
		}
		if iPrompt != nil && jPrompt != nil && !iPrompt.Equal(*jPrompt) {
			return iPrompt.After(*jPrompt)
		}

		// Tier 3: fallback to coalesce(last_heartbeat, created_at) DESC
		iTime := mi.CreatedAt
		if mi.LastHeartbeat != nil {
			iTime = *mi.LastHeartbeat
		}
		jTime := mj.CreatedAt
		if mj.LastHeartbeat != nil {
			jTime = *mj.LastHeartbeat
		}
		return iTime.After(jTime)
	})
}
