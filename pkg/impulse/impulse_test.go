package impulse

import (
	"context"
	"log/slog"
	"os"
	"sync/atomic"
	"testing"

	runsv1alpha1 "github.com/bubustack/bobrapet/api/runs/v1alpha1"
	sdk "github.com/bubustack/bubu-sdk-go"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	cfgpkg "github.com/bubustack/cron-impulse/pkg/config"
)

func TestCanStartRunForbidUsesActiveSession(t *testing.T) {
	t.Parallel()

	dispatcher := sdk.NewStoryDispatcher(sdk.WithStoryRuntime(
		func(context.Context, string, string, map[string]any) (*runsv1alpha1.StoryRun, error) {
			return &runsv1alpha1.StoryRun{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "storyrun-1",
					Namespace: "default",
				},
			}, nil
		},
		func(context.Context, string, string) error { return nil },
	))

	imp := &CronImpulse{
		cfg: &cfgpkg.Config{
			ConcurrencyPolicy: "forbid",
		},
		dispatcher: dispatcher,
		logger:     slog.New(slog.NewTextHandler(os.Stdout, nil)),
	}

	sessionKey := imp.sessionKeyForSchedule("nightly", "run-1")
	_, err := imp.dispatcher.Trigger(context.Background(), sdk.StoryTriggerRequest{
		Key:            sessionKey,
		StoryName:      "story",
		StoryNamespace: "default",
		Inputs:         map[string]any{},
	})
	if err != nil {
		t.Fatalf("seed session: %v", err)
	}

	if imp.canStartRun(context.Background(), sessionKey, imp.logger) {
		t.Fatalf("expected forbid policy to reject while session is active")
	}
}

func TestCanStartRunReplaceStopsExistingSession(t *testing.T) {
	t.Parallel()

	var stopCalls atomic.Int32
	dispatcher := sdk.NewStoryDispatcher(sdk.WithStoryRuntime(
		func(context.Context, string, string, map[string]any) (*runsv1alpha1.StoryRun, error) {
			return &runsv1alpha1.StoryRun{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "storyrun-1",
					Namespace: "default",
				},
			}, nil
		},
		func(context.Context, string, string) error {
			stopCalls.Add(1)
			return nil
		},
	))

	imp := &CronImpulse{
		cfg: &cfgpkg.Config{
			ConcurrencyPolicy: "replace",
		},
		dispatcher: dispatcher,
		logger:     slog.New(slog.NewTextHandler(os.Stdout, nil)),
	}

	sessionKey := imp.sessionKeyForSchedule("nightly", "run-1")
	_, err := imp.dispatcher.Trigger(context.Background(), sdk.StoryTriggerRequest{
		Key:            sessionKey,
		StoryName:      "story",
		StoryNamespace: "default",
		Inputs:         map[string]any{},
	})
	if err != nil {
		t.Fatalf("seed session: %v", err)
	}

	if !imp.canStartRun(context.Background(), sessionKey, imp.logger) {
		t.Fatalf("expected replace policy to allow new run after stopping previous session")
	}
	if got := stopCalls.Load(); got != 1 {
		t.Fatalf("stop calls = %d, want 1", got)
	}
}
