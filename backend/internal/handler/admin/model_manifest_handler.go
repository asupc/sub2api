package admin

import (
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// ModelManifestHandler 模型清单与账号供给透视
type ModelManifestHandler struct {
	manifestService *service.ModelManifestService
}

// NewModelManifestHandler creates a new model manifest handler
func NewModelManifestHandler(manifestService *service.ModelManifestService) *ModelManifestHandler {
	return &ModelManifestHandler{manifestService: manifestService}
}

// GetManifest handles GET /api/v1/admin/models/manifest
func (h *ModelManifestHandler) GetManifest(c *gin.Context) {
	query := service.ModelManifestQuery{
		Platform: c.Query("platform"),
		Status:   c.Query("status"),
		Search:   c.Query("search"),
	}
	manifest, err := h.manifestService.GetManifest(c.Request.Context(), query)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "Failed to build model manifest")
		return
	}
	response.Success(c, manifest)
}
