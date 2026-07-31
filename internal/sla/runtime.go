package sla

// This file owns the live side of SLA. Policies and calendars are configuration;
// instances are the durable clocks that make a deadline explainable after a
// restart. All mutations are workspace-scoped and safe to repeat because the
// migration gives each subject/kind one timer.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/hubchat/hubchat/internal/database"
	"github.com/hubchat/hubchat/internal/events"
	"github.com/hubchat/hubchat/internal/ids"
	"github.com/jackc/pgx/v5"
)

const JobEvaluate = "sla.evaluate"

var errNoSLATarget = errors.New("sla: no matching target")

type Instance struct {
	ID             string     `json:"id"`
	WorkspaceID    string     `json:"workspace_id"`
	PolicyID       string     `json:"policy_id"`
	ConversationID *string    `json:"conversation_id,omitempty"`
	TicketID       *string    `json:"ticket_id,omitempty"`
	Kind           string     `json:"kind"`
	State          string     `json:"state"`
	TargetMinutes  int        `json:"target_minutes"`
	ElapsedMinutes int        `json:"elapsed_minutes"`
	DeadlineAt     *time.Time `json:"deadline_at,omitempty"`
	PausedAt       *time.Time `json:"paused_at,omitempty"`
	PausedReason   *string    `json:"paused_reason,omitempty"`
	StartedAt      time.Time  `json:"started_at"`
	SatisfiedAt    *time.Time `json:"satisfied_at,omitempty"`
	BreachedAt     *time.Time `json:"breached_at,omitempty"`
	WarnedAt       *time.Time `json:"warned_at,omitempty"`
	RunningSince   *time.Time `json:"running_since,omitempty"`
}

type runtimePolicy struct {
	pauseStates []string
	warning     int
	target      int
	calendar    *Calendar
}

