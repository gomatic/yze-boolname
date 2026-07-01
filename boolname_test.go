package boolname_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"golang.org/x/tools/go/analysis/analysistest"

	boolname "github.com/gomatic/yze-go-boolname"
)

func TestBooleanNamingIsReportedAndFixed(t *testing.T) {
	analysistest.RunWithSuggestedFixes(t, analysistest.TestData(), boolname.Analyzer, "a")
}

func TestRegistrationIsWellFormed(t *testing.T) {
	assert.NoError(t, boolname.Registration.Validate())
	assert.Equal(t, "yze/boolname", boolname.Registration.RuleID())
	assert.Same(t, boolname.Analyzer, boolname.Registration.Analyzer)
}
