package cmd

import (
	"strings"
	"testing"
)

// `auth login` stores an API key billed pay-as-you-go (see cmd/auth.go); the
// root help must not send users there expecting plan credits.
func TestRootHelpDescribesAPIKeyLogin(t *testing.T) {
	if strings.Contains(rootCmd.Long, "plan credits") {
		t.Fatalf("root help still says auth login uses plan credits:\n%s", rootCmd.Long)
	}
	if !strings.Contains(rootCmd.Long, `"zebracat auth login" (saves an API key`) {
		t.Fatalf("root help does not describe auth login as API-key login:\n%s", rootCmd.Long)
	}
}
