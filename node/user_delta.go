package node

import (
	"sort"

	panel "github.com/keli-123456/kelinode/api/v2board"
)

func applyUserDelta(old []panel.UserInfo, deleted []panel.UserInfo, upsert []panel.UserInfo) (next []panel.UserInfo, deletedApplied, added, updated []panel.UserInfo) {
	oldMap := make(map[string]panel.UserInfo, len(old))
	for _, u := range old {
		oldMap[u.Uuid] = u
	}

	for _, u := range deleted {
		if oldUser, ok := oldMap[u.Uuid]; ok {
			deletedApplied = append(deletedApplied, oldUser)
			delete(oldMap, u.Uuid)
		}
	}

	for _, u := range upsert {
		if _, ok := oldMap[u.Uuid]; ok {
			updated = append(updated, u)
		} else {
			added = append(added, u)
		}
		oldMap[u.Uuid] = u
	}

	next = make([]panel.UserInfo, 0, len(oldMap))
	for _, u := range oldMap {
		next = append(next, u)
	}
	sort.Slice(next, func(i, j int) bool { return next[i].Uuid < next[j].Uuid })
	return next, deletedApplied, added, updated
}