// ListInstances is intentionally a bounded operational read. The dashboard
// can filter state and subject without ever receiving another workspace's
// timers.
func (s *Service) ListInstances(ctx context.Context, workspaceID, state string, limit int) ([]Instance, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id,workspace_id,policy_id,conversation_id,ticket_id,kind,state,
		       target_minutes,elapsed_minutes,deadline_at,paused_at,paused_reason,
		       started_at,satisfied_at,breached_at,warned_at,running_since
		FROM sla_instances
		WHERE workspace_id=$1 AND ($2='' OR state=$2)
		ORDER BY CASE WHEN state='breached' THEN 0 WHEN state='active' THEN 1 ELSE 2 END,
		         COALESCE(deadline_at, started_at), id
		LIMIT $3`, workspaceID, state, limit)
	if err != nil {
		return nil, fmt.Errorf("sla: list instances: %w", err)
	}
	defer rows.Close()
	result := make([]Instance, 0)
	for rows.Next() {
		item, err := scanInstance(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, *item)
	}
	return result, rows.Err()
}

func scanInstance(row interface{ Scan(...any) error }) (*Instance, error) {
	var item Instance
	if err := row.Scan(&item.ID, &item.WorkspaceID, &item.PolicyID, &item.ConversationID,
		&item.TicketID, &item.Kind, &item.State, &item.TargetMinutes,
		&item.ElapsedMinutes, &item.DeadlineAt, &item.PausedAt, &item.PausedReason,
		&item.StartedAt, &item.SatisfiedAt, &item.BreachedAt, &item.WarnedAt,
		&item.RunningSince); err != nil {
		return nil, err
	}
	return &item, nil
}

// EnsureConversation starts the first-response and resolution clocks for a
// conversation. It is safe to call from both the event consumer and a repair
// command after a deployment.
func (s *Service) EnsureConversation(ctx context.Context, workspaceID, conversationID string, now time.Time) error {
	return database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		var priority, state, policyID string
		var createdAt time.Time
		err := tx.QueryRow(ctx, `
			SELECT c.priority,c.state,COALESCE(i.sla_policy_id,
				(SELECT id FROM sla_policies WHERE workspace_id=$1 AND enabled ORDER BY created_at LIMIT 1)),c.created_at
			FROM conversations c JOIN inboxes i ON i.id=c.inbox_id
			WHERE c.workspace_id=$1 AND c.id=$2`, workspaceID, conversationID).
			Scan(&priority, &state, &policyID, &createdAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil || policyID == "" {
			return err
		}
		for _, timer := range []struct {
			kind string
		}{
			{kind: "first_response"},
			{kind: "resolution"},
		} {
			policy, err := s.loadRuntimePolicy(ctx, tx, workspaceID, policyID, priority, timer.kind)
			if errors.Is(err, errNoSLATarget) {
				continue
			}
			if err != nil {
				return err
			}
			if err := s.insertTimer(ctx, tx, workspaceID, policyID, conversationID, "", timer.kind, policy.target, createdAt, now, contains(policy.pauseStates, state), state, policy.calendar); err != nil {
				return err
			}
		}
		return nil
	})
}

// EnsureTicket starts the same clocks for a ticket. Ticket policies override
// inbox/default policies, matching the assignment semantics in the schema.
func (s *Service) EnsureTicket(ctx context.Context, workspaceID, ticketID string, now time.Time) error {
	return database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		var priority, state, policyID string
		var createdAt time.Time
		err := tx.QueryRow(ctx, `
			SELECT t.priority,t.status,COALESCE(t.sla_policy_id,i.sla_policy_id,
				(SELECT id FROM sla_policies WHERE workspace_id=$1 AND enabled ORDER BY created_at LIMIT 1)),t.created_at
			FROM tickets t LEFT JOIN inboxes i ON i.id=t.inbox_id
			WHERE t.workspace_id=$1 AND t.id=$2`, workspaceID, ticketID).
			Scan(&priority, &state, &policyID, &createdAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil || policyID == "" {
			return err
		}
		for _, timer := range []struct {
			kind string
		}{
			{kind: "first_response"},
			{kind: "resolution"},
		} {
			policy, err := s.loadRuntimePolicy(ctx, tx, workspaceID, policyID, priority, timer.kind)
			if errors.Is(err, errNoSLATarget) {
				continue
			}
			if err != nil {
				return err
			}
			if err := s.insertTimer(ctx, tx, workspaceID, policyID, "", ticketID, timer.kind, policy.target, createdAt, now, contains(policy.pauseStates, state), state, policy.calendar); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Service) insertTimer(ctx context.Context, tx pgx.Tx, workspaceID, policyID, conversationID, ticketID, kind string, target int, started, now time.Time, paused bool, reason string, calendar *Calendar) error {
	if started.IsZero() {
		started = now
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	var deadline any
	var running any = now
	state := "active"
	var pausedAt any
	var pausedReason any
	if paused {
		state, running, pausedAt, pausedReason = "paused", nil, now, reason
	} else {
		value, err := calendar.Add(started, time.Duration(target)*time.Minute)
		if err != nil {
			return err
		}
		deadline = value
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO sla_instances(id,workspace_id,policy_id,conversation_id,ticket_id,kind,state,
			target_minutes,deadline_at,paused_at,paused_reason,started_at,running_since)
		VALUES($1,$2,$3,NULLIF($4,''),NULLIF($5,''),$6,$7,$8,$9,$10,$11,$12,$13)
		ON CONFLICT DO NOTHING`, ids.New(ids.PrefixSLAInstance), workspaceID, policyID,
		conversationID, ticketID, kind, state, target, deadline, pausedAt, pausedReason, started, running)
	return err
}

func (s *Service) loadRuntimePolicy(ctx context.Context, tx pgx.Tx, workspaceID, policyID, priority, kind string) (*runtimePolicy, error) {
	var pauseStates []string
	var warning int
	var timezone string
	var weekly []byte
	var calendarID *string
	var target *int
	err := tx.QueryRow(ctx, `
		SELECT p.pause_states,p.warning_threshold_percent,c.id,COALESCE(c.timezone,'UTC'),COALESCE(c.weekly,'null'::jsonb),
			CASE WHEN $4='resolution' THEN t.resolution_minutes WHEN $4='next_response' THEN t.next_response_minutes ELSE t.first_response_minutes END
		FROM sla_policies p
		LEFT JOIN business_hour_calendars c ON c.id=p.calendar_id
		LEFT JOIN sla_policy_targets t ON t.policy_id=p.id AND t.priority=$3
		WHERE p.workspace_id=$1 AND p.id=$2 AND p.enabled`, workspaceID, policyID, priority, kind).
		Scan(&pauseStates, &warning, &calendarID, &timezone, &weekly, &target)
	if errors.Is(err, pgx.ErrNoRows) || target == nil {
		return nil, errNoSLATarget
	}
	if err != nil {
		return nil, err
	}
	calendar, err := s.calendarFromRow(ctx, tx, calendarID, timezone, weekly)
	if err != nil {
		return nil, err
	}
	return &runtimePolicy{pauseStates: pauseStates, warning: warning, target: *target, calendar: calendar}, nil
}

