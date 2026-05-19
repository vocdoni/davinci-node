package circuits

// compileLogStats describes the counters exposed by gnark constraint systems.
type compileLogStats interface {
	GetNbConstraints() int
	GetNbPublicVariables() int
	GetNbSecretVariables() int
}

// CompileLogFields returns the structured fields appended to circuit compile logs.
func CompileLogFields(ccs compileLogStats) []any {
	return []any{
		"nbConstraints", ccs.GetNbConstraints(),
		"nbPublic", ccs.GetNbPublicVariables(),
		"nbSecret", ccs.GetNbSecretVariables(),
	}
}
