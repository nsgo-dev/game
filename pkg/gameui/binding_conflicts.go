package gameui

type ConflictPolicy int

const (
	ConflictSwap ConflictPolicy = iota
	ConflictClear
	ConflictRefuse
)

func ResolveBindingConflict(bindings map[string][]string, groups map[string]string, action, token string, policy ConflictPolicy) (string, bool) {
	group := groups[action]
	for other, tokens := range bindings {
		if other == action || groups[other] != group {
			continue
		}
		for _, t := range tokens {
			if t != token {
				continue
			}
			if policy == ConflictRefuse {
				return other, false
			}
			bindings[other] = removeToken(tokens, token)
			if policy == ConflictSwap {
				bindings[action] = append(bindings[action], token)
			}
			return other, true
		}
	}
	bindings[action] = append(bindings[action], token)
	return "", true
}
func removeToken(in []string, t string) []string {
	out := in[:0]
	for _, v := range in {
		if v != t {
			out = append(out, v)
		}
	}
	return out
}
