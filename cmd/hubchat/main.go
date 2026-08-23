// Command hubchat is the entire Hubchat application: HTTP server, realtime
// gateway, background worker, scheduler, migrations, and administrative tools,
// in one binary (§3.1).
//
// Subcommands rather than flags-only, because the operations an administrator
// performs (migrate, doctor, export) are genuinely different programs sharing
// one configuration loader — not modes of the server.
package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/hubchat/hubchat/embedded"
	"github.com/hubchat/hubchat/internal/analytics"
	"github.com/hubchat/hubchat/internal/api"
	"github.com/hubchat/hubchat/internal/apikey"
	"github.com/hubchat/hubchat/internal/audit"
	"github.com/hubchat/hubchat/internal/auth"
	"github.com/hubchat/hubchat/internal/automation"
	"github.com/hubchat/hubchat/internal/config"
	"github.com/hubchat/hubchat/internal/conversation"
	"github.com/hubchat/hubchat/internal/customer"
	"github.com/hubchat/hubchat/internal/database"
	"github.com/hubchat/hubchat/internal/emailchannel"
	"github.com/hubchat/hubchat/internal/emailtemplate"
	"github.com/hubchat/hubchat/internal/events"
	"github.com/hubchat/hubchat/internal/feedback"
	filemodule "github.com/hubchat/hubchat/internal/file"
	formmodule "github.com/hubchat/hubchat/internal/form"
	"github.com/hubchat/hubchat/internal/geoip"
	"github.com/hubchat/hubchat/internal/httpserver"
	"github.com/hubchat/hubchat/internal/ids"
	"github.com/hubchat/hubchat/internal/inbox"
	"github.com/hubchat/hubchat/internal/jobs"
	"github.com/hubchat/hubchat/internal/knowledgebase"
	"github.com/hubchat/hubchat/internal/mailer"
	"github.com/hubchat/hubchat/internal/notification"
	"github.com/hubchat/hubchat/internal/portability"
	"github.com/hubchat/hubchat/internal/portal"
	"github.com/hubchat/hubchat/internal/realtime"
	"github.com/hubchat/hubchat/internal/savedview"
	"github.com/hubchat/hubchat/internal/search"
	"github.com/hubchat/hubchat/internal/sla"
	"github.com/hubchat/hubchat/internal/survey"
	"github.com/hubchat/hubchat/internal/task"
	"github.com/hubchat/hubchat/internal/telemetry"
	"github.com/hubchat/hubchat/internal/ticket"
	"github.com/hubchat/hubchat/internal/webhook"
	"github.com/hubchat/hubchat/internal/widget"
	"github.com/hubchat/hubchat/internal/workspace"
	"github.com/jackc/pgx/v5"
)

// Set by the linker at release time; see the Makefile.
var (
	version   = "dev"
	commit    = "none"
	buildDate = "unknown"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "hubchat: %v\n", err)
		os.Exit(1)
	}
}

func oauthOptions(cfg config.Config) *auth.OAuthOptions {
	if !cfg.OAuth.Enabled || cfg.Server.PublicURL == nil {
		return nil
	}
	callback := *cfg.Server.PublicURL
	callback.Path = strings.TrimSuffix(callback.Path, "/") + "/api/v1/auth/oauth/" + url.PathEscape(cfg.OAuth.Provider) + "/callback"
	callback.RawQuery = ""
	return &auth.OAuthOptions{
		Provider:         cfg.OAuth.Provider,
		Profile:          cfg.OAuth.Profile,
		ClientID:         cfg.OAuth.ClientID,
		ClientSecret:     cfg.OAuth.ClientSecret,
		AuthorizationURL: cfg.OAuth.AuthorizationURL,
		TokenURL:         cfg.OAuth.TokenURL,
		UserinfoURL:      cfg.OAuth.UserinfoURL,
		RedirectURL:      callback.String(),
		Scopes:           cfg.OAuth.Scopes,
		AllowedDomains:   cfg.OAuth.AllowedDomains,
	}
}

func run(args []string) error {
	command := "serve"
	if len(args) > 0 {
		command = args[0]
		args = args[1:]
	}

	switch command {
	case "serve":
		return serve(args)
	case "migrate":
		return migrate(args)
	case "setup":
		return setupCommand(args)
	case "doctor":
		return doctor(args)
	case "config":
		return configCheck(args)
	case "admin":
		return adminCommand(args)
	case "workspace":
		return workspaceCommand(args)
	case "jobs":
		return jobsCommand(args)
	case "version":
		fmt.Printf("hubchat %s (%s, built %s)\n", version, commit, buildDate)
		return nil
	case "help", "-h", "--help":
		usage()
		return nil
	default:
		usage()
		return fmt.Errorf("unknown command %q", command)
	}
}

func serve(args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if err := applyServeArgs(&cfg, args); err != nil {
		return err
	}

	baseLogger := newLogger(cfg)

	if err := cfg.Validate(); err != nil {
		// Configuration problems are reported as a list, because an operator
		// fixing them should not have to restart once per mistake.
		return fmt.Errorf("configuration is not usable:\n%w", err)
	}

	observer, telemetryErr := telemetry.New(cfg.Observability.DevLite, baseLogger, version, commit)
	if telemetryErr != nil {
		// Observability must never make the support service unavailable. Invalid
		// values are caught by Config.Validate; any remaining SDK failure is
		// reported locally and the process continues without the remote sink.
		baseLogger.Warn("devlite initialization failed", slog.Any("error", telemetryErr))
	}
	defer observer.Close()
	logger := observer.Logger(baseLogger.Handler())

	assets, err := loadAssets(cfg)
	if err != nil {
		logger.Error("could not load browser assets", slog.Any("error", err))
		return err
	}

	// SIGINT and SIGTERM both mean "stop accepting work and finish what you
	// have". Docker sends the latter; a terminal sends the former.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger.Info("starting hubchat",
		slog.String("version", version),
		slog.Any("roles", cfg.Server.Roles),
		slog.String("public_url", cfg.Server.PublicURL.String()),
	)

	app, err := wireAPI(ctx, cfg, logger, observer)
	if err != nil {
		logger.Error("could not initialize hubchat", slog.Any("error", err))
		return err
	}
	defer app.Close()
	observer.StartMetrics(ctx, cfg.Observability.DevLite.MetricsInterval, app.pool, app.jobs)

	// Background roles run alongside the HTTP server in the same process by
	// default (§8.5). They are started before the listener so that work
	// enqueued during startup is already being drained by the time the first
	// request arrives.
	var background sync.WaitGroup
	if app.Worker != nil && cfg.Server.Has(config.RoleWorker) {
		background.Add(1)
		go func() {
			defer background.Done()
			app.Worker.Run(ctx)
		}()
	}
	if app.Listener != nil && cfg.Realtime.Enabled && cfg.Server.Has(config.RoleRealtime) {
		background.Add(1)
		go func() {
			defer background.Done()
			app.Listener.Run(ctx)
		}()
	}
	if app.WebhookListener != nil && cfg.Jobs.Enabled && cfg.Server.Has(config.RoleWorker) {
		background.Add(1)
		go func() {
			defer background.Done()
			app.WebhookListener.Run(ctx)
		}()
	}
	if app.SLAListener != nil && cfg.Jobs.Enabled && cfg.Server.Has(config.RoleWorker) {
		background.Add(1)
		go func() {
			defer background.Done()
			app.SLAListener.Run(ctx)
		}()
	}
	if app.AutomationListener != nil && cfg.Jobs.Enabled && cfg.Server.Has(config.RoleWorker) {
		background.Add(1)
		go func() {
			defer background.Done()
			app.AutomationListener.Run(ctx)
		}()
	}
	if app.EmailListener != nil && cfg.Jobs.Enabled && cfg.Server.Has(config.RoleWorker) {
		background.Add(1)
		go func() {
			defer background.Done()
			app.EmailListener.Run(ctx)
		}()
	}
	if app.NotificationListener != nil && cfg.Jobs.Enabled && cfg.Server.Has(config.RoleWorker) {
		background.Add(1)
		go func() {
			defer background.Done()
			app.NotificationListener.Run(ctx)
		}()
	}

	var serverErr error
	if cfg.Server.Has(config.RoleHTTP) {
		server, err := httpserver.New(cfg, logger, assets, app.Routes, observer.Middleware)
		if err != nil {
			logger.Error("could not initialize http server", slog.Any("error", err))
			return err
		}
		serverErr = server.Start(ctx)
		if serverErr != nil {
			logger.Error("http server stopped with an error", slog.Any("error", serverErr))
		}
	} else {
		// A worker/scheduler-only process has no listener to block on. Keep the
		// same signal-driven lifetime as the HTTP process so orchestration can
		// stop it cleanly and background roles can drain before the database
		// closes.
		<-ctx.Done()
	}

	// Start returns once the listener has drained. Wait for the background
	// roles to notice the same cancellation before closing the pool out from
	// under them, or a job mid-write loses its outcome.
	background.Wait()

	return serverErr
}

