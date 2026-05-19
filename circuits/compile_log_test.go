package circuits

import (
	"testing"

	qt "github.com/frankban/quicktest"
)

type compileLogStatsStub struct {
	nbConstraints     int
	nbPublicVariables int
	nbSecretVariables int
}

func (s compileLogStatsStub) GetNbConstraints() int {
	return s.nbConstraints
}

func (s compileLogStatsStub) GetNbPublicVariables() int {
	return s.nbPublicVariables
}

func (s compileLogStatsStub) GetNbSecretVariables() int {
	return s.nbSecretVariables
}

func TestCompileLogFields(t *testing.T) {
	c := qt.New(t)

	fields := CompileLogFields(compileLogStatsStub{
		nbConstraints:     123,
		nbPublicVariables: 4,
		nbSecretVariables: 56,
	})

	c.Assert(fields, qt.DeepEquals, []any{
		"nbConstraints", 123,
		"nbPublic", 4,
		"nbSecret", 56,
	})
}
