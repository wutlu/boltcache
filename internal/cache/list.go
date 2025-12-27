package cache


func (c *BoltCache) LPush(key string, values ...string) int {
	c.Mu.Lock()
	defer c.Mu.Unlock()

	var list []string
	if val, ok := c.Get(key); ok {
		if l, ok := val.([]string); ok {
			list = l
		}
	}

	list = append(values, list...)
	c.Set(key, list, 0)
	return len(list)
}

func (c *BoltCache) LPop(key string) (string, bool) {
	c.Mu.Lock()
	defer c.Mu.Unlock()

	if val, ok := c.Get(key); ok {
		if list, ok := val.([]string); ok && len(list) > 0 {
			item := list[0]
			list = list[1:]
			if len(list) == 0 {
				c.Delete(key)
			} else {
				c.Set(key, list, 0)
			}
			return item, true
		}
	}
	return "", false
}