func applyServeArgs(cfg *config.Config, args []string) error {
	for index := 0; index < len(args); index++ {
		arg := args[index]
		var value string
		switch {
		case arg == "--roles":
			if index+1 >= len(args) {
				return errors.New("serve: --roles requires a value")
			}
			index++
			value = args[index]
		case strings.HasPrefix(arg, "--roles="):
			value = strings.TrimPrefix(arg, "--roles=")
		default:
			return fmt.Errorf("serve: unknown argument %q", arg)
		}
		roles, err := config.ParseRoles(value)
		if err != nil {
			return fmt.Errorf("serve: %w", err)
		}
		cfg.Server.Roles = roles
	}
	return nil
}

// application is everything wireAPI constructed, kept together so serve can
// start the background roles and shut the whole thing down in one place.
type application struct {
	Routes               httpserver.Routes
	Worker               *jobs.Worker
	Listener             *events.Listener
	WebhookListener      *events.Listener
	SLAListener          *events.Listener
	AutomationListener   *events.Listener
	EmailListener        *events.Listener
	NotificationListener *events.Listener
	pool                 *database.Pool
	jobs                 *jobs.Client
	closeDB              func()
}

func (a *application) Close() {
	if a.closeDB != nil {
		a.closeDB()
	}
}

// wireAPI connects to the database, applies (or verifies) migrations
// according to cfg.Database.MigratePolicy, constructs every module service in
// dependency order, and wires realtime broadcasting between conversation and
// the WebSocket hub.
//
// This is the one place the concrete dependency graph is assembled — the
// "app" role described in internal/app/doc.go. It lives in main rather than
// in that package for now because there is exactly one binary entry point
// that needs it; if a second one appears, this function is what moves.
//
// A missing HUBCHAT_DATABASE_URL is not fatal at this layer: the server still
// starts and serves the compiled frontends and health checks, with the API
// responding 503. That lets `hubchat doctor` and asset-serving smoke tests run
// without a database, while a real deployment's config.Validate() (which does
// require the database URL) still refuses to start improperly configured.
func wireAPI(ctx context.Context, cfg config.Config, logger *slog.Logger, observer *telemetry.Client) (*application, error) {
	if cfg.Database.URL == "" {
		return &application{}, nil
	}

	pool, err := database.Open(ctx, cfg.Database)
	if err != nil {
		return nil, err
	}

	migrations, err := embedded.Migrations()
	if err != nil {
		pool.Close()
		return nil, err
	}

	switch cfg.Database.MigratePolicy {
	case config.MigrateApply:
		applied, err := database.Migrate(ctx, pool, migrations)
		if err != nil {
			pool.Close()
			return nil, fmt.Errorf("applying migrations: %w", err)
		}
		if len(applied) > 0 {
			logger.Info("applied migrations", slog.Any("files", applied))
		}
	case config.MigrateSkip:
		// Deliberately does nothing — used for read replicas and debugging.
	default: // config.MigrateVerify
		if err := database.VerifyNoPending(ctx, pool, migrations); err != nil {
			pool.Close()
			return nil, err
		}
	}

	// Infrastructure first: every module service below may append events,
	// enqueue jobs, or write audit records, so these have no module
	// dependencies of their own and are constructed before anything else.
	eventLog := events.New(pool)
	auditLog := audit.New(pool)
	jobClient := jobs.NewClient(pool)

	authService := auth.New(pool, auth.Options{
		SessionLifetime: cfg.Security.SessionLifetime,
		CookieDomain:    cfg.Security.CookieDomain,
		CookieSecure:    cfg.Security.CookieSecure,
		LoginAttempts:   cfg.Security.LoginAttempts,
		LockoutWindow:   cfg.Security.LockoutWindow,
		OAuth:           oauthOptions(cfg),
	})
	workspaceService := workspace.New(pool, eventLog, auditLog)
	// Services publish by appending to the event log, not by calling realtime
	// directly. That is why nothing below is wired to the hub: the hub reads
	// what was written, so a message reaches a connected browser whether it
	// was created by this process, another one, or a background job.
	conversationService := conversation.New(pool, eventLog, auditLog)
	inboxService := inbox.New(pool, eventLog, auditLog)
	customerService := customer.New(pool, eventLog, auditLog, cfg.Limits)
	geoIPResolver, err := geoip.Open(cfg.Security.GeoIPDatabasePath)
	if err != nil {
		pool.Close()
		return nil, fmt.Errorf("open GeoIP database: %w", err)
	}
	searchService := search.New(conversationService, customerService)
	ticketService := ticket.New(pool, workspaceService, eventLog, auditLog)
	portalService := portal.New(pool, portal.Options{SessionLifetime: cfg.Security.SessionLifetime})
	notificationService := notification.New(pool, jobClient)
	notificationService.SetPublicURL(cfg.Server.PublicURL)
	apiKeyService := apikey.New(pool)
	webhookService := webhook.New(pool, cfg.Security.SecretKey, jobClient)
	emailChannelService := emailchannel.New(pool, cfg.Security.SecretKey, conversationService, customerService, inboxService, jobClient)
	emailTemplateService := emailtemplate.New(pool)
	knowledgebaseService := knowledgebase.New(pool, knowledgebase.Options{Events: eventLog})
	feedbackService := feedback.New(pool, eventLog, auditLog)
	surveyService := survey.New(pool, survey.Options{Jobs: jobClient, PublicURL: cfg.Server.PublicURL, Events: eventLog})
	notificationService.SetSurveyDispatcher(surveyService)
	slaService := sla.New(pool, eventLog)
	taskService := task.New(pool)
	automationService := automation.New(pool, automation.Options{
		Conversation: conversationService,
		Ticket:       ticketService,
		Jobs:         jobClient,
		SLA:          slaService,
		Tasks:        taskService,
		Webhook:      webhookService,
	})
	savedViewService := savedview.New(pool, eventLog, auditLog)
	analyticsService := analytics.New(pool)
	formService := formmodule.New(pool, formmodule.TargetServices{
		Conversation: conversationService,
		Customer:     customerService,
		Feedback:     feedbackService,
		Inbox:        inboxService,
		Survey:       surveyService,
		Ticket:       ticketService,
	})
	widgetService := widget.New(pool, eventLog, auditLog, inboxService, conversationService, customerService, cfg.Security.SecretKey)
	var fileStore filemodule.Store
	if cfg.Storage.Backend == "s3" {
		fileStore, err = filemodule.NewS3Store(cfg.Storage.S3Endpoint, cfg.Storage.S3Region, cfg.Storage.S3Bucket, cfg.Storage.S3AccessKey, cfg.Storage.S3SecretKey, cfg.Storage.S3PathStyle, cfg.Storage.MaxFileBytes, cfg.Storage.AllowedMimeTypes)
	} else {
		fileStore, err = filemodule.NewLocalStore(cfg.Storage.LocalPath, cfg.Storage.MaxFileBytes, cfg.Storage.AllowedMimeTypes)
	}
	if err != nil {
		pool.Close()
		return nil, fmt.Errorf("file storage: %w", err)
	}
	fileService := filemodule.New(pool, fileStore, auditLog)
	emailChannelService.SetFileService(fileService)
	notificationService.SetFileService(fileService)
	portabilityService := portability.New(pool, fileService, jobClient)
	portabilityService.SetCustomerImporter(customerService)
	portabilityService.SetTicketImporter(ticketService)
	portabilityService.SetFeedbackImporter(feedbackService)
	portabilityService.SetKnowledgeBaseImporter(knowledgebaseService)

	hub := realtime.NewHub(logger, cfg.Realtime.OutboundQueueSize)

	deps := api.Deps{
		Pool:           pool,
		Logger:         logger,
		Auth:           authService,
		Workspace:      workspaceService,
		Conversation:   conversationService,
		Inbox:          inboxService,
		Customer:       customerService,
		Search:         searchService,
		Ticket:         ticketService,
		Widget:         widgetService,
		GeoIP:          geoIPResolver,
		File:           fileService,
		Portal:         portalService,
		Notification:   notificationService,
		Form:           formService,
		APIKeys:        apiKeyService,
		Webhook:        webhookService,
		Knowledgebase:  knowledgebaseService,
		Feedback:       feedbackService,
		Survey:         surveyService,
		SLA:            slaService,
		Task:           taskService,
		Automation:     automationService,
		SavedView:      savedViewService,
		Analytics:      analyticsService,
		EmailChannel:   emailChannelService,
		EmailTemplates: emailTemplateService,
		Portability:    portabilityService,
		Hub:            hub,
		Events:         eventLog,
		Audit:          auditLog,
		Jobs:           jobClient,
		Telemetry:      observer,
		PublicURL:      cfg.Server.PublicURL,
		Config:         cfg,
		CookieDomain:   cfg.Security.CookieDomain,
		CookieSecure:   cfg.Security.CookieSecure,
	}

	var wsHandler http.Handler
	if cfg.Realtime.Enabled && cfg.Server.Has(config.RoleRealtime) {
		wsHandler = api.NewWebSocketHandler(deps, hub)
	}

	app := &application{
		Routes: httpserver.Routes{
			API:   api.New(deps),
			WS:    wsHandler,
			Ready: deps.Ready,
		},
		pool: pool,
		jobs: jobClient,
		closeDB: func() {
			_ = geoIPResolver.Close()
			pool.Close()
		},
	}

	if cfg.Jobs.Enabled && (cfg.Server.Has(config.RoleWorker) || cfg.Server.Has(config.RoleScheduler)) {
		app.Worker = jobs.NewWorker(pool, logger, cfg.Jobs)
		registerJobHandlers(app.Worker, jobClient, mailer.New(cfg.Email, logger), fileService, emailChannelService, emailTemplateService, portabilityService, automationService, conversationService, customerService, webhookService, surveyService, widgetService, auditLog, slaService, analyticsService, knowledgebaseService, logger)

		if cfg.Server.Has(config.RoleScheduler) {
			// Primes the self-perpetuating snooze-wake tick (see JobWakeSnoozed's
			// doc comment). ErrDuplicate means one is already pending — a restart
			// racing its own previous tick, not a problem.
			if _, err := jobClient.Enqueue(ctx, jobs.Spec{
				Type: conversation.JobWakeSnoozed, DedupeKey: "wake-snoozed",
			}); err != nil && !errors.Is(err, jobs.ErrDuplicate) {
				logger.Warn("could not prime snooze wake job", slog.Any("error", err))
			}
			if _, err := jobClient.Enqueue(ctx, jobs.Spec{
				Type: emailchannel.JobPollIMAP, DedupeKey: "email-imap-poll",
			}); err != nil && !errors.Is(err, jobs.ErrDuplicate) {
				logger.Warn("could not prime IMAP poll job", slog.Any("error", err))
			}
			if _, err := jobClient.Enqueue(ctx, jobs.Spec{
				Type: customer.JobRetentionSweep, DedupeKey: "retention-sweep",
			}); err != nil && !errors.Is(err, jobs.ErrDuplicate) {
				logger.Warn("could not prime retention sweep job", slog.Any("error", err))
			}
			if _, err := jobClient.Enqueue(ctx, jobs.Spec{
				Type: filemodule.JobCleanupAbandoned, DedupeKey: "file-cleanup-abandoned",
			}); err != nil && !errors.Is(err, jobs.ErrDuplicate) {
				logger.Warn("could not prime abandoned upload cleanup job", slog.Any("error", err))
			}
			if _, err := jobClient.Enqueue(ctx, jobs.Spec{
				Type: portability.JobExpireExports, DedupeKey: "portability-expire-exports",
			}); err != nil && !errors.Is(err, jobs.ErrDuplicate) {
				logger.Warn("could not prime expired export cleanup job", slog.Any("error", err))
			}
			if _, err := jobClient.Enqueue(ctx, jobs.Spec{
				Type: sla.JobEvaluate, DedupeKey: "sla-evaluate",
			}); err != nil && !errors.Is(err, jobs.ErrDuplicate) {
				logger.Warn("could not prime SLA evaluation job", slog.Any("error", err))
			}
			if _, err := jobClient.Enqueue(ctx, jobs.Spec{
				Type: analytics.JobRollup, DedupeKey: "analytics-rollup",
			}); err != nil && !errors.Is(err, jobs.ErrDuplicate) {
				logger.Warn("could not prime analytics rollup job", slog.Any("error", err))
			}
			if _, err := jobClient.Enqueue(ctx, jobs.Spec{
				Type: knowledgebase.JobPublishScheduled, DedupeKey: "knowledgebase-publish-scheduled",
			}); err != nil && !errors.Is(err, jobs.ErrDuplicate) {
				logger.Warn("could not prime scheduled knowledge-base publish job", slog.Any("error", err))
			}
			if _, err := jobClient.Enqueue(ctx, jobs.Spec{
				Type: analytics.JobScheduledReports, DedupeKey: "analytics-scheduled-reports",
			}); err != nil && !errors.Is(err, jobs.ErrDuplicate) {
				logger.Warn("could not prime scheduled report job", slog.Any("error", err))
			}
			if _, err := jobClient.Enqueue(ctx, jobs.Spec{
				Type: automation.JobRunScheduled, DedupeKey: "automation-scheduled-actions",
			}); err != nil && !errors.Is(err, jobs.ErrDuplicate) {
				logger.Warn("could not prime scheduled automation job", slog.Any("error", err))
			}
		}
	}

	// The listener turns another process's writes into local realtime
	// delivery. A single-process deployment still runs it: its own writes
	// arrive the same way, so there is one delivery path to reason about
	// rather than two that can diverge.
	if cfg.Realtime.Enabled && cfg.Server.Has(config.RoleRealtime) {
		app.Listener = events.NewListener(pool, logger)
		hub.Subscribe(ctx, app.Listener.Signals(), eventLog)
	}
	if cfg.Jobs.Enabled && cfg.Server.Has(config.RoleWorker) {
		app.WebhookListener = events.NewListener(pool, logger)
		go webhookService.RunEventConsumer(ctx, app.WebhookListener.Signals(), eventLog)
		app.SLAListener = events.NewListener(pool, logger)
		go slaService.RunEventConsumer(ctx, app.SLAListener.Signals(), eventLog)
		app.AutomationListener = events.NewListener(pool, logger)
		go automationService.RunEventConsumer(ctx, app.AutomationListener.Signals(), eventLog)
		app.EmailListener = events.NewListener(pool, logger)
		go emailChannelService.RunEventConsumer(ctx, app.EmailListener.Signals(), eventLog)
		app.NotificationListener = events.NewListener(pool, logger)
		go notificationService.RunEventConsumer(ctx, app.NotificationListener.Signals(), eventLog)
	}

	return app, nil
}

