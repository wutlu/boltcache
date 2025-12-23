package cache


// Set operations
func (c *BoltCache) SAdd(key string, members ...string) int {
	c.Mu.Lock()
	defer c.Mu.Unlock()

	set := make(map[string]struct{})
	if val, ok := c.Get(key); ok {
		if s, ok := val.(map[string]struct{}); ok {
			set = s
		}
	}

	added := 0
	for _, m := range members {
		if _, ok := set[m]; !ok {
			set[m] = struct{}{}
			added++
		}
	}

	c.Set(key, set, 0)
	return len(set)
}

func (c *BoltCache) SMembers(key string) []string {
	if val, ok := c.Get(key); ok {
		if set, ok := val.(map[string]struct{}); ok {
			members := make([]string, 0, len(set))
			for m := range set {
				members = append(members, m)
			}
			return members
		}
	}
	return nil
}