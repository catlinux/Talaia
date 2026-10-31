package api

import (
	"net/http"

	"github.com/catlinux/Talaia/core/internal/templates"
)

type templateResponse struct {
	ID           string            `json:"id"`
	Version      string            `json:"version"`
	Names        map[string]string `json:"names"`
	Descriptions map[string]string `json:"descriptions"`
	Image        string            `json:"image"`
	Service      string            `json:"service"`
	Port         int               `json:"port"`
	RAMMB        int               `json:"ram_mb"`
}

// NewTemplatesHandler returns GET /api/v1/templates.
func NewTemplatesHandler(deps AppDeps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := []templateResponse{}
		if deps.Templates != nil {
			for _, template := range deps.Templates.List() {
				response = append(response, templateResponse{
					ID: template.ID, Version: template.Version,
					Names: names(template), Descriptions: descriptions(template),
					Image: template.Image, Service: template.Service,
					Port: template.Port, RAMMB: template.RAMMB,
				})
			}
		}
		writeJSON(w, http.StatusOK, response)
	})
}

func names(template templates.Template) map[string]string {
	return map[string]string{"ca": template.Name("ca"), "es": template.Name("es"), "en": template.Name("en")}
}

func descriptions(template templates.Template) map[string]string {
	return map[string]string{
		"ca": template.Description("ca"), "es": template.Description("es"), "en": template.Description("en"),
	}
}
