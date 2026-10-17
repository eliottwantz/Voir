package git

import "strings"

var (
	generatedSegments = map[string]bool{
		"__generated__": true, "__snapshots__": true, ".generated": true, "codegen": true, "gen": true,
		"generated": true, "generated-sources": true, "generated-src": true,
	}
	generatedNames = map[string]bool{
		"bun.lock": true, "bun.lockb": true, "cargo.lock": true, "gemfile.lock": true, "npm-shrinkwrap.json": true,
		"package-lock.json": true, "pnpm-lock.yaml": true, "poetry.lock": true, "pubspec.lock": true, "uv.lock": true,
		"yarn.lock": true, "go.sum": true,
	}
	generatedSuffixes = []string{
		".d.ts.map", ".g.dart", ".generated.cjs", ".generated.css", ".generated.js", ".generated.jsx", ".generated.mjs",
		".generated.ts", ".generated.tsx", ".pb.go", ".pb.gw.go", ".snap", ".snapshot",
		"-generated.js", "-generated.ts", "-generated.tsx", ".min.cjs", ".min.css", ".min.js", ".min.mjs", ".pb.cc",
		".pb.h", ".pb.rb", "_generated.go", "_generated.rs", "_pb2.py", "_pb2_grpc.py",
	}
)

// LooksGenerated reports whether a path is of a file tools usually write,
// by its name: lock files, minified and generated sources, snapshots.
func LooksGenerated(path string) bool {
	p := strings.ToLower(strings.ReplaceAll(path, `\`, "/"))
	parts := strings.Split(p, "/")
	for _, part := range parts {
		if generatedSegments[part] {
			return true
		}
	}
	base := parts[len(parts)-1]
	if generatedNames[base] {
		return true
	}
	for _, s := range generatedSuffixes {
		if strings.HasSuffix(base, s) {
			return true
		}
	}
	return strings.HasSuffix(base, ".map") && !strings.HasSuffix(base, ".importmap")
}
