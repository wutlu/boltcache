package swaggerui

import (
	"regexp"
	"strings"
)

import (
	appinfo "boltcache/appinfo"
)

type APIRoute struct {
	Method      string
	Path        string
	Summary     string
	Description string
	Tag         string
	RequestBody map[string]interface{}
	Responses   map[string]interface{}
}

var pathParamRegex = regexp.MustCompile(`\{([^}]+)\}`)

func extractPathParams(path string) []map[string]interface{} {
	matches := pathParamRegex.FindAllStringSubmatch(path, -1)

	var params []map[string]interface{}
	for _, m := range matches {
		params = append(params, map[string]interface{}{
			"name":     m[1],
			"in":       "path",
			"required": true,
			"schema": map[string]interface{}{
				"type": "string",
			},
		})
	}
	return params
}

// OPENAPI GENERATOR
func generateOpenAPISpec() map[string]interface{} {
	paths := map[string]interface{}{}
	tagSet := map[string]bool{}

	for _, r := range apiRoutes {
		if paths[r.Path] == nil {
			paths[r.Path] = map[string]interface{}{}
		}

		op := map[string]interface{}{
			"summary":   r.Summary,
			"tags":      []string{r.Tag},
			"responses": r.Responses,
		}

		params := extractPathParams(r.Path)
		if len(params) > 0 {
			op["parameters"] = params
		}

		if r.Description != "" {
			op["description"] = r.Description
		}

		if r.RequestBody != nil {
			op["requestBody"] = map[string]interface{}{
				"required": true,
				"content": map[string]interface{}{
					"application/json": map[string]interface{}{
						"schema": r.RequestBody,
					},
				},
			}
		}

		paths[r.Path].(map[string]interface{})[strings.ToLower(r.Method)] = op
		tagSet[r.Tag] = true
	}

	var tags []map[string]string
	for t := range tagSet {
		tags = append(tags, map[string]string{"name": t})
	}

	return map[string]interface{}{
		"openapi": "3.0.0",
		"info": map[string]interface{}{
			"title":   appinfo.Title,
			"version": appinfo.Version,
		},
		"paths": paths,
		"tags":  tags,
	}
}
