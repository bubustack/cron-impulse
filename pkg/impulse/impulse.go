// Package impulse implements the core logic for the cron impulse.
// This component submits StoryTrigger requests based on cron schedules, enabling
// time-based automation workflows without external dependencies.
//
// The impulse is designed to be stateless and can be scaled horizontally,
// though typically only one replica is needed for cron-based triggers.
package impulse

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"math/big"
	"net/http"
	"strings"
	"time"

	runsv1alpha1 "github.com/bubustack/bobrapet/api/runs/v1alpha1"
	sdk "github.com/bubustack/bubu-sdk-go"
	sdkengram "github.com/bubustack/bubu-sdk-go/engram"
	sdkk8s "github.com/bubustack/bubu-sdk-go/k8s"
	"github.com/robfig/cron/v3"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"

	cfgpkg "github.com/bubustack/cron-impulse/pkg/config"
)

const (
	defaultHealthPort      = 8080
	concurrencyPolicyAllow = "allow"
)

// CronImpulse implements the sdk.Impulse interface for cron-based triggers.
type CronImpulse struct {
	cfg        *cfgpkg.Config
	secrets    *sdkengram.Secrets
	cron       *cron.Cron
	dispatcher *sdk.StoryDispatcher
	logger     *slog.Logger
	location   *time.Location
	k8sClient  *sdkk8s.Client
}

// New creates a new CronImpulse instance.
func New() *CronImpulse {
	return &CronImpulse{
		dispatcher: sdk.NewStoryDispatcher(),
	}
}

func (i *CronImpulse) loggerWithContext(ctx context.Context) *slog.Logger {
	if ctx != nil {
		if l := sdk.LoggerFromContext(ctx); l != nil {
			return l.With("component", "cron-impulse")
		}
	}
	if i.logger != nil {
		return i.logger
	}
	return slog.Default()
}

// Init initializes the cron impulse with configuration and secrets.
func (i *CronImpulse) Init(ctx context.Context, cfg cfgpkg.Config, secrets *sdkengram.Secrets) error {
	logger := i.loggerWithContext(ctx)

	// Verify target story is configured via Impulse.spec.storyRef (injected by operator)
	target, err := sdk.GetTargetStory()
	if err != nil {
		return fmt.Errorf("Impulse.spec.storyRef must be configured: %w", err)
	}
	logger.Info("Target story resolved from Impulse.spec.storyRef",
		slog.String("name", target.Name),
		slog.String("namespace", target.Namespace),
	)

	// Apply defaults
	if cfg.Timezone == "" {
		cfg.Timezone = "UTC"
	}
	if cfg.ConcurrencyPolicy == "" {
		cfg.ConcurrencyPolicy = concurrencyPolicyAllow
	}

	// Parse timezone
	loc, err := time.LoadLocation(cfg.Timezone)
	if err != nil {
		return fmt.Errorf("invalid timezone %q: %w", cfg.Timezone, err)
	}
	i.location = loc

	// Validate schedules
	if len(cfg.Schedules) == 0 {
		return errors.New("at least one schedule must be configured")
	}

	enabledCount := 0
	for idx, sched := range cfg.Schedules {
		if sched.Name == "" {
			return fmt.Errorf("schedule[%d]: name is required", idx)
		}
		if sched.Cron == "" {
			return fmt.Errorf("schedule[%d] %q: cron expression is required", idx, sched.Name)
		}
		if !sched.IsEnabled() {
			continue
		}
		enabledCount++

		// Validate cron expression
		parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
		if _, err := parser.Parse(sched.Cron); err != nil {
			return fmt.Errorf("schedule[%d] %q: invalid cron expression %q: %w", idx, sched.Name, sched.Cron, err)
		}
	}

	if enabledCount == 0 {
		logger.Warn("No enabled schedules found; cron impulse will be idle")
	}

	i.cfg = &cfg
	i.secrets = secrets
	i.logger = logger

	logger.Info("Cron impulse initialized",
		slog.Int("schedules", len(cfg.Schedules)),
		slog.Int("enabledSchedules", enabledCount),
		slog.String("timezone", cfg.Timezone),
		slog.String("concurrencyPolicy", cfg.ConcurrencyPolicy),
	)

	return nil
}