// registerJobHandlers binds every background job type to its handler.
func openEmailAttachment(ctx context.Context, files *filemodule.Service, workspaceID, id string) (filemodule.Record, []byte, error) {
	if files == nil {
		return filemodule.Record{}, nil, errors.New("email: attachment storage is unavailable")
	}
	record, reader, err := files.Open(ctx, workspaceID, id)
	if err != nil {
		return filemodule.Record{}, nil, fmt.Errorf("email: open attachment %s: %w", id, err)
	}
	defer reader.Close()
	body, err := io.ReadAll(io.LimitReader(reader, record.SizeBytes+1))
	if err != nil {
		return filemodule.Record{}, nil, fmt.Errorf("email: read attachment %s: %w", id, err)
	}
	if int64(len(body)) != record.SizeBytes {
		return filemodule.Record{}, nil, fmt.Errorf("email: attachment %s changed while sending", id)
	}
	return *record, body, nil
}

// Handlers land here as their owning modules are built. Keeping the
// registrations in one function means "what work can this binary do" is
// answerable by reading it, rather than by grepping for Register calls.
func registerJobHandlers(
	worker *jobs.Worker, jobClient *jobs.Client, sender *mailer.SMTPSender,
	fileService *filemodule.Service, emailChannelService *emailchannel.Service, emailTemplateService *emailtemplate.Service, portabilityService *portability.Service, automationService *automation.Service, conversationService *conversation.Service, customerService *customer.Service, webhookService *webhook.Service, surveyService *survey.Service, widgetService *widget.Service, auditLog *audit.Log, slaService *sla.Service, analyticsService *analytics.Service, knowledgebaseService *knowledgebase.Service, logger *slog.Logger,
) {
	worker.Register(api.JobEmailSend, func(ctx context.Context, job *jobs.Job) error {
		var payload api.EmailPayload
		if err := job.Decode(&payload); err != nil {
			// A payload that will never parse is not worth five attempts.
			return jobs.Permanent(err)
		}
		if payload.TemplateKey != "" && payload.WorkspaceID != "" && emailTemplateService != nil {
			subject, body, renderErr := emailTemplateService.Render(ctx, payload.WorkspaceID, payload.TemplateKey, payload.TemplateData)
			if renderErr != nil {
				return jobs.Permanent(fmt.Errorf("render customer email template: %w", renderErr))
			}
			payload.Subject, payload.Body = subject, body
		}

		message := mailer.Message{
			To:        payload.To,
			Subject:   payload.Subject,
			Body:      payload.Body,
			ReplyTo:   payload.ReplyTo,
			MessageID: payload.MessageID,
			InReplyTo: payload.InReplyTo,
		}
		for _, attachmentID := range payload.AttachmentIDs {
			record, body, openErr := openEmailAttachment(ctx, fileService, payload.WorkspaceID, attachmentID)
			if openErr != nil {
				return openErr
			}
			message.Attachments = append(message.Attachments, mailer.Attachment{
				Name: record.Name, MIMEType: record.MIMEType, Body: body,
			})
		}
		err := sender.Send(ctx, message)
		if errors.Is(err, mailer.ErrNotConfigured) {
			// A self-hosted instance with no SMTP server is a supported
			// configuration (§8.1). Retrying forever would fill the dead-letter
			// queue with mail that was never going to send, so this is logged
			// and dropped rather than treated as a failure.
			logger.Warn("email not sent: no SMTP server configured",
				"to", payload.To, "subject", payload.Subject)
			if payload.EmailMessageID != "" {
				if markErr := emailChannelService.MarkFailed(ctx, payload.WorkspaceID, payload.EmailMessageID, err); markErr != nil {
					logger.Warn("could not record email delivery failure", "email_message_id", payload.EmailMessageID, "error", markErr)
				}
			}
			return nil
		}
		if err != nil {
			if payload.EmailMessageID != "" {
				if markErr := emailChannelService.MarkFailed(ctx, payload.WorkspaceID, payload.EmailMessageID, err); markErr != nil {
					logger.Warn("could not record email delivery failure", "email_message_id", payload.EmailMessageID, "error", markErr)
				}
			}
			return err
		}
		if payload.EmailMessageID != "" {
			if markErr := emailChannelService.MarkSent(ctx, payload.WorkspaceID, payload.EmailMessageID); markErr != nil {
				return markErr
			}
		}
		return err
	})

	worker.Register(conversation.JobWakeSnoozed, func(ctx context.Context, job *jobs.Job) error {
		woken, err := conversationService.WakeSnoozed(ctx)
		if err != nil {
			return err
		}
		if woken > 0 {
			logger.Info("woke snoozed conversations", "count", woken)
		}

		// Re-enqueues itself for the next tick. Include the completed job ID in
		// the key so the next row can be created while this row is still marked
		// running; duplicate delivery remains harmless.
		if _, err := jobClient.Enqueue(ctx, jobs.Spec{
			Type: conversation.JobWakeSnoozed, RunAt: time.Now().Add(wakeSnoozedInterval),
			DedupeKey: "wake-snoozed:" + job.ID,
		}); err != nil && !errors.Is(err, jobs.ErrDuplicate) {
			return err
		}
		return nil
	})

	worker.Register(emailchannel.JobPollIMAP, func(ctx context.Context, job *jobs.Job) error {
		processed, pollErr := emailChannelService.PollIMAP(ctx)
		if processed > 0 {
			logger.Info("polled IMAP messages", "count", processed)
		}
		if _, err := jobClient.Enqueue(ctx, jobs.Spec{
			Type: emailchannel.JobPollIMAP, RunAt: time.Now().Add(emailchannel.IMAPPollEvery),
			DedupeKey: "email-imap-poll:" + job.ID,
		}); err != nil && !errors.Is(err, jobs.ErrDuplicate) {
			if pollErr == nil {
				return err
			}
			logger.Warn("could not schedule next IMAP poll", "error", err)
		}
		return pollErr
	})

	worker.Register(customer.JobRetentionSweep, func(ctx context.Context, job *jobs.Job) error {
		eventsDeleted, sessionsDeleted, err := customerService.RunRetentionSweep(ctx)
		if err != nil {
			return err
		}
		webhooksDeleted, err := webhookService.RetentionSweep(ctx)
		if err != nil {
			return err
		}
		surveysDeleted, err := surveyService.RetentionSweep(ctx)
		if err != nil {
			return err
		}
		identityNoncesDeleted, err := widgetService.SweepIdentityNonces(ctx, time.Now().UTC(), 1000)
		if err != nil {
			return err
		}
		auditDeleted, err := auditLog.RetentionSweep(ctx)
		if err != nil {
			return err
		}
		if eventsDeleted > 0 || sessionsDeleted > 0 || webhooksDeleted > 0 || surveysDeleted > 0 || identityNoncesDeleted > 0 || auditDeleted > 0 {
			logger.Info("retention sweep", "events_deleted", eventsDeleted, "sessions_deleted", sessionsDeleted, "webhooks_deleted", webhooksDeleted, "surveys_deleted", surveysDeleted, "identity_nonces_deleted", identityNoncesDeleted, "audit_deleted", auditDeleted)
		}

		if _, err := jobClient.Enqueue(ctx, jobs.Spec{
			Type: customer.JobRetentionSweep, RunAt: time.Now().Add(retentionSweepInterval),
			DedupeKey: "retention-sweep:" + job.ID,
		}); err != nil && !errors.Is(err, jobs.ErrDuplicate) {
			return err
		}
		return nil
	})

	worker.Register(filemodule.JobCleanupAbandoned, func(ctx context.Context, job *jobs.Job) error {
		removed, err := fileService.SweepAbandoned(ctx, time.Now().UTC().Add(-abandonedUploadAge), 100)
		if removed > 0 {
			logger.Info("cleaned abandoned uploads", "count", removed)
		}
		if _, scheduleErr := jobClient.Enqueue(ctx, jobs.Spec{
			Type: filemodule.JobCleanupAbandoned, RunAt: time.Now().Add(abandonedUploadSweepEvery),
			DedupeKey: "file-cleanup-abandoned:" + job.ID,
		}); scheduleErr != nil && !errors.Is(scheduleErr, jobs.ErrDuplicate) {
			if err == nil {
				return scheduleErr
			}
			logger.Warn("could not schedule abandoned upload cleanup", "error", scheduleErr)
		}
		return err
	})

	worker.Register(webhook.JobDeliver, func(ctx context.Context, job *jobs.Job) error {
		var payload struct {
			DeliveryID string `json:"delivery_id"`
		}
		if err := job.Decode(&payload); err != nil || payload.DeliveryID == "" {
			if err == nil {
				err = errors.New("webhook delivery id is required")
			}
			return jobs.Permanent(err)
		}
		return webhookService.Deliver(ctx, payload.DeliveryID)
	})

	worker.Register(sla.JobEvaluate, func(ctx context.Context, job *jobs.Job) error {
		warnings, breaches, err := slaService.Evaluate(ctx, time.Now().UTC())
		if err != nil {
			return err
		}
		if warnings > 0 || breaches > 0 {
			logger.Info("SLA timers evaluated", "warnings", warnings, "breaches", breaches)
		}
		if _, err := jobClient.Enqueue(ctx, jobs.Spec{Type: sla.JobEvaluate, RunAt: time.Now().Add(slaEvaluationInterval()), DedupeKey: "sla-evaluate:" + job.ID}); err != nil && !errors.Is(err, jobs.ErrDuplicate) {
			return err
		}
		return nil
	})

	worker.Register(analytics.JobRollup, func(ctx context.Context, job *jobs.Job) error {
		count, err := analyticsService.FoldAll(ctx, time.Now().UTC())
		if err != nil {
			return err
		}
		if count > 0 {
			logger.Info("analytics events folded", "count", count)
		}
		if _, err := jobClient.Enqueue(ctx, jobs.Spec{Type: analytics.JobRollup, RunAt: time.Now().Add(analyticsRollupInterval), DedupeKey: "analytics-rollup:" + job.ID}); err != nil && !errors.Is(err, jobs.ErrDuplicate) {
			return err
		}
		return nil
	})

	worker.Register(analytics.JobScheduledReports, func(ctx context.Context, job *jobs.Job) error {
		count, err := analyticsService.RunScheduledReports(ctx, time.Now().UTC(), jobClient)
		if err != nil {
			return err
		}
		if count > 0 {
			logger.Info("scheduled reports queued", "count", count)
		}
		if _, err := jobClient.Enqueue(ctx, jobs.Spec{Type: analytics.JobScheduledReports, RunAt: time.Now().Add(scheduledReportsInterval), DedupeKey: "analytics-scheduled-reports:" + job.ID}); err != nil && !errors.Is(err, jobs.ErrDuplicate) {
			return err
		}
		return nil
	})

	worker.Register(knowledgebase.JobPublishScheduled, func(ctx context.Context, job *jobs.Job) error {
		count, err := knowledgebaseService.PublishScheduled(ctx, time.Now().UTC())
		if err != nil {
			return err
		}
		if count > 0 {
			logger.Info("scheduled knowledge-base articles published", "count", count)
		}
		if _, err := jobClient.Enqueue(ctx, jobs.Spec{Type: knowledgebase.JobPublishScheduled, RunAt: time.Now().Add(knowledgebasePublishInterval), DedupeKey: "knowledgebase-publish-scheduled:" + job.ID}); err != nil && !errors.Is(err, jobs.ErrDuplicate) {
			return err
		}
		return nil
	})

	worker.Register(automation.JobRunScheduled, func(ctx context.Context, job *jobs.Job) error {
		executed, failed, err := automationService.RunScheduledActions(ctx)
		if err != nil {
			return err
		}
		if executed > 0 || failed > 0 {
			logger.Info("scheduled automation actions processed", "executed", executed, "failed", failed)
		}
		if _, err := jobClient.Enqueue(ctx, jobs.Spec{Type: automation.JobRunScheduled, RunAt: time.Now().Add(time.Minute), DedupeKey: "automation-scheduled-actions:" + job.ID}); err != nil && !errors.Is(err, jobs.ErrDuplicate) {
			return err
		}
		return nil
	})

	worker.Register(portability.JobExport, func(ctx context.Context, job *jobs.Job) error {
		var payload struct {
			RequestID string `json:"request_id"`
		}
		if err := job.Decode(&payload); err != nil || payload.RequestID == "" {
			if err == nil {
				err = errors.New("portability export request id is required")
			}
			return jobs.Permanent(err)
		}
		return portabilityService.RunExport(ctx, payload.RequestID)
	})

	worker.Register(portability.JobImport, func(ctx context.Context, job *jobs.Job) error {
		var payload struct {
			RequestID string `json:"request_id"`
		}
		if err := job.Decode(&payload); err != nil || payload.RequestID == "" {
			if err == nil {
				err = errors.New("portability import request id is required")
			}
			return jobs.Permanent(err)
		}
		return portabilityService.RunImport(ctx, payload.RequestID)
	})

	worker.Register(portability.JobExpireExports, func(ctx context.Context, job *jobs.Job) error {
		removed, sweepErr := portabilityService.SweepExpiredExports(ctx, time.Now().UTC(), 25)
		if removed > 0 {
			logger.Info("expired workspace exports cleaned", "count", removed)
		}
		if _, err := jobClient.Enqueue(ctx, jobs.Spec{
			Type: portability.JobExpireExports, RunAt: time.Now().Add(portabilityExpiryInterval),
			DedupeKey: "portability-expire-exports:" + job.ID,
		}); err != nil && !errors.Is(err, jobs.ErrDuplicate) {
			if sweepErr == nil {
				return err
			}
			logger.Warn("could not schedule expired export cleanup", "error", err)
		}
		return sweepErr
	})

}