func (s *Service) calendarFromRow(ctx context.Context, tx pgx.Tx, id *string, timezone string, weekly []byte) (*Calendar, error) {
	if id == nil {
		return NewCalendar("UTC", fullDayWeekly(), nil)
	}
	var schedule [7][]Window
	if err := json.Unmarshal(weekly, &schedule); err != nil {
		return nil, fmt.Errorf("sla: decode calendar: %w", err)
	}
	rows, err := tx.Query(ctx, `SELECT date::text FROM calendar_holidays WHERE calendar_id=$1`, *id)
	if err != nil {
		return nil, err
	}
	var holidays []string
	for rows.Next() {
		var date string
		if err := rows.Scan(&date); err != nil {
			rows.Close()
			return nil, err
		}
		holidays = append(holidays, date)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	return NewCalendar(timezone, schedule, holidays)
}

func fullDayWeekly() [7][]Window {
	var weekly [7][]Window
	for i := range weekly {
		weekly[i] = []Window{{Start: "00:00", End: "24:00"}}
	}
	return weekly
}

// MessageReceived advances the response clocks. Customer messages start the
// next-response clock; agent messages satisfy the current response clocks.
func (s *Service) MessageReceived(ctx context.Context, workspaceID, conversationID, authorType string, now time.Time) error {
	if err := s.EnsureConversation(ctx, workspaceID, conversationID, now); err != nil {
		return err
	}
	return database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		if authorType == "customer" {
			var priority, state, policyID string
			if err := tx.QueryRow(ctx, `SELECT c.priority,c.state,COALESCE(i.sla_policy_id,(SELECT id FROM sla_policies WHERE workspace_id=$1 AND enabled ORDER BY created_at LIMIT 1)) FROM conversations c JOIN inboxes i ON i.id=c.inbox_id WHERE c.workspace_id=$1 AND c.id=$2`, workspaceID, conversationID).Scan(&priority, &state, &policyID); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return nil
				}
				return err
			}
			policy, err := s.loadRuntimePolicy(ctx, tx, workspaceID, policyID, priority, "next_response")
			if errors.Is(err, errNoSLATarget) {
				return nil
			}
			if err != nil {
				return err
			}
			paused := contains(policy.pauseStates, state)
			return s.startOrResetNext(ctx, tx, workspaceID, policyID, conversationID, policy.target, now, paused, state, policy.calendar)
		}
		_, err := tx.Exec(ctx, `UPDATE sla_instances SET state='met',satisfied_at=COALESCE(satisfied_at,$3),elapsed_minutes=target_minutes,running_since=NULL,deadline_at=NULL WHERE workspace_id=$1 AND conversation_id=$2 AND kind IN ('first_response','next_response') AND state IN ('active','paused')`, workspaceID, conversationID, now)
		return err
	})
}