// Run starts the cron scheduler and health check server.
func (i *CronImpulse) Run(ctx context.Context, k8sClient *sdkk8s.Client) error {
	logger := i.loggerWithContext(ctx)
	i.k8sClient = k8sClient

	// Create cron scheduler with timezone
	i.cron = cron.New(
		cron.WithLocation(i.location),
		cron.WithParser(cron.NewParser(
			cron.Minute|cron.Hour|cron.Dom|cron.Month|cron.Dow|cron.Descriptor,
		)),
		cron.WithLogger(cronLogAdapter{logger: logger}),
	)

	// Register schedules
	for _, sched := range i.cfg.Schedules {
		if !sched.IsEnabled() {
			logger.Info("Skipping disabled schedule", slog.String("name", sched.Name))
			continue
		}

		schedule := sched // capture for closure
		entryID, err := i.cron.AddFunc(schedule.Cron, func() {
			i.triggerSchedule(ctx, logger, &schedule)
		})
		if err != nil {
			return fmt.Errorf("failed to add schedule %q: %w", schedule.Name, err)
		}

		logger.Info("Registered cron schedule",
			slog.String("name", schedule.Name),
			slog.String("cron", schedule.Cron),
			slog.Int("entryID", int(entryID)),
		)
	}

	// Start the cron scheduler
	i.cron.Start()
	logger.Info("Cron scheduler started", slog.Int("entries", len(i.cron.Entries())))

	// Run on startup if configured
	if i.cfg.RunOnStartup {
		logger.Info("RunOnStartup enabled; triggering all schedules now")
		for _, sched := range i.cfg.Schedules {
			if sched.IsEnabled() {
				schedule := sched
				go i.triggerSchedule(ctx, logger, &schedule)
			}
		}
	}

	// Start health check server (port defined by ImpulseTemplate, fixed at 8080)
	mux := http.NewServeMux()
	mux.HandleFunc("/health", i.healthHandler)
	mux.HandleFunc("/ready", i.readyHandler)
	mux.HandleFunc("/schedules", i.schedulesHandler)

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", defaultHealthPort),
		Handler: mux,
	}

	logger.Info("Health server listening", slog.String("addr", srv.Addr))

	// Graceful shutdown
	go func() {
		<-ctx.Done()
		logger.Info("Shutting down cron impulse...")

		// Stop cron scheduler
		cronCtx := i.cron.Stop()
		select {
		case <-cronCtx.Done():
			logger.Info("Cron scheduler stopped")
		case <-time.After(30 * time.Second):
			logger.Warn("Cron scheduler shutdown timed out")
		}

		// Stop HTTP server
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		return fmt.Errorf("health server error: %w", err)
	}
	return nil
}

// triggerSchedule submits a StoryTrigger request for the given schedule.
func (i *CronImpulse) triggerSchedule(ctx context.Context, logger *slog.Logger, sched *cfgpkg.Schedule) {
	runID := generateRunID()

	logger = logger.With(
		slog.String("schedule", sched.Name),
		slog.String("runID", runID),
	)

	sessionKey := i.sessionKeyForSchedule(sched.Name, runID)

	// Check concurrency policy against active StoryRun session, not just trigger call duration.
	if !i.canStartRun(ctx, sessionKey, logger) {
		return
	}

	// Apply jitter if configured
	if sched.Jitter > 0 {
		jitter := randomDuration(sched.Jitter)
		logger.Info("Applying jitter before trigger", slog.Duration("jitter", jitter))
		select {
		case <-time.After(jitter):
		case <-ctx.Done():
			logger.Info("Context canceled during jitter wait")
			return
		}
	}

	// Build inputs from schedule
	inputs := make(map[string]any)
	maps.Copy(inputs, sched.Inputs)

	// Add cron metadata
	inputs["_cron"] = map[string]any{
		"schedule":    sched.Name,
		"description": sched.Description,
		"triggeredAt": time.Now().In(i.location).Format(time.RFC3339),
		"runID":       runID,
	}

	logger.Info("Triggering scheduled story run")

	// Use SDK's story dispatcher - story name is resolved from Impulse.spec.storyRef
	// via environment variables (BUBU_TARGET_STORY_NAME, BUBU_TARGET_STORY_NAMESPACE)
	req := sdk.StoryTriggerRequest{
		Key:      sessionKey,
		Inputs:   inputs,
		Metadata: sched.Metadata,
	}

	result, err := i.dispatcher.Trigger(ctx, req)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			logger.Info("Scheduled run was canceled")
			return
		}
		logger.Error("Failed to trigger story", slog.Any("error", err))
		return
	}

	if result.Session != nil {
		logger.Info("Successfully triggered story run",
			slog.String("namespace", result.Session.Namespace),
			slog.String("storyRun", result.Session.StoryRun),
		)
	}
}