// wakeSnoozedInterval is how often the snooze-wake tick reschedules itself.
// Snoozing is a coarse "come back to this later" tool, not a precision timer,
// so this trades a few seconds of latency for not hammering the jobs table.
const wakeSnoozedInterval = 30 * time.Second

// retentionSweepInterval is how often the customer_events/contact_sessions
// retention sweep runs. Deletion by day-granularity retention windows has no
// need for the snooze tick's near-real-time cadence, so this runs far less
// often.
const retentionSweepInterval = 1 * time.Hour

const abandonedUploadAge = 1 * time.Hour

const abandonedUploadSweepEvery = 1 * time.Hour

const portabilityExpiryInterval = 1 * time.Hour

const defaultSLAEvaluationInterval = 30 * time.Second

// slaEvaluationInterval keeps the normal scheduler cadence conservative while
// allowing a deployment or bounded acceptance environment to tune how quickly
// active timers are observed. Invalid or non-positive values deliberately
// fall back to the production default instead of disabling evaluation.
func slaEvaluationInterval() time.Duration {
	value := strings.TrimSpace(os.Getenv("HUBCHAT_SLA_EVALUATION_INTERVAL"))
	if value == "" {
		return defaultSLAEvaluationInterval
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		return defaultSLAEvaluationInterval
	}
	return parsed
}

