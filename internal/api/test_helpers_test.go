package api

import "testing"

func disableAuthForTest(t *testing.T) {
	t.Helper()
	t.Setenv("SPLAI_API_AUTH_MODE", "off")
}
