package usage

import "strings"

// sameLogin reports whether two results are the same Claude login: equal
// account UUIDs, or (when either lacks a UUID) equal emails, case-insensitive.
// Logged-out rows have no login and never match.
func sameLogin(a, b AccountUsage) bool {
	if a.State == StateLoggedOut || b.State == StateLoggedOut {
		return false
	}
	if a.AccountUUID != "" && b.AccountUUID != "" {
		return a.AccountUUID == b.AccountUUID
	}
	return a.Email != "" && strings.EqualFold(a.Email, b.Email)
}

// CanonicalIndex maps every row to the index of its canonical row. Rows that
// are the same login form a group; its canonical row is the first usable
// (ok/expired-refreshed) member, else the first member, so a failing earlier
// account never hides a healthy duplicate. A row that is its own canonical
// maps to itself. It is recomputed on every call, so duplicates split as soon
// as the underlying logins differ.
func CanonicalIndex(list []AccountUsage) []int {
	root := make([]int, len(list))
	for i := range list {
		root[i] = i
		for j := 0; j < i; j++ {
			if root[j] == j && sameLogin(list[j], list[i]) {
				root[i] = j
				break
			}
		}
	}
	best := map[int]int{} // group root -> canonical index
	for i, r := range root {
		cur, seen := best[r]
		if !seen || (!countable(list[cur].State) && countable(list[i].State)) {
			best[r] = i
		}
	}
	out := make([]int, len(list))
	for i, r := range root {
		out[i] = best[r]
	}
	return out
}

// Distinct returns list without rows that duplicate an earlier login. The
// input is not modified.
func Distinct(list []AccountUsage) []AccountUsage {
	canon := CanonicalIndex(list)
	out := make([]AccountUsage, 0, len(list))
	for i, a := range list {
		if canon[i] == i {
			out = append(out, a)
		}
	}
	return out
}