const analyticsRollupInterval = 5 * time.Minute

const scheduledReportsInterval = 1 * time.Minute

const knowledgebasePublishInterval = 1 * time.Minute

// loadAssets picks the embedded bundles in production and the on-disk build
// output in development, so a rebuilt frontend appears without recompiling Go.
func loadAssets(cfg config.Config) (httpserver.Assets, error) {
	if cfg.Dev {
		return httpserver.Assets{}, nil
	}

	dashboard, err := embedded.Dashboard()
	if err != nil {
		return httpserver.Assets{}, fmt.Errorf("embedded dashboard: %w", err)
	}

	portal, err := embedded.Portal()
	if err != nil {
		return httpserver.Assets{}, fmt.Errorf("embedded portal: %w", err)
	}

	widget, err := embedded.Widget()
	if err != nil {
		return httpserver.Assets{}, fmt.Errorf("embedded widget: %w", err)
	}

	return httpserver.Assets{Dashboard: dashboard, Portal: portal, Widget: widget}, nil
}

func migrate(args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	migrations, err := embedded.Migrations()
	if err != nil {
		return err
	}

	if len(args) > 0 && args[0] == "status" {
		files, err := listMigrations()
		if err != nil {
			return err
		}

		if cfg.Database.URL == "" {
			fmt.Printf("%d migrations embedded in this binary:\n", len(files))
			for _, name := range files {
				fmt.Printf("  %s\n", name)
			}
			fmt.Println("\nSet HUBCHAT_DATABASE_URL to see which have been applied.")
			return nil
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		pool, err := database.Open(ctx, cfg.Database)
		if err != nil {
			return err
		}
		defer pool.Close()

		applied, err := database.AppliedMigrations(ctx, pool)
		if err != nil {
			return err
		}

		for _, name := range files {
			mark := "pending"
			if applied[name] {
				mark = "applied"
			}
			fmt.Printf("  [%-7s] %s\n", mark, name)
		}
		return nil
	}

	if cfg.Database.URL == "" {
		return errors.New("HUBCHAT_DATABASE_URL is required to run migrations")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool, err := database.Open(ctx, cfg.Database)
	if err != nil {
		return err
	}
	defer pool.Close()

	applied, err := database.Migrate(ctx, pool, migrations)
	if err != nil {
		return err
	}

	if len(applied) == 0 {
		fmt.Println("Already up to date.")
		return nil
	}

	fmt.Printf("Applied %d migration(s):\n", len(applied))
	for _, name := range applied {
		fmt.Printf("  %s\n", name)
	}
	return nil
}

type doctorCheck struct {
	Name     string `json:"name"`
	OK       bool   `json:"ok"`
	Required bool   `json:"required"`
	Detail   string `json:"detail,omitempty"`
}

// doctor performs safe, read-mostly installation checks. The local storage
// probe creates the configured directory when it is missing because that is
// exactly what the first upload would do; it writes only a short temporary
// marker and removes it immediately. No customer data, credentials, or
// message content is included in either output format.
func doctor(args []string) error {
	jsonOutput := false
	for _, arg := range args {
		switch arg {
		case "--json":
			jsonOutput = true
		default:
			return fmt.Errorf("doctor: unknown argument %q (use --json)", arg)
		}
	}

	checks := make([]doctorCheck, 0, 16)
	add := func(name string, ok, required bool, detail string) {
		checks = append(checks, doctorCheck{Name: name, OK: ok, Required: required, Detail: detail})
	}

	cfg, loadErr := config.Load()
	add("configuration loads", loadErr == nil, true, errText(loadErr))

	var validateErr error
	if loadErr == nil {
		validateErr = cfg.Validate()
	} else {
		validateErr = loadErr
	}
	add("configuration is valid", validateErr == nil, true, errText(validateErr))

	add("public URL configured", cfg.Server.PublicURL != nil, true,
		"used for cookies, widget origins, and email links")
	add("secret key present", len(cfg.Security.SecretKey) >= 32, true,
		"required to decrypt stored integration secrets")

	migrations, migrationsErr := listMigrations()
	add("migrations embedded", migrationsErr == nil, true,
		fmt.Sprintf("%d files", len(migrations)))

	_, dashboardErr := embedded.Dashboard()
	add("dashboard bundle embedded", dashboardErr == nil, true, errText(dashboardErr))

	_, portalErr := embedded.Portal()
	add("portal bundle embedded", portalErr == nil, true, errText(portalErr))

	_, widgetErr := embedded.Widget()
	add("widget bundle embedded", widgetErr == nil, true, errText(widgetErr))

	var pool *database.Pool
	if cfg.Database.URL == "" {
		add("database reachable", false, true, "HUBCHAT_DATABASE_URL is not configured")
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		pool, loadErr = database.Open(ctx, cfg.Database)
		cancel()
		if loadErr != nil {
			add("database reachable", false, true, loadErr.Error())
		} else {
			add("database reachable", true, true, "PostgreSQL responded to a health check")
		}
	}
	if pool != nil {
		defer pool.Close()
		applied, err := database.AppliedMigrations(context.Background(), pool)
		if err != nil {
			add("database migrations current", false, true, err.Error())
		} else {
			pending := 0
			for _, name := range migrations {
				if !applied[name] {
					pending++
				}
			}
			add("database migrations current", pending == 0, true,
				fmt.Sprintf("%d pending", pending))
		}
	}

	storageSupported := cfg.Storage.Backend == "local" || cfg.Storage.Backend == "s3"
	add("file storage backend supported", storageSupported, true,
		"supported backends: local, s3-compatible")
	if cfg.Storage.Backend == "local" {
		storageErr := probeLocalStorage(cfg.Storage.LocalPath)
		add("local file storage writable", storageErr == nil, true, errText(storageErr))
	} else if cfg.Storage.Backend == "s3" {
		_, storageErr := filemodule.NewS3Store(cfg.Storage.S3Endpoint, cfg.Storage.S3Region, cfg.Storage.S3Bucket, cfg.Storage.S3AccessKey, cfg.Storage.S3SecretKey, cfg.Storage.S3PathStyle, cfg.Storage.MaxFileBytes, cfg.Storage.AllowedMimeTypes)
		add("S3 storage configuration valid", storageErr == nil, true, errText(storageErr))
	}

	if cfg.Email.Enabled {
		configured := mailer.New(cfg.Email, slog.Default()).Configured()
		add("outbound email configured", configured, true,
			"SMTP host, sender, and enabled flag are required")
	} else {
		add("outbound email configured", true, false,
			"optional — customer notifications will queue without SMTP")
	}

	failed := 0
	for _, check := range checks {
		if check.Required && !check.OK {
			failed++
		}
	}
	if jsonOutput {
		if err := json.NewEncoder(os.Stdout).Encode(map[string]any{
			"ok":       failed == 0,
			"checks":   checks,
			"failures": failed,
		}); err != nil {
			return err
		}
	} else {
		fmt.Println("hubchat doctor")
		fmt.Println("──────────────")
		for _, check := range checks {
			report(check.Name, check.OK, check.Detail)
		}
	}

	if failed > 0 {
		return errors.New("doctor found problems that prevent startup")
	}
	return nil
}

func probeLocalStorage(path string) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("storage path is empty")
	}
	if err := os.MkdirAll(path, 0o750); err != nil {
		return fmt.Errorf("create storage directory: %w", err)
	}
	marker, err := os.CreateTemp(path, ".hubchat-doctor-")
	if err != nil {
		return fmt.Errorf("create storage probe: %w", err)
	}
	name := marker.Name()
	defer os.Remove(name)
	if _, err := marker.WriteString("hubchat storage probe\n"); err != nil {
		_ = marker.Close()
		return fmt.Errorf("write storage probe: %w", err)
	}
	if err := marker.Sync(); err != nil {
		_ = marker.Close()
		return fmt.Errorf("sync storage probe: %w", err)
	}
	if err := marker.Close(); err != nil {
		return fmt.Errorf("close storage probe: %w", err)
	}
	return nil
}

