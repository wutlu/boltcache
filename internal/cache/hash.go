package cache

// Hash operations
func (c *BoltCache) HSet(key, field, value string) {
	c.Mu.Lock()
	defer c.Mu.Unlock()

	hash := make(map[string]string)
	if val, ok := c.Get(key); ok {
		if h, ok := val.(map[string]string); ok {
			hash = h
		}
	}

	hash[field] = value
	c.Set(key, hash, 0)
}

func (c *BoltCache) HGet(key, field string) (string, bool) {
	if val, ok := c.Get(key); ok {
		if hash, ok := val.(map[string]string); ok {
			v, ok := hash[field]
			return v, ok
		}
	}
	return "", false
}