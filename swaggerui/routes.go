package swaggerui

type obj = map[string]any

func ok(desc string) obj { return obj{"200": obj{"description": desc}} }

func okOr(desc string, codes ...string) obj {
	r := ok(desc)
	text := map[string]string{
		"400": "Invalid request",
		"404": "Not found",
		"409": "Key holds a different type (WRONGTYPE)",
		"507": "Memory limit reached (noeviction policy)",
	}
	for _, c := range codes {
		r[c] = obj{"description": text[c]}
	}
	return r
}

var (
	valueBody = obj{
		"type":     "object",
		"required": []string{"value"},
		"properties": obj{
			"value": obj{"description": "Any JSON value; non-strings are stored as their JSON text", "example": "John Doe"},
			"ttl":   obj{"type": "string", "description": "Go duration, e.g. 30s, 5m, 2h", "example": "5m"},
		},
	}
	stringArray = obj{"type": "array", "items": obj{"type": "string"}, "example": []string{"a", "b"}}
)

var apiRoutes = []APIRoute{
	{Method: "PUT", Path: "/cache/{key}", Summary: "Set a string value (optional TTL)", Tag: "Cache", RequestBody: valueBody, Responses: okOr("Stored", "400", "507")},
	{Method: "GET", Path: "/cache/{key}", Summary: "Get a value of any type", Description: "Strings are returned as strings, lists and sets as arrays and hashes as objects.", Tag: "Cache", Responses: okOr("Value", "404")},
	{Method: "DELETE", Path: "/cache/{key}", Summary: "Delete a key", Tag: "Cache", Responses: ok("Deleted (count = number of keys removed)")},
	{Method: "GET", Path: "/cache/{key}/ttl", Summary: "Remaining time to live", Tag: "Cache", Responses: okOr("TTL in milliseconds; persistent=true when the key never expires", "404")},
	{Method: "GET", Path: "/keys", Summary: "List keys matching a glob pattern (paginated)", Description: "Query parameters: pattern (default *), limit (default 1000), cursor (from the previous page; \"0\" when done).", Tag: "Cache", Responses: ok("Keys and next cursor")},

	{Method: "POST", Path: "/list/{key}", Summary: "Push values (LPUSH; ?side=right for RPUSH)", Tag: "List", RequestBody: stringArray, Responses: okOr("New length", "400", "409")},
	{Method: "DELETE", Path: "/list/{key}", Summary: "Pop a value (LPOP; ?side=right for RPOP)", Tag: "List", Responses: okOr("Popped value", "404", "409")},
	{Method: "GET", Path: "/list/{key}", Summary: "Range of values (?start=0&stop=-1)", Tag: "List", Responses: okOr("Values", "409")},

	{Method: "POST", Path: "/set/{key}", Summary: "Add members", Tag: "Set", RequestBody: stringArray, Responses: okOr("Number of new members", "400", "409")},
	{Method: "GET", Path: "/set/{key}", Summary: "Get members", Tag: "Set", Responses: okOr("Members", "409")},
	{Method: "DELETE", Path: "/set/{key}/{member}", Summary: "Remove a member", Tag: "Set", Responses: okOr("Number removed", "409")},

	{Method: "PUT", Path: "/hash/{key}/{field}", Summary: "Set a field", Tag: "Hash", RequestBody: valueBody, Responses: okOr("Stored", "400", "409")},
	{Method: "GET", Path: "/hash/{key}/{field}", Summary: "Get a field", Tag: "Hash", Responses: okOr("Value", "404", "409")},
	{Method: "DELETE", Path: "/hash/{key}/{field}", Summary: "Delete a field", Tag: "Hash", Responses: okOr("Number removed", "409")},
	{Method: "GET", Path: "/hash/{key}", Summary: "Get all fields as an object", Tag: "Hash", Responses: okOr("Fields", "409")},

	{Method: "POST", Path: "/publish/{channel}", Summary: "Publish a message", Tag: "Pub/Sub",
		RequestBody: obj{"type": "object", "properties": obj{"message": obj{"type": "string", "example": "Hello World"}}},
		Responses:   ok("Number of subscribers that received the message")},
	{Method: "GET", Path: "/subscribe/{channel}", Summary: "Subscribe over WebSocket", Description: "Upgrade to a WebSocket; each published message arrives as a text frame. Browsers pass the token as ?token=.", Tag: "Pub/Sub", Responses: obj{"101": obj{"description": "Switching protocols"}}},

	{Method: "POST", Path: "/eval", Summary: "Run a Lua script", Tag: "Scripting",
		RequestBody: obj{"type": "object", "properties": obj{
			"script": obj{"type": "string", "example": "return redis.call('INCR', KEYS[1])"},
			"keys":   obj{"type": "array", "items": obj{"type": "string"}, "example": []string{"counter"}},
			"args":   obj{"type": "array", "items": obj{"type": "string"}},
		}},
		Responses: okOr("Script result", "400")},
	{Method: "POST", Path: "/command", Summary: "Run any Redis command", Description: "Body: {\"args\": [\"HSET\", \"user:1\", \"name\", \"Ada\"]} or a bare JSON array. Replies are converted to JSON.", Tag: "Scripting",
		RequestBody: obj{"type": "object", "properties": obj{"args": obj{"type": "array", "items": obj{"type": "string"}, "example": []string{"INCRBY", "visits", "5"}}}},
		Responses:   okOr("Command reply", "400")},

	{Method: "GET", Path: "/auth/tokens", Summary: "List tokens (masked)", Tag: "Auth", Responses: okOr("Tokens", "404")},
	{Method: "POST", Path: "/auth/tokens", Summary: "Create a runtime token", Description: "Runtime tokens live in memory until restart and work for every protocol.", Tag: "Auth", Responses: okOr("New token", "404")},
	{Method: "DELETE", Path: "/auth/tokens/{token}", Summary: "Revoke a runtime token (value or masked id)", Tag: "Auth", Responses: okOr("Revoked", "404")},

	{Method: "GET", Path: "/info", Summary: "Server statistics", Tag: "Server", Responses: ok("Statistics")},
	{Method: "GET", Path: "/ping", Summary: "Liveness check (no auth)", Tag: "Server", Responses: ok("PONG")},
	{Method: "GET", Path: "/health", Summary: "Health check (no auth)", Tag: "Server", Responses: ok("Healthy")},
	{Method: "GET", Path: "/metrics", Summary: "Prometheus metrics", Tag: "Server", Responses: ok("Prometheus text format")},
}