func configCheck(args []string) error {
	if len(args) > 0 && args[0] != "check" {
		return fmt.Errorf("unknown config subcommand %q", args[0])
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if err := cfg.Validate(); err != nil {
		return err
	}

	// Redacted: this output is routinely pasted into issue reports (§11.5).
	redacted := cfg.Redacted()
	fmt.Printf("listen:      %s\n", redacted.Server.Listen)
	if redacted.Server.PublicURL != nil {
		fmt.Printf("public url:  %s\n", redacted.Server.PublicURL)
	}
	fmt.Printf("roles:       %v\n", redacted.Server.Roles)
	fmt.Printf("database:    %s\n", redacted.Database.URL)
	fmt.Printf("migrations:  %s\n", redacted.Database.MigratePolicy)
	fmt.Printf("storage:     %s (%s)\n", redacted.Storage.Backend, redacted.Storage.LocalPath)
	fmt.Printf("email:       %t\n", redacted.Email.Enabled)
	fmt.Printf("realtime:    %t\n", redacted.Realtime.Enabled)
	fmt.Println("\nConfiguration is valid.")
	return nil
}

// setupCommand provisions the first owner and workspace without requiring a
// browser. It is intentionally first-run only: adding another owner belongs
// to the authenticated admin surface, not a command that might be run from a
// copied shell history.
func setupCommand(args []string) error {
	var name, email, password, slug string
	jsonOutput := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--name", "--email", "--password", "--slug":
			if i+1 >= len(args) {
				return fmt.Errorf("setup: %s requires a value", args[i])
			}
			value := args[i+1]
			i++
			switch args[i-1] {
			case "--name":
				name = value
			case "--email":
				email = value
			case "--password":
				password = value
			case "--slug":
				slug = value
			}
		case "--json":
			jsonOutput = true
		default:
			return fmt.Errorf("setup: unknown argument %q", args[i])
		}
	}
	if name == "" {
		name = os.Getenv("HUBCHAT_SETUP_NAME")
	}
	if email == "" {
		email = os.Getenv("HUBCHAT_SETUP_EMAIL")
	}
	if password == "" {
		password = os.Getenv("HUBCHAT_SETUP_PASSWORD")
	}
	if slug == "" {
		slug = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(name), " ", "-"))
	}
	if name == "" || email == "" || password == "" {
		return errors.New("setup requires --name, --email, and --password (or HUBCHAT_SETUP_* environment variables)")
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if cfg.Database.URL == "" {
		return errors.New("HUBCHAT_DATABASE_URL is required for setup")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	p, err := database.Open(ctx, cfg.Database)
	if err != nil {
		return err
	}
	defer p.Close()
	if migrations, migrationErr := embedded.Migrations(); migrationErr == nil {
		if _, err := database.Migrate(ctx, p, migrations); err != nil {
			return fmt.Errorf("setup migrations: %w", err)
		}
	} else {
		return migrationErr
	}
	var users int
	if err := p.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&users); err != nil {
		return err
	}
	if users > 0 {
		return errors.New("setup is only available on an empty database; use the dashboard or admin workflow")
	}
	userService := auth.New(p, auth.Options{SessionLifetime: cfg.Security.SessionLifetime, CookieDomain: cfg.Security.CookieDomain, CookieSecure: cfg.Security.CookieSecure, LoginAttempts: cfg.Security.LoginAttempts, LockoutWindow: cfg.Security.LockoutWindow})
	user, err := userService.SignUp(ctx, name, email, password)
	if err != nil {
		return err
	}
	workspaceService := workspace.New(p, events.New(p), audit.New(p))
	ws, err := workspaceService.Bootstrap(ctx, user.ID, name+"'s workspace", slug)
	if err != nil {
		return err
	}
	if jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"user_id": user.ID, "workspace_id": ws.ID, "workspace_slug": ws.Slug})
	}
	fmt.Printf("Setup complete.\nOwner: %s\nWorkspace: %s (%s)\n", user.Email, ws.Name, ws.Slug)
	return nil
}

func adminCommand(args []string) error {
	subcommand := "list"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		subcommand, args = args[0], args[1:]
	}
	var name, email, password, workspaceID, workspaceName, workspaceSlug, role string
	jsonOutput := false
	for i := 0; i < len(args); i++ {
		value := func(flag string) (string, error) {
			if i+1 >= len(args) {
				return "", fmt.Errorf("admin: %s requires a value", flag)
			}
			i++
			return args[i], nil
		}
		switch args[i] {
		case "--name":
			var err error
			name, err = value("--name")
			if err != nil {
				return err
			}
		case "--email":
			var err error
			email, err = value("--email")
			if err != nil {
				return err
			}
		case "--password":
			var err error
			password, err = value("--password")
			if err != nil {
				return err
			}
		case "--workspace":
			var err error
			workspaceID, err = value("--workspace")
			if err != nil {
				return err
			}
		case "--workspace-name":
			var err error
			workspaceName, err = value("--workspace-name")
			if err != nil {
				return err
			}
		case "--workspace-slug":
			var err error
			workspaceSlug, err = value("--workspace-slug")
			if err != nil {
				return err
			}
		case "--role":
			var err error
			role, err = value("--role")
			if err != nil {
				return err
			}
		case "--json":
			jsonOutput = true
		default:
			return fmt.Errorf("admin: unknown argument %q", args[i])
		}
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if cfg.Database.URL == "" {
		return errors.New("HUBCHAT_DATABASE_URL is required for admin commands")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	p, err := database.Open(ctx, cfg.Database)
	if err != nil {
		return err
	}
	defer p.Close()

	switch subcommand {
	case "list":
		rows, err := p.Query(ctx, `
			SELECT u.id,u.name,u.email::text,coalesce(json_agg(json_build_object('workspace_id',w.id,'workspace',w.name,'role',m.role) ORDER BY w.name) FILTER (WHERE w.id IS NOT NULL),'[]'::json)
			FROM users u
			LEFT JOIN workspace_members m ON m.user_id=u.id
			LEFT JOIN workspaces w ON w.id=m.workspace_id
			GROUP BY u.id,u.name,u.email ORDER BY u.created_at,u.id
		`)
		if err != nil {
			return err
		}
		defer rows.Close()
		type account struct {
			ID         string          `json:"id"`
			Name       string          `json:"name"`
			Email      string          `json:"email"`
			Workspaces json.RawMessage `json:"workspaces"`
		}
		accounts := make([]account, 0)
		for rows.Next() {
			var item account
			if err := rows.Scan(&item.ID, &item.Name, &item.Email, &item.Workspaces); err != nil {
				return err
			}
			accounts = append(accounts, item)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if jsonOutput {
			return json.NewEncoder(os.Stdout).Encode(accounts)
		}
		for _, item := range accounts {
			fmt.Printf("%-28s %-32s %s\n", item.ID, item.Email, item.Name)
		}
		return nil

	case "create":
		if name == "" || email == "" || password == "" {
			return errors.New("admin create requires --name, --email, and --password")
		}
		if workspaceID != "" && (workspaceName != "" || workspaceSlug != "") {
			return errors.New("admin create: use either --workspace or --workspace-name/--workspace-slug")
		}
		if workspaceID != "" && role == "" {
			role = "admin"
		}
		if role != "" && role != "owner" && role != "admin" && role != "manager" && role != "agent" && role != "developer" && role != "analyst" {
			return fmt.Errorf("admin create: unsupported role %q", role)
		}
		// Validate the target before creating the account. A failed membership
		// lookup or an incomplete workspace selector must never leave an orphan
		// user behind as a side effect of a rejected command.
		if workspaceID != "" {
			var exists bool
			if err := p.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM workspaces WHERE id=$1)`, workspaceID).Scan(&exists); err != nil {
				return err
			}
			if !exists {
				return fmt.Errorf("admin create: workspace %q was not found", workspaceID)
			}
			if role == "owner" {
				var owner bool
				if err := p.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM workspace_members WHERE workspace_id=$1 AND role='owner')`, workspaceID).Scan(&owner); err != nil {
					return err
				}
				if owner {
					return errors.New("admin create: workspace already has an owner")
				}
			}
		} else if workspaceName != "" || workspaceSlug != "" {
			if workspaceName == "" || workspaceSlug == "" {
				return errors.New("admin create: --workspace-name and --workspace-slug must be supplied together")
			}
		} else {
			return errors.New("admin create: provide --workspace WORKSPACE_ID or --workspace-name and --workspace-slug")
		}
		userService := auth.New(p, auth.Options{SessionLifetime: cfg.Security.SessionLifetime, CookieDomain: cfg.Security.CookieDomain, CookieSecure: cfg.Security.CookieSecure, LoginAttempts: cfg.Security.LoginAttempts, LockoutWindow: cfg.Security.LockoutWindow})
		user, err := userService.SignUp(ctx, name, email, password)
		if err != nil {
			return err
		}
		var createdWorkspace *workspace.Workspace
		if workspaceID != "" {
			if _, err := p.Exec(ctx, `INSERT INTO workspace_members(id,workspace_id,user_id,role) VALUES($1,$2,$3,$4)`, ids.New(ids.PrefixMember), workspaceID, user.ID, role); err != nil {
				_, _ = p.Exec(ctx, `DELETE FROM users WHERE id=$1`, user.ID)
				return err
			}
		} else if workspaceName != "" || workspaceSlug != "" {
			workspaceService := workspace.New(p, events.New(p), audit.New(p))
			createdWorkspace, err = workspaceService.Bootstrap(ctx, user.ID, workspaceName, workspaceSlug)
			if err != nil {
				_, _ = p.Exec(ctx, `DELETE FROM users WHERE id=$1`, user.ID)
				return err
			}
		}
		result := map[string]any{"user_id": user.ID, "email": user.Email, "workspace_id": workspaceID, "role": role}
		if createdWorkspace != nil {
			result["workspace_id"] = createdWorkspace.ID
			result["workspace_slug"] = createdWorkspace.Slug
			result["role"] = "owner"
		}
		if jsonOutput {
			return json.NewEncoder(os.Stdout).Encode(result)
		}
		fmt.Printf("Admin account created: %s\n", user.Email)
		if result["workspace_id"] != "" {
			fmt.Printf("Workspace membership: %s (%s)\n", result["workspace_id"], result["role"])
		}
		return nil

	case "reset-password":
		if email == "" || password == "" {
			return errors.New("admin reset-password requires --email EMAIL and --password PASSWORD")
		}
		userService := auth.New(p, auth.Options{SessionLifetime: cfg.Security.SessionLifetime, CookieDomain: cfg.Security.CookieDomain, CookieSecure: cfg.Security.CookieSecure, LoginAttempts: cfg.Security.LoginAttempts, LockoutWindow: cfg.Security.LockoutWindow})
		user, err := userService.ResetPasswordForAdmin(ctx, email, password)
		if err != nil {
			return err
		}
		result := map[string]any{"action": "reset-password", "user_id": user.ID, "email": user.Email, "sessions_revoked": true}
		if jsonOutput {
			return json.NewEncoder(os.Stdout).Encode(result)
		}
		fmt.Printf("Password reset for %s. All existing sessions were revoked.\n", user.Email)
		return nil
	default:
		return fmt.Errorf("unknown admin subcommand %q (use create, reset-password, or list)", subcommand)
	}
}

