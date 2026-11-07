package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const maxActivityEntries = 1000

var ActivityTypes = []string{"event", "ticket_type", "paid_sales", "payment", "refund", "registration",
	"staff", "campaign", "check_in", "support"}

type ActivityEntry struct {
	ID          string    `json:"id"`
	CreatedAt   time.Time `json:"createdAt"`
	ActorID     *string   `json:"actorId"`
	ActorName   *string   `json:"actorName"`
	Action      string    `json:"action"`
	Type        string    `json:"type"`
	EntityType  string    `json:"entityType"`
	EntityID    string    `json:"entityId"`
	Description string    `json:"description"`
}

type ActivityFilter struct {
	From *time.Time
	To   *time.Time
	Type string
}

func (p *Postgres) ListActivity(ctx context.Context, organizerID, eventID string, f ActivityFilter) ([]ActivityEntry, error) {
	if _, err := p.EventByOwner(ctx, organizerID, eventID); err != nil {
		return nil, err
	}
	rows, err := p.pool.Query(ctx,
		`SELECT a.id, a.created_at, a.actor_id, COALESCE(NULLIF(u.display_name, ''), u.email),
		 a.action, a.entity_type, a.entity_id, a.details
		 FROM audit_log a LEFT JOIN users u ON u.id = a.actor_id
		 WHERE a.event_id = $1
		 AND ($2::timestamptz IS NULL OR a.created_at >= $2)
		 AND ($3::timestamptz IS NULL OR a.created_at < $3)
		 AND ($4 = '' OR split_part(a.action, '.', 1) = $4)
		 ORDER BY a.created_at, a.id LIMIT $5`, eventID, f.From, f.To, f.Type, maxActivityEntries)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (ActivityEntry, error) {
		var a ActivityEntry
		var details map[string]any
		err := row.Scan(&a.ID, &a.CreatedAt, &a.ActorID, &a.ActorName, &a.Action, &a.EntityType, &a.EntityID, &details)
		a.Type, _, _ = strings.Cut(a.Action, ".")
		a.Description = describeActivity(a.Action, details)
		return a, err
	})
}

func change(d map[string]any, key, label string) string {
	pair, ok := d[key].([]any)
	if !ok || len(pair) != 2 {
		return ""
	}
	return fmt.Sprintf("; %s %v → %v", label, pair[0], pair[1])
}

func describeActivity(action string, d map[string]any) string {
	switch action {
	case "event.created":
		return "Event created"
	case "event.duplicated":
		return "Event created as a copy of another event"
	case "event.updated":
		return "Event details updated" + change(d, "capacity", "capacity")
	case "event.published":
		return "Event published"
	case "event.unpublished":
		return "Event moved back to draft"
	case "event.cancelled":
		return "Event cancelled"
	case "ticket_type.created":
		return fmt.Sprintf("Ticket type %q created at %v KZT, quantity %v", d["name"], d["priceAmount"], d["quantity"])
	case "ticket_type.updated":
		return fmt.Sprintf("Ticket type %q updated", d["name"]) + change(d, "priceAmount", "price") + change(d, "quantity", "quantity")
	case "ticket_type.deleted":
		return fmt.Sprintf("Ticket type %q deleted", d["name"])
	case "paid_sales.activated":
		return fmt.Sprintf("Paid sales activated (demo fee %v KZT)", d["feeAmount"])
	case "payment.succeeded":
		return fmt.Sprintf("Demo payment of %v KZT succeeded", d["amount"])
	case "payment.failed":
		return fmt.Sprintf("Demo payment of %v KZT failed", d["amount"])
	case "refund.succeeded":
		return fmt.Sprintf("Demo refund of %v KZT issued", d["amount"])
	case "registration.cancelled":
		return "Free registration cancelled"
	case "staff.assigned":
		return fmt.Sprintf("Staff member assigned as %v", d["role"])
	case "staff.removed":
		return "Staff member removed"
	case "campaign.created":
		return fmt.Sprintf("Campaign %q created", d["name"])
	case "campaign.updated":
		return fmt.Sprintf("Campaign %q updated", d["name"])
	case "campaign.enabled":
		return "Campaign enabled"
	case "campaign.disabled":
		return "Campaign disabled"
	case "check_in.recorded":
		return "Ticket checked in"
	case "check_in.reversed":
		return "Check-in reversed"
	case "support.status_changed":
		return fmt.Sprintf("Support case status changed to %v", d["status"])
	}
	return action
}
