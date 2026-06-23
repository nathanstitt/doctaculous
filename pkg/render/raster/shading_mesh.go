package raster

import (
	"fmt"

	"github.com/nathanstitt/doctaculous/pkg/pdf"
	"github.com/nathanstitt/doctaculous/pkg/render"
)

// newMeshShader builds a render.Shader for a mesh shading (ShadingType 4–7).
// Mesh shadings carry their data in a stream of packed vertices/patches rather
// than a continuous parametric function, so they are evaluated by tessellating
// to triangles rather than per-pixel projection — implemented in Phase C. Until
// then this returns an error so the `sh` operator and shading patterns degrade
// gracefully (skip + log).
func newMeshShader(doc *pdf.Document, dict pdf.Dict) (render.Shader, error) {
	st, _ := doc.GetInt(dict["ShadingType"])
	return nil, fmt.Errorf("shading: mesh /ShadingType %d not yet supported", st)
}
