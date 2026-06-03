package architect

// DepDoctorLensName is the selector for the dependency-doctor lens.
const DepDoctorLensName = "depdoctor"

// init registers the dependency-doctor lens: the keystone graph provider. It
// bundles the DepsCollector (package import graph, cycle detection, layer
// rules, unused/heavy dependency analysis) over the shared SCAN spine, so
// `pilot architect --lens depdoctor` aims the whole pipeline at dependency
// health. It ignores ScanOptions: its analysis is driven entirely by the live
// `go` toolchain output, not the core thresholds.
func init() {
	RegisterLens(Lens{
		Name:        DepDoctorLensName,
		Description: "dependency doctor: import-graph cycles, layer violations, unused/heavy deps",
		Collectors: func(_ string, _ ScanOptions) []Collector {
			return []Collector{NewDepsCollector()}
		},
	})
}
