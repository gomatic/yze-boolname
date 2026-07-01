package a

// crossFile lives in the package's second file so reference collection must
// resolve the declaring file rather than assume the first.
func crossFile(
	fast bool, // want `boolean fast should use an is/has/can/should/will prefix or an Enabled/Disabled suffix`
) bool {
	return fast
}