func (s *Service) startOrResetNext(ctx context.Context, tx pgx.Tx, workspaceID, policyID, conversationID string, target int, now time.Time, paused bool, reason string, calendar *Calendar) error {
	var deadline any
	var running any = now
	state := "active"
	var pausedAt any
	var pausedReason any
	if paused {
		state, running, pausedAt, pausedReason = "paused", nil, now, reason
	} else {
		value, err := calendar.Add(now, time.Duration(target)*time.Minute)
		if err != nil {
			return err
		}
		deadline = value
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO sla_instances(id,workspace_id,policy_id,conversation_id,kind,state,target_minutes,deadline_at,paused_at,paused_reason,started_at,running_since)
		VALUES($1,$2,$3,$4,'next_response',$5,$6,$7,$8,$9,$10,$11)
		ON CONFLICT (conversation_id,kind) WHERE conversation_id IS NOT NULL DO UPDATE SET
			state=EXCLUDED.state,target_minutes=EXCLUDED.target_minutes,elapsed_minutes=0,
			deadline_at=EXCLUDED.deadline_at,paused_at=EXCLUDED.paused_at,paused_reason=EXCLUDED.paused_reason,
			satisfied_at=NULL,breached_at=NULL,warned_at=NULL,running_since=EXCLUDED.running_since`,
		ids.New(ids.PrefixSLAInstance), workspaceID, policyID, conversationID, state, target,
		deadline, pausedAt, pausedReason, now, running)
	return err
}

func (s *Service) SetSubjectState(ctx context.Context, workspaceID, subjectType, subjectID, state string, now time.Time) error {
	return database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		column := "conversation_id"
		if subjectType == "ticket" {
			column = "ticket_id"
		}
		rows, err := tx.Query(ctx, fmt.Sprintf(`SELECT id,policy_id,state,target_minutes,elapsed_minutes,running_since FROM sla_instances WHERE workspace_id=$1 AND %s=$2 AND state IN ('active','paused') FOR UPDATE`, column), workspaceID, subjectID)
		if err != nil {
			return err
		}
		type timerRow struct {
			id, policyID, state string
			target, elapsed     int
			runningSince        *time.Time
		}
		var timers []timerRow
		for rows.Next() {
			var item timerRow
			if err := rows.Scan(&item.id, &item.policyID, &item.state, &item.target, &item.elapsed, &item.runningSince); err != nil {
				rows.Close()
				return err
			}
			timers = append(timers, item)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		for _, timer := range timers {
			policy, err := s.loadPolicyForInstance(ctx, tx, workspaceID, timer.policyID, timer.target)
			if err != nil {
				return err
			}
			shouldPause := contains(policy.pauseStates, state)
			if state == "resolved" || state == "closed" {
				_, err = tx.Exec(ctx, `UPDATE sla_instances SET state='met',satisfied_at=COALESCE(satisfied_at,$2),elapsed_minutes=target_minutes,running_since=NULL,deadline_at=NULL,paused_at=NULL,paused_reason=NULL WHERE id=$1`, timer.id, now)
			} else if shouldPause && timer.state == "active" {
				elapsed := timer.elapsed
				if timer.runningSince != nil {
					elapsed += int(mustElapsed(policy.calendar, *timer.runningSince, now) / time.Minute)
					if elapsed > timer.target {
						elapsed = timer.target
					}
				}
				_, err = tx.Exec(ctx, `UPDATE sla_instances SET state='paused',elapsed_minutes=$2,running_since=NULL,deadline_at=NULL,paused_at=$3,paused_reason=$4 WHERE id=$1`, timer.id, elapsed, now, state)
			} else if !shouldPause && timer.state == "paused" {
				remaining := timer.target - timer.elapsed
				if remaining < 0 {
					remaining = 0
				}
				deadline, addErr := policy.calendar.Add(now, time.Duration(remaining)*time.Minute)
				if addErr != nil {
					return addErr
				}
				_, err = tx.Exec(ctx, `UPDATE sla_instances SET state='active',running_since=$2,deadline_at=$3,paused_at=NULL,paused_reason=NULL WHERE id=$1`, timer.id, now, deadline)
			}
			if err != nil {
				return err
			}
		}
		return nil
	})
}

// PauseSubject pauses active timers without changing the conversation or
// ticket state. Automation uses this for explicit maintenance/wait actions;
// elapsed time is calculated with the same business-hours calendar as the
// normal state-transition path.
func (s *Service) PauseSubject(ctx context.Context, workspaceID, subjectType, subjectID, reason string, now time.Time) error {
	if subjectType != "conversation" && subjectType != "ticket" {
		return errors.New("sla: subject must be a conversation or ticket")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	column := "conversation_id"
	if subjectType == "ticket" {
		column = "ticket_id"
	}
	return database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, fmt.Sprintf(`SELECT id,policy_id,target_minutes,elapsed_minutes,running_since FROM sla_instances WHERE workspace_id=$1 AND %s=$2 AND state='active' FOR UPDATE`, column), workspaceID, subjectID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id, policyID string
			var target, elapsed int
			var runningSince *time.Time
			if err := rows.Scan(&id, &policyID, &target, &elapsed, &runningSince); err != nil {
				return err
			}
			policy, err := s.loadPolicyForInstance(ctx, tx, workspaceID, policyID, target)
			if err != nil {
				return err
			}
			if runningSince != nil {
				elapsed += int(mustElapsed(policy.calendar, *runningSince, now) / time.Minute)
				if elapsed > target {
					elapsed = target
				}
			}
			if _, err := tx.Exec(ctx, `UPDATE sla_instances SET state='paused',elapsed_minutes=$2,running_since=NULL,deadline_at=NULL,paused_at=$3,paused_reason=NULLIF($4,'') WHERE id=$1`, id, elapsed, now, reason); err != nil {
				return err
			}
		}
		return rows.Err()
	})
}

func (s *Service) loadPolicyForInstance(ctx context.Context, tx pgx.Tx, workspaceID, policyID string, target int) (*runtimePolicy, error) {
	var pause []string
	var warning int
	var timezone string
	var weekly []byte
	var calendarID *string
	if err := tx.QueryRow(ctx, `SELECT p.pause_states,p.warning_threshold_percent,c.id,COALESCE(c.timezone,'UTC'),COALESCE(c.weekly,'null'::jsonb) FROM sla_policies p LEFT JOIN business_hour_calendars c ON c.id=p.calendar_id WHERE p.workspace_id=$1 AND p.id=$2`, workspaceID, policyID).Scan(&pause, &warning, &calendarID, &timezone, &weekly); err != nil {
		return nil, err
	}
	calendar, err := s.calendarFromRow(ctx, tx, calendarID, timezone, weekly)
	if err != nil {
		return nil, err
	}
	return &runtimePolicy{pauseStates: pause, warning: warning, target: target, calendar: calendar}, nil
}

func mustElapsed(calendar *Calendar, start, end time.Time) time.Duration {
	value, err := calendar.Elapsed(start, end)
	if err != nil {
		return 0
	}
	return value
}

// Evaluate advances running elapsed values, emits one approaching event, and
// transitions each due timer exactly once. It is safe for multiple workers:
// each instance is claimed by a row lock before it is changed.
func (s *Service) Evaluate(ctx context.Context, now time.Time) (warnings, breaches int, err error) {
	rows, err := s.pool.Query(ctx, `SELECT id FROM sla_instances WHERE state='active' AND (deadline_at <= $1 OR warned_at IS NULL) ORDER BY deadline_at NULLS LAST LIMIT 500`, now)
	if err != nil {
		return 0, 0, err
	}
	var idsToCheck []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, 0, err
		}
		idsToCheck = append(idsToCheck, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, 0, err
	}
	rows.Close()
	for _, id := range idsToCheck {
		warning, breach, processErr := s.evaluateOne(ctx, id, now)
		if processErr != nil {
			return warnings, breaches, processErr
		}
		if warning {
			warnings++
		}
		if breach {
			breaches++
		}
	}
	return warnings, breaches, nil
}

func (s *Service) evaluateOne(ctx context.Context, id string, now time.Time) (warning, breach bool, err error) {
	err = database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		item, scanErr := scanInstance(tx.QueryRow(ctx, `SELECT id,workspace_id,policy_id,conversation_id,ticket_id,kind,state,target_minutes,elapsed_minutes,deadline_at,paused_at,paused_reason,started_at,satisfied_at,breached_at,warned_at,running_since FROM sla_instances WHERE id=$1 AND state='active' FOR UPDATE`, id))
		if scanErr != nil {
			if errors.Is(scanErr, pgx.ErrNoRows) {
				return nil
			}
			return scanErr
		}
		policy, policyErr := s.loadPolicyForInstance(ctx, tx, item.WorkspaceID, item.PolicyID, item.TargetMinutes)
		if policyErr != nil {
			return policyErr
		}
		if item.RunningSince != nil {
			item.ElapsedMinutes += int(mustElapsed(policy.calendar, *item.RunningSince, now) / time.Minute)
		}
		if item.ElapsedMinutes > item.TargetMinutes {
			item.ElapsedMinutes = item.TargetMinutes
		}
		if item.WarnedAt == nil && item.ElapsedMinutes*100 >= item.TargetMinutes*policy.warning {
			if _, updateErr := tx.Exec(ctx, `UPDATE sla_instances SET warned_at=$2,elapsed_minutes=$3 WHERE id=$1`, item.ID, now, item.ElapsedMinutes); updateErr != nil {
				return updateErr
			}
			warning = true
			if err := s.appendRuntimeEvent(ctx, tx, *item, events.SLAApproaching, now); err != nil {
				return err
			}
		}
		if item.DeadlineAt != nil && !item.DeadlineAt.After(now) {
			if _, updateErr := tx.Exec(ctx, `UPDATE sla_instances SET state='breached',breached_at=$2,elapsed_minutes=$3,running_since=NULL WHERE id=$1`, item.ID, now, item.ElapsedMinutes); updateErr != nil {
				return updateErr
			}
			breach = true
			return s.appendRuntimeEvent(ctx, tx, *item, events.SLABreached, now)
		}
		_, err := tx.Exec(ctx, `UPDATE sla_instances SET elapsed_minutes=$2 WHERE id=$1 AND state='active'`, item.ID, item.ElapsedMinutes)
		return err
	})
	return warning, breach, err
}

func (s *Service) appendRuntimeEvent(ctx context.Context, tx pgx.Tx, item Instance, eventType events.Type, now time.Time) error {
	if s.events == nil {
		return nil
	}
	entityType, entityID := "conversation", deref(item.ConversationID)
	if item.TicketID != nil {
		entityType, entityID = "ticket", *item.TicketID
	}
	_, err := s.events.Append(ctx, tx, events.Event{WorkspaceID: item.WorkspaceID, Type: eventType, EntityType: entityType, EntityID: entityID, ActorType: events.ActorSystem, Data: map[string]any{"instance_id": item.ID, "kind": item.Kind, "target_minutes": item.TargetMinutes, "occurred_at": now.UTC().Format(time.RFC3339)}})
	return err
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
func contains(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}

// RunEventConsumer turns durable domain events into timer changes. The first
// signal starts at its own sequence, while later signals close every gap.
func (s *Service) RunEventConsumer(ctx context.Context, signals <-chan events.Signal, source interface {
	Since(context.Context, string, int64, int) ([]events.Record, error)
}) {
	for {
		select {
		case <-ctx.Done():
			return
		case signal, ok := <-signals:
			if !ok {
				return
			}
			s.seenMu.Lock()
			after, exists := s.seen[signal.WorkspaceID]
			if !exists {
				after = signal.Sequence - 1
			}
			s.seenMu.Unlock()
			for {
				records, err := source.Since(ctx, signal.WorkspaceID, after, 200)
				if err != nil {
					break
				}
				if len(records) == 0 {
					break
				}
				failed := false
				for _, record := range records {
					if err := s.processEvent(ctx, record); err != nil {
						failed = true
						break
					}
					after = record.Sequence
				}
				if failed {
					break
				}
				s.seenMu.Lock()
				s.seen[signal.WorkspaceID] = after
				s.seenMu.Unlock()
				if len(records) < 200 {
					break
				}
			}
		}
	}
}

func (s *Service) processEvent(ctx context.Context, record events.Record) error {
	var data struct {
		ConversationID string `json:"conversation_id"`
		AuthorType     string `json:"author_type"`
		To             string `json:"to"`
	}
	_ = json.Unmarshal(record.Data, &data)
	now := record.OccurredAt
	switch record.Type {
	case events.ConversationCreated:
		return s.EnsureConversation(ctx, record.WorkspaceID, record.EntityID, now)
	case events.TicketCreated:
		return s.EnsureTicket(ctx, record.WorkspaceID, record.EntityID, now)
	case events.MessageCreated:
		id := record.EntityID
		if data.ConversationID != "" {
			id = data.ConversationID
		}
		return s.MessageReceived(ctx, record.WorkspaceID, id, data.AuthorType, now)
	case events.ConversationStateSet, events.ConversationResolved:
		state := data.To
		if state == "" && record.Type == events.ConversationResolved {
			state = "resolved"
		}
		return s.SetSubjectState(ctx, record.WorkspaceID, "conversation", record.EntityID, state, now)
	case events.TicketStateSet:
		return s.SetSubjectState(ctx, record.WorkspaceID, "ticket", record.EntityID, data.To, now)
	default:
		return nil
	}
}
