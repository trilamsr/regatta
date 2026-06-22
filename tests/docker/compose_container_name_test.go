//go:build docker

package docker_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCompose_NoHardcodedContainerNames asserts docker-compose.yml drops container_name so two `docker compose -p <X>` stacks can coexist without conflicts.
func TestCompose_NoHardcodedContainerNames(t *testing.T) {
	repoRoot := repoRootFromCWD(t)
	composeFile := filepath.Join(repoRoot, "docker-compose.yml")
	body, err := os.ReadFile(composeFile)
	if err != nil {
		t.Fatalf("read compose: %v", err)
	}
	src := string(body)
	if strings.Contains(src, "\n    container_name:") || strings.Contains(src, "\ncontainer_name:") {
		t.Fatalf("docker-compose.yml has hardcoded container_name — blocks side-by-side stacks. Drop the lines; let compose derive `<project>-<service>-1`.")
	}
}
