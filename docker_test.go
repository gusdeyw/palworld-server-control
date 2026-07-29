package main

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestDockerUpdatePullsForcesRecreationAndVerifiesImage(t *testing.T) {
	manager := NewDockerManager("palworld", "/opt/palworld", "palworld", false, "", "", false)
	var calls []string
	inspectCount := 0
	manager.runCommand = func(_ context.Context, dir string, args ...string) (string, error) {
		calls = append(calls, dir+"|"+strings.Join(args, " "))
		if reflect.DeepEqual(args, []string{"inspect", "--format", "{{.Image}}", "palworld"}) {
			inspectCount++
			if inspectCount == 1 {
				return "sha256:aaaaaaaaaaaaaaaa\n", nil
			}
			return "sha256:bbbbbbbbbbbbbbbb\n", nil
		}
		return "", nil
	}

	message, err := manager.Update(context.Background())
	if err != nil {
		t.Fatalf("Update returned an error: %v", err)
	}
	if message != "Server image updated (aaaaaaaaaaaa to bbbbbbbbbbbb)" {
		t.Fatalf("unexpected update message %q", message)
	}
	wantCalls := []string{
		"|inspect --format {{.Image}} palworld",
		"|inspect --format {{.Config.Image}} palworld",
		"/opt/palworld|compose pull palworld",
		"/opt/palworld|compose up -d --force-recreate --no-deps palworld",
		"|inspect --format {{.Image}} palworld",
	}
	if !reflect.DeepEqual(calls, wantCalls) {
		t.Fatalf("unexpected Docker calls:\n got: %#v\nwant: %#v", calls, wantCalls)
	}
}

func TestDockerUpdateReportsAlreadyCurrentImage(t *testing.T) {
	manager := NewDockerManager("palworld", "/opt/palworld", "palworld", false, "", "", false)
	manager.runCommand = func(_ context.Context, _ string, args ...string) (string, error) {
		if reflect.DeepEqual(args, []string{"inspect", "--format", "{{.Image}}", "palworld"}) {
			return "sha256:cccccccccccccccc\n", nil
		}
		if reflect.DeepEqual(args, []string{"inspect", "--format", "{{.Config.Image}}", "palworld"}) {
			return "ghcr.io/pocketpairjp/palserver:latest\n", nil
		}
		if reflect.DeepEqual(args, []string{
			"image", "inspect", "--format", "{{.Id}}", "ghcr.io/pocketpairjp/palserver:latest",
		}) {
			return "sha256:cccccccccccccccc\n", nil
		}
		return "", nil
	}

	message, err := manager.Update(context.Background())
	if err != nil {
		t.Fatalf("Update returned an error: %v", err)
	}
	if message != "Server is already using the latest image (cccccccccccc)" {
		t.Fatalf("unexpected update message %q", message)
	}
}

func TestDockerUpdateFailsWhenRecreatedContainerCannotBeVerified(t *testing.T) {
	manager := NewDockerManager("palworld", "/opt/palworld", "palworld", false, "", "", false)
	inspectCount := 0
	manager.runCommand = func(_ context.Context, _ string, args ...string) (string, error) {
		if len(args) > 0 && args[0] == "inspect" {
			inspectCount++
			if inspectCount == 1 {
				return "sha256:aaaaaaaaaaaaaaaa\n", nil
			}
			return "", fmt.Errorf("container missing")
		}
		return "", nil
	}

	_, err := manager.Update(context.Background())
	if err == nil || !strings.Contains(err.Error(), "verify updated container image") {
		t.Fatalf("expected verification error, got %v", err)
	}
}

func TestOnlyNativeUpdateRequiresStoppedServer(t *testing.T) {
	dockerManager := NewDockerManager("palworld", "/opt/palworld", "palworld", false, "", "", false)
	if dockerManager.updateRequiresStop() {
		t.Fatal("Docker Compose update should pull before recreating the running server")
	}

	nativeManager := NewDockerManager("palworld", "", "", false, "http://127.0.0.1:8213", "secret", false)
	if !nativeManager.updateRequiresStop() {
		t.Fatal("native SteamCMD update must stop the server before updating")
	}

	lowSpaceManager := NewDockerManager("palworld", "/opt/palworld", "palworld", true, "", "", false)
	if lowSpaceManager.updateRequiresStop() {
		t.Fatal("low-storage Docker update must keep the server running during its first pull attempt")
	}
}

func TestDockerLowSpaceUpdateRemovesOldImageBeforePulling(t *testing.T) {
	manager := NewDockerManager("palworld", "/opt/palworld", "palworld", true, "", "", false)
	var calls []string
	imageInspectCount := 0
	pullCount := 0
	manager.runCommand = func(_ context.Context, dir string, args ...string) (string, error) {
		calls = append(calls, dir+"|"+strings.Join(args, " "))
		switch {
		case reflect.DeepEqual(args, []string{"inspect", "--format", "{{.Image}}", "palworld"}):
			imageInspectCount++
			if imageInspectCount <= 2 {
				return "sha256:aaaaaaaaaaaaaaaa\n", nil
			}
			return "sha256:bbbbbbbbbbbbbbbb\n", nil
		case reflect.DeepEqual(args, []string{"compose", "pull", "palworld"}):
			pullCount++
			if pullCount == 1 {
				return "", errDockerNoSpace
			}
			return "", nil
		case reflect.DeepEqual(args, []string{"inspect", "--format", "{{.Config.Image}}", "palworld"}):
			return "ghcr.io/pocketpairjp/palserver:latest\n", nil
		case reflect.DeepEqual(args, []string{
			"image", "inspect", "--format", "{{index .RepoDigests 0}}", "sha256:aaaaaaaaaaaaaaaa",
		}):
			return "ghcr.io/pocketpairjp/palserver@sha256:aaaaaaaaaaaaaaaa\n", nil
		default:
			return "", nil
		}
	}

	message, err := manager.Update(context.Background())
	if err != nil {
		t.Fatalf("Update returned an error: %v", err)
	}
	if message != "Server image replaced in low-storage mode (aaaaaaaaaaaa to bbbbbbbbbbbb)" {
		t.Fatalf("unexpected low-storage update message %q", message)
	}

	firstPullIndex := indexCall(calls, "/opt/palworld|compose pull palworld")
	removeIndex := indexCall(calls, "|image rm -f sha256:aaaaaaaaaaaaaaaa")
	secondPullIndex := indexCallAfter(calls, "/opt/palworld|compose pull palworld", firstPullIndex+1)
	if firstPullIndex < 0 || removeIndex <= firstPullIndex || secondPullIndex <= removeIndex {
		t.Fatalf("fallback must remove the old image between the normal and low-storage pulls:\n%v", calls)
	}
}

func TestSummarizeDockerErrorMakesDiskFailureActionable(t *testing.T) {
	got := summarizeDockerError("many progress lines\nwrite file: no space left on device")
	want := "not enough disk space while downloading or extracting the server image"
	if got != want {
		t.Fatalf("unexpected disk error summary %q", got)
	}
}

func indexCall(calls []string, wanted string) int {
	return indexCallAfter(calls, wanted, 0)
}

func indexCallAfter(calls []string, wanted string, start int) int {
	for index := start; index < len(calls); index++ {
		call := calls[index]
		if call == wanted {
			return index
		}
	}
	return -1
}
