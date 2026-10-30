package sizes

import (
	"strings"

	"k8s.io/apimachinery/pkg/api/resource"
)

// RuntimeFromBuildpacks identifies the runtime that remains in the image.
// A static server takes precedence over Node used only to build its assets;
// PHP over the web server it runs behind (the PHP buildpacks bring nginx or
// httpd along).
func RuntimeFromBuildpacks(ids []string) string {
	for _, id := range ids {
		switch strings.ToLower(id) {
		case "paketo-buildpacks/php", "paketo-buildpacks/php-dist", "paketo-buildpacks/php-start":
			return "PHP"
		}
	}
	for _, id := range ids {
		switch strings.ToLower(id) {
		case "paketo-buildpacks/nginx", "paketo-buildpacks/httpd", "paketo-buildpacks/web-servers":
			return "static"
		}
	}
	for _, id := range ids {
		switch strings.ToLower(id) {
		case "paketo-buildpacks/nodejs", "paketo-buildpacks/node-engine":
			return "Node.js"
		case "paketo-buildpacks/dotnet-core", "paketo-buildpacks/dotnet-core-runtime", "paketo-buildpacks/dotnet-core-aspnet-runtime", "paketo-buildpacks/dotnet-core-sdk", "paketo-buildpacks/dotnet-execute", "paketo-buildpacks/dotnet-publish":
			return ".NET"
		case "paketo-buildpacks/ruby", "paketo-buildpacks/mri":
			return "Ruby"
		case "paketo-buildpacks/python", "paketo-buildpacks/cpython":
			return "Python"
		case "paketo-buildpacks/java", "paketo-buildpacks/bellsoft-liberica", "paketo-buildpacks/amazon-corretto", "paketo-buildpacks/azul-zulu", "paketo-buildpacks/eclipse-openj9":
			return "JVM"
		case "paketo-buildpacks/go", "paketo-buildpacks/go-build":
			return "Go"
		case "paketo-buildpacks/rust", "paketo-community/rust":
			return "Rust"
		}
	}
	return ""
}

// RuntimeNeedsMemory says the runtime does not fit the 64Mi default: the
// examples (shpyrd-examples) crash or sleep forever at it, and ask for
// shared-m. PHP is nginx and PHP-FPM's workers; .NET its runtime.
func RuntimeNeedsMemory(runtime string) bool {
	switch runtime {
	case "Node.js", "Ruby", "Python", "JVM", "PHP", ".NET":
		return true
	}
	return false
}

// DefaultForRuntime honors an operator's larger default and custom catalogs.
// Never silently shrink the memory allocation or name a size that does not exist.
func (c Catalog) DefaultForRuntime(runtime string) string {
	if !RuntimeNeedsMemory(runtime) {
		return c.Default
	}
	recommended, ok := c.Get("shared-m")
	if !ok {
		return c.Default
	}
	current, ok := c.Get(c.Default)
	if ok {
		currentMemory := resource.MustParse(current.Memory)
		if currentMemory.Cmp(resource.MustParse(recommended.Memory)) >= 0 {
			return c.Default
		}
	}
	return recommended.Name
}