func jobsCommand(args []string) error {
	subcommand := "list"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		subcommand, args = args[0], args[1:]
	}
	deadLetterAction := "list"
	if subcommand == "dead-letter" && len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		deadLetterAction, args = args[0], args[1:]
	}

	var workspaceID, state, queue, jobID string
	jsonOutput := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--workspace":
			if i+1 >= len(args) {
				return errors.New("jobs: --workspace requires a value")
			}
			workspaceID, i = args[i+1], i+1
		case "--state":
			if i+1 >= len(args) {
				return errors.New("jobs: --state requires a value")
			}
			state, i = args[i+1], i+1
		case "--queue":
			if i+1 >= len(args) {
				return errors.New("jobs: --queue requires a value")
			}
			queue, i = args[i+1], i+1
		case "--json":
			jsonOutput = true
		default:
			if (subcommand == "retry" || subcommand == "cancel" || (subcommand == "dead-letter" && deadLetterAction == "retry")) && jobID == "" && !strings.HasPrefix(args[i], "-") {
				jobID = args[i]
				continue
			}
			return fmt.Errorf("jobs: unknown argument %q", args[i])
		}
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if cfg.Database.URL == "" {
		return errors.New("HUBCHAT_DATABASE_URL is required for job administration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := database.Open(ctx, cfg.Database)
	if err != nil {
		return err
	}
	defer pool.Close()
	client := jobs.NewClient(pool)

	switch subcommand {
	case "list":
		items, err := client.List(ctx, jobs.ListFilter{WorkspaceID: workspaceID, State: jobs.State(state), Queue: queue})
		if err != nil {
			return err
		}
		if jsonOutput {
			return json.NewEncoder(os.Stdout).Encode(items)
		}
		for _, item := range items {
			fmt.Printf("%-28s %-10s %-24s %-12s attempt=%d/%d scheduled=%s\n",
				item.ID, item.State, item.Type, item.Queue, item.Attempt, item.MaxAttempts,
				item.ScheduledAt.UTC().Format(time.RFC3339))
			if item.LastError != "" {
				fmt.Printf("  error: %s\n", item.LastError)
			}
		}
		return nil
	case "retry":
		if workspaceID == "" || jobID == "" {
			return errors.New("jobs retry requires --workspace WORKSPACE_ID JOB_ID")
		}
		if err := client.Retry(ctx, workspaceID, jobID); err != nil {
			return err
		}
		if jsonOutput {
			return json.NewEncoder(os.Stdout).Encode(map[string]any{"action": "retry", "job_id": jobID, "workspace_id": workspaceID})
		}
		fmt.Printf("Retried %s.\n", jobID)
		return nil
	case "cancel":
		if workspaceID == "" || jobID == "" {
			return errors.New("jobs cancel requires --workspace WORKSPACE_ID JOB_ID")
		}
		if err := client.Cancel(ctx, workspaceID, jobID); err != nil {
			return err
		}
		if jsonOutput {
			return json.NewEncoder(os.Stdout).Encode(map[string]any{"action": "cancel", "job_id": jobID, "workspace_id": workspaceID})
		}
		fmt.Printf("Cancelled %s.\n", jobID)
		return nil
	case "dead-letter":
		switch deadLetterAction {
		case "list":
			items, err := client.List(ctx, jobs.ListFilter{WorkspaceID: workspaceID, State: jobs.StateDead, Queue: queue})
			if err != nil {
				return err
			}
			if jsonOutput {
				return json.NewEncoder(os.Stdout).Encode(items)
			}
			for _, item := range items {
				fmt.Printf("%-28s %-24s %-12s attempt=%d/%d scheduled=%s\n",
					item.ID, item.Type, item.Queue, item.Attempt, item.MaxAttempts,
					item.ScheduledAt.UTC().Format(time.RFC3339))
				if item.LastError != "" {
					fmt.Printf("  error: %s\n", item.LastError)
				}
			}
			return nil
		case "retry":
			if workspaceID == "" || jobID == "" {
				return errors.New("jobs dead-letter retry requires --workspace WORKSPACE_ID JOB_ID")
			}
			if err := client.Retry(ctx, workspaceID, jobID); err != nil {
				return err
			}
			if jsonOutput {
				return json.NewEncoder(os.Stdout).Encode(map[string]any{"action": "retry", "job_id": jobID, "workspace_id": workspaceID, "source": "dead-letter"})
			}
			fmt.Printf("Retried dead-letter job %s.\n", jobID)
			return nil
		default:
			return fmt.Errorf("unknown jobs dead-letter subcommand %q (use list or retry)", deadLetterAction)
		}
	default:
		return fmt.Errorf("unknown jobs subcommand %q (use list, cancel, retry, or dead-letter)", subcommand)
	}
}

func workspaceCommand(args []string) error {
	subcommand := "export"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		subcommand, args = args[0], args[1:]
	}
	var workspaceID, slug, output, input string
	jsonOutput, dryRun := false, false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--workspace":
			if i+1 >= len(args) {
				return errors.New("workspace: --workspace requires a value")
			}
			i++
			workspaceID = args[i]
		case "--slug":
			if i+1 >= len(args) {
				return errors.New("workspace: --slug requires a value")
			}
			i++
			slug = args[i]
		case "--out":
			if i+1 >= len(args) {
				return errors.New("workspace: --out requires a value")
			}
			i++
			output = args[i]
		case "--file":
			if i+1 >= len(args) {
				return errors.New("workspace: --file requires a value")
			}
			i++
			input = args[i]
		case "--json":
			jsonOutput = true
		case "--dry-run":
			dryRun = true
		default:
			return fmt.Errorf("workspace: unknown argument %q", args[i])
		}
	}
	// Verification is deliberately offline: an operator should be able to
	// validate a downloaded archive before configuring or connecting to the
	// target installation.
	if subcommand == "verify" {
		if input == "" {
			return errors.New("workspace verify requires --file FILE")
		}
		_, verification, err := verifyWorkspaceArchive(input)
		if err != nil {
			return err
		}
		return writeArchiveVerification(jsonOutput, input, verification)
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if cfg.Database.URL == "" {
		return errors.New("HUBCHAT_DATABASE_URL is required for workspace archives")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	p, err := database.Open(ctx, cfg.Database)
	if err != nil {
		return err
	}
	defer p.Close()
	if workspaceID == "" && slug != "" {
		workspaceID, err = resolveWorkspaceID(ctx, p, slug)
		if err != nil {
			return err
		}
	}
	switch subcommand {
	case "export":
		if workspaceID == "" {
			return errors.New("workspace export requires --workspace ID or --slug SLUG")
		}
		if output == "" {
			return errors.New("workspace export requires --out FILE (use .json.gz)")
		}
		archive, summaries, err := portability.Export(ctx, p, workspaceID, time.Now().UTC())
		if err != nil {
			return err
		}
		file, err := os.OpenFile(output, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			return err
		}
		gzipWriter := gzip.NewWriter(file)
		encodeErr := json.NewEncoder(gzipWriter).Encode(archive)
		closeErr := gzipWriter.Close()
		fileErr := file.Close()
		if encodeErr != nil {
			return encodeErr
		}
		if closeErr != nil {
			return closeErr
		}
		if fileErr != nil {
			return fileErr
		}
		if _, _, err := verifyWorkspaceArchive(output); err != nil {
			return fmt.Errorf("workspace export: verify generated archive: %w", err)
		}
		return writeArchiveSummary(jsonOutput, "exported", output, summaries)
	case "import":
		if input == "" {
			return errors.New("workspace import requires --file FILE")
		}
		if workspaceID == "" {
			return errors.New("workspace import requires --workspace ID or --slug SLUG")
		}
		archive, err := readArchive(input)
		if err != nil {
			return err
		}
		summaries, err := portability.Import(ctx, p, archive, workspaceID, dryRun)
		if err != nil {
			return err
		}
		label := "imported"
		if dryRun {
			label = "validated"
		}
		return writeArchiveSummary(jsonOutput, label, input, summaries)
	default:
		return fmt.Errorf("unknown workspace subcommand %q (use export, import, or verify)", subcommand)
	}
}

func resolveWorkspaceID(ctx context.Context, pool *database.Pool, slug string) (string, error) {
	var id string
	if err := pool.QueryRow(ctx, `SELECT id FROM workspaces WHERE slug=$1`, strings.ToLower(strings.TrimSpace(slug))).Scan(&id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", fmt.Errorf("workspace %q was not found", slug)
		}
		return "", err
	}
	return id, nil
}

