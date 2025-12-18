package swaggerui

var apiRoutes = []APIRoute{
	// Cache
	{
		Method:  "PUT",
		Path:    "/cache/{key}",
		Summary: "Set cache value",
		Tag:     "Cache",
		RequestBody: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"value": map[string]interface{}{"type": "string"},
				"ttl":   map[string]interface{}{"type": "string"},
			},
		},
		Responses: map[string]interface{}{
			"200": map[string]interface{}{
				"description": "OK",
			},
		},
	},
	{
		Method:  "GET",
		Path:    "/cache/{key}",
		Summary: "Get cache value",
		Tag:     "Cache",
		Responses: map[string]interface{}{
			"200": map[string]interface{}{
				"description": "OK",
			},
			"404": map[string]interface{}{
				"description": "Not found",
			},
		},
	},
	{
		Method:  "DELETE",
		Path:    "/cache/{key}",
		Summary: "Delete cache key",
		Tag:     "Cache",
		Responses: map[string]interface{}{
			"200": map[string]interface{}{"description": "Deleted"},
		},
	},

	// List
	{
		Method:  "POST",
		Path:    "/list/{key}",
		Summary: "Push to list",
		Tag:     "List",
		RequestBody: map[string]interface{}{
			"type":  "array",
			"items": map[string]interface{}{"type": "string"},
		},
		Responses: map[string]interface{}{
			"200": map[string]interface{}{
				"description": "OK",
			},
		},
	},
	{
		Method:  "DELETE",
		Path:    "/list/{key}",
		Summary: "Pop from list",
		Tag:     "List",
		Responses: map[string]interface{}{
			"200": map[string]interface{}{
				"description": "OK",
			},
			"404": map[string]interface{}{
				"description": "Not found",
			},
		},
	},

	// Set
	{
		Method:  "POST",
		Path:    "/set/{key}",
		Summary: "Add set members",
		Tag:     "Set",
		RequestBody: map[string]interface{}{
			"type":  "array",
			"items": map[string]interface{}{"type": "string"},
		},
		Responses: map[string]interface{}{
			"200": map[string]interface{}{
				"description": "OK",
			},
		},
	},
	{
		Method:  "GET",
		Path:    "/set/{key}",
		Summary: "Get set members",
		Tag:     "Set",
		Responses: map[string]interface{}{
			"200": map[string]interface{}{
				"description": "OK",
			},
		},
	},

	// Hash
	{
		Method:  "PUT",
		Path:    "/hash/{key}/{field}",
		Summary: "Set hash field",
		Tag:     "Hash",
		RequestBody: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"value": map[string]interface{}{"type": "string"},
			},
		},
		Responses: map[string]interface{}{
			"200": map[string]interface{}{
				"description": "OK",
			},
		},
	},
	{
		Method:  "GET",
		Path:    "/hash/{key}/{field}",
		Summary: "Get hash field",
		Tag:     "Hash",
		Responses: map[string]interface{}{
			"200": map[string]interface{}{
				"description": "OK",
			},
			"404": map[string]interface{}{
				"description": "Not found",
			},
		},
	},

	// Pub/Sub
	{
		Method:  "GET",
		Path:    "/subscribe/{channel}",
		Summary: "Subscribe (WebSocket)",
		Tag:     "PubSub",
		Responses: map[string]interface{}{
			"101": map[string]interface{}{
				"description": "Switching Protocols",
			},
		},
	},
	{
		Method:  "POST",
		Path:    "/publish/{channel}",
		Summary: "Publish message",
		Tag:     "PubSub",
		RequestBody: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"message": map[string]interface{}{"type": "string"},
			},
		},
		Responses: map[string]interface{}{
			"200": map[string]interface{}{
				"description": "OK",
			},
		},
	},

	// Script
	{
		Method:  "POST",
		Path:    "/eval",
		Summary: "Execute Lua script",
		Tag:     "Scripting",
		RequestBody: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"script": map[string]interface{}{"type": "string"},
				"keys":   map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
				"args":   map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
			},
		},
		Responses: map[string]interface{}{
			"200": map[string]interface{}{
				"description": "OK",
			},
		},
	},

	// Auth
	{
		Method:  "GET",
		Path:    "/auth/tokens",
		Summary: "List tokens",
		Tag:     "Auth",
		Responses: map[string]interface{}{
			"200": map[string]interface{}{
				"description": "OK",
			},
		},
	},
	{
		Method:  "POST",
		Path:    "/auth/tokens",
		Summary: "Create token",
		Tag:     "Auth",
		Responses: map[string]interface{}{
			"200": map[string]interface{}{
				"description": "OK",
			},
		},
	},
	{
		Method:  "DELETE",
		Path:    "/auth/tokens/{token}",
		Summary: "Delete token",
		Tag:     "Auth",
		Responses: map[string]interface{}{
			"200": map[string]interface{}{"description": "Deleted"},
		},
	},

	// Info
	{
		Method:  "GET",
		Path:    "/info",
		Summary: "Server info",
		Tag:     "System",
		Responses: map[string]interface{}{
			"200": map[string]interface{}{
				"description": "OK",
			},
		},
	},
	{
		Method:  "GET",
		Path:    "/ping",
		Summary: "Health check",
		Tag:     "System",
		Responses: map[string]interface{}{
			"200": map[string]interface{}{"description": "Pong"},
		},
	},
}