// Package docs registers the maintained Swagger specification.
// Edit swagger.json and swagger.yaml together; do not regenerate empty files
// with swag init until handler annotations cover every supported operation.
package docs

import (
	_ "embed"

	"github.com/swaggo/swag"
)

//go:embed swagger.json
var specification string

func init() {
	swag.Register(swag.Name, &swag.Spec{InfoInstanceName: swag.Name, SwaggerTemplate: specification})
}