type archiveVerification struct {
	Action            string                     `json:"action"`
	Path              string                     `json:"path"`
	SizeBytes         int64                      `json:"size_bytes"`
	Checksum          string                     `json:"checksum"`
	Version           int                        `json:"version"`
	SourceWorkspaceID string                     `json:"source_workspace_id"`
	ExportedAt        time.Time                  `json:"exported_at"`
	RowCount          int                        `json:"row_count"`
	AttachmentCount   int                        `json:"attachment_count"`
	AttachmentBytes   int64                      `json:"attachment_bytes"`
	Tables            []portability.TableSummary `json:"tables"`
}

func verifyWorkspaceArchive(path string) (*portability.Archive, *archiveVerification, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		return nil, nil, err
	}
	if stat.IsDir() || stat.Size() <= 0 {
		return nil, nil, errors.New("workspace archive: file is empty or not a regular file")
	}
	if stat.Size() > portability.MaxArchiveBytes {
		return nil, nil, fmt.Errorf("workspace archive: compressed file exceeds the %d MiB limit", portability.MaxArchiveBytes/(1<<20))
	}
	compressed, err := io.ReadAll(io.LimitReader(file, portability.MaxArchiveBytes+1))
	if err != nil {
		return nil, nil, fmt.Errorf("workspace archive: read file: %w", err)
	}
	if int64(len(compressed)) > portability.MaxArchiveBytes {
		return nil, nil, fmt.Errorf("workspace archive: compressed file exceeds the %d MiB limit", portability.MaxArchiveBytes/(1<<20))
	}

	gzipReader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return nil, nil, fmt.Errorf("workspace archive: open gzip: %w", err)
	}
	gzipReader.Multistream(false)
	limited := &io.LimitedReader{R: gzipReader, N: portability.MaxArchiveBytes + 1}
	var archive portability.Archive
	decoder := json.NewDecoder(limited)
	if err := decoder.Decode(&archive); err != nil {
		_ = gzipReader.Close()
		return nil, nil, fmt.Errorf("workspace archive: decode JSON: %w", err)
	}
	var trailing any
	trailingErr := decoder.Decode(&trailing)
	closeErr := gzipReader.Close()
	if limited.N <= 0 {
		return nil, nil, fmt.Errorf("workspace archive: decompressed content exceeds the %d MiB limit", portability.MaxArchiveBytes/(1<<20))
	}
	if trailingErr != io.EOF {
		if trailingErr == nil {
			return nil, nil, errors.New("workspace archive: trailing JSON data is not allowed")
		}
		return nil, nil, fmt.Errorf("workspace archive: invalid trailing JSON: %w", trailingErr)
	}
	if closeErr != nil {
		return nil, nil, fmt.Errorf("workspace archive: close gzip: %w", closeErr)
	}
	inspection, err := portability.ValidateArchive(&archive)
	if err != nil {
		return nil, nil, err
	}
	sum := sha256.Sum256(compressed)
	return &archive, &archiveVerification{
		Action:            "verified",
		Path:              path,
		SizeBytes:         int64(len(compressed)),
		Checksum:          filemodule.ChecksumHex(sum[:]),
		Version:           inspection.Version,
		SourceWorkspaceID: inspection.SourceWorkspaceID,
		ExportedAt:        inspection.ExportedAt,
		RowCount:          inspection.RowCount,
		AttachmentCount:   inspection.AttachmentCount,
		AttachmentBytes:   inspection.AttachmentBytes,
		Tables:            inspection.Tables,
	}, nil
}

func readArchive(path string) (*portability.Archive, error) {
	archive, _, err := verifyWorkspaceArchive(path)
	if err != nil {
		return nil, fmt.Errorf("workspace import: invalid archive: %w", err)
	}
	return archive, nil
}

func writeArchiveVerification(jsonOutput bool, path string, verification *archiveVerification) error {
	if jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(verification)
	}
	fmt.Printf("Workspace archive verified: %s\nSHA-256: %s\n%d rows across %d tables; %d attachments (%d bytes).\n", path, verification.Checksum, verification.RowCount, len(verification.Tables), verification.AttachmentCount, verification.AttachmentBytes)
	return nil
}

func writeArchiveSummary(jsonOutput bool, action, path string, summaries []portability.TableSummary) error {
	rows := 0
	for _, summary := range summaries {
		rows += summary.Rows
	}
	if jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"action": action, "path": path, "tables": summaries, "rows": rows})
	}
	fmt.Printf("Workspace archive %s: %s\n%d rows across %d tables.\n", action, path, rows, len(summaries))
	return nil
}

func listMigrations() ([]string, error) {
	migrations, err := embedded.Migrations()
	if err != nil {
		return nil, err
	}

	entries, err := readDirNames(migrations)
	if err != nil {
		return nil, err
	}
	return entries, nil
}

func report(label string, ok bool, detail string) {
	mark := "✗"
	if ok {
		mark = "✓"
	}
	if detail != "" {
		fmt.Printf("  %s  %-32s %s\n", mark, label, detail)
		return
	}
	fmt.Printf("  %s  %s\n", mark, label)
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func newLogger(cfg config.Config) *slog.Logger {
	level := slog.LevelInfo
	switch cfg.Observability.LogLevel {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}

	options := &slog.HandlerOptions{Level: level}

	if cfg.Observability.LogFormat == "json" {
		return slog.New(slog.NewJSONHandler(os.Stdout, options))
	}
	return slog.New(slog.NewTextHandler(os.Stdout, options))
}

func usage() {
	fmt.Print(`hubchat — open-source customer support, in one binary

Usage:
  hubchat <command> [flags]

Commands:
  serve              Run the server (default)
  migrate            Apply pending database migrations
  migrate status     List migrations embedded in this binary
  setup              First-run installation
  doctor             Check configuration, assets, and dependencies [--json]
  config check       Validate and print the resolved configuration
  admin              Manage owner accounts
  workspace          Export or import a workspace archive
  jobs               Inspect, cancel, retry, and manage background jobs
  version            Print version information

Setup flags:
  hubchat setup --name NAME --email EMAIL --password PASSWORD [--slug SLUG] [--json]

Admin flags:
  hubchat admin list [--json]
  hubchat admin create --name NAME --email EMAIL --password PASSWORD \
    (--workspace ID [--role ROLE] | --workspace-name NAME --workspace-slug SLUG) [--json]
  hubchat admin reset-password --email EMAIL --password PASSWORD [--json]

Workspace flags:
  hubchat workspace export --workspace ID|--slug SLUG --out FILE.json.gz [--json]
  hubchat workspace import --workspace ID|--slug SLUG --file FILE [--dry-run] [--json]
  hubchat workspace verify --file FILE.json.gz [--json]

Job flags:
  hubchat jobs list [--workspace ID] [--state STATE] [--queue QUEUE] [--json]
  hubchat jobs cancel --workspace ID JOB_ID [--json]
  hubchat jobs retry --workspace ID JOB_ID [--json]
  hubchat jobs dead-letter [--workspace ID] [--queue QUEUE] [--json]
  hubchat jobs dead-letter retry --workspace ID JOB_ID [--json]

Environment:
  HUBCHAT_DATABASE_URL   PostgreSQL connection string          (required)
  HUBCHAT_PUBLIC_URL     How browsers reach this deployment    (required)
  HUBCHAT_SECRET_KEY     32+ bytes; signs cookies and tokens   (required)
  HUBCHAT_LISTEN         Bind address                          (default :8080)
  HUBCHAT_ROLES          http,realtime,worker,scheduler        (default all)
  HUBCHAT_MIGRATE        verify | apply | skip                 (default verify)
  HUBCHAT_DATA_DIR       Attachment storage path               (default ./data/files)
  HUBCHAT_DEV            Set to 1 for development mode

Documentation: https://github.com/hubchat/hubchat/tree/main/docs
`)
}