// canStartRun checks if a new run can start based on concurrency policy.
func (i *CronImpulse) canStartRun(ctx context.Context, sessionKey string, logger *slog.Logger) bool {
	if i.cfg.ConcurrencyPolicy == concurrencyPolicyAllow {
		return true
	}
	if !i.isSessionActive(ctx, sessionKey, logger) {
		return true
	}

	switch i.cfg.ConcurrencyPolicy {
	case "forbid":
		logger.Info("Skipping run due to concurrency policy (forbid): previous run still active")
		return false
	case "replace":
		logger.Info("Canceling previous run due to concurrency policy (replace)")
		_, err := i.dispatcher.Stop(ctx, sessionKey)
		if err != nil && !errors.Is(err, sdk.ErrImpulseSessionNotFound) && !errors.Is(err, sdk.ErrStoryRunNotFound) {
			logger.Warn("Failed to stop previous run for replace policy",
				slog.String("sessionKey", sessionKey),
				slog.Any("error", err))
			return false
		}
		return true
	default:
		return true
	}
}

func (i *CronImpulse) sessionKeyForSchedule(scheduleName, runID string) string {
	if i.cfg.ConcurrencyPolicy == concurrencyPolicyAllow {
		return fmt.Sprintf("cron-%s-%s", scheduleName, runID)
	}
	return fmt.Sprintf("cron-%s", scheduleName)
}

func (i *CronImpulse) isSessionActive(ctx context.Context, sessionKey string, logger *slog.Logger) bool {
	if !i.dispatcher.HasSession(sessionKey) {
		return false
	}
	session, ok := i.dispatcher.Session(sessionKey)
	if !ok {
		return false
	}
	if i.k8sClient == nil {
		return true
	}

	storyRun := &runsv1alpha1.StoryRun{}
	key := types.NamespacedName{Name: session.StoryRun, Namespace: session.Namespace}
	if err := i.k8sClient.Get(ctx, key, storyRun); err != nil {
		if apierrors.IsNotFound(err) {
			i.dispatcher.Forget(sessionKey)
			return false
		}
		logger.Warn("Could not inspect active StoryRun for concurrency policy",
			slog.String("sessionKey", sessionKey),
			slog.String("storyRunNamespace", session.Namespace),
			slog.String("storyRun", session.StoryRun),
			slog.Any("error", err))
		return true
	}

	switch strings.TrimSpace(string(storyRun.Status.Phase)) {
	case "Succeeded", "Failed", "Finished":
		i.dispatcher.Forget(sessionKey)
		return false
	default:
		return true
	}
}

// Health check handlers
func (i *CronImpulse) healthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprint(w, "ok")
}

func (i *CronImpulse) readyHandler(w http.ResponseWriter, r *http.Request) {
	if i.cron == nil || len(i.cron.Entries()) == 0 {
		http.Error(w, "no schedules registered", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprintf(w, "ready: %d schedules", len(i.cron.Entries()))
}

func (i *CronImpulse) schedulesHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	for _, entry := range i.cron.Entries() {
		next := entry.Next.In(i.location).Format(time.RFC3339)
		_, _ = fmt.Fprintf(w, "ID=%d Next=%s\n", entry.ID, next)
	}
}

// cronLogAdapter adapts slog.Logger to cron.Logger interface.
type cronLogAdapter struct {
	logger *slog.Logger
}

func (a cronLogAdapter) Info(msg string, keysAndValues ...interface{}) {
	a.logger.Info(msg, keysAndValues...)
}

func (a cronLogAdapter) Error(err error, msg string, keysAndValues ...interface{}) {
	args := append([]interface{}{slog.Any("error", err)}, keysAndValues...)
	a.logger.Error(msg, args...)
}

// Helper functions

func generateRunID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func randomDuration(max time.Duration) time.Duration {
	if max <= 0 {
		return 0
	}
	n, _ := rand.Int(rand.Reader, big.NewInt(int64(max)))
	return time.Duration(n.Int64())
}
