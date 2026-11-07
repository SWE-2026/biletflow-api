package store

import (
	"context"
	"crypto/rand"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

var ErrCampaignTicketTypes = errors.New("campaign ticket types must belong to the event")

type CampaignStats struct {
	Redemptions    int   `json:"redemptions"`
	TicketsSold    int   `json:"ticketsSold"`
	GrossAmount    int64 `json:"grossAmount"`
	DiscountAmount int64 `json:"discountAmount"`
	NetAmount      int64 `json:"netAmount"`
}

type Campaign struct {
	ID             string        `json:"id"`
	EventID        string        `json:"eventId"`
	Name           string        `json:"name"`
	Code           string        `json:"code"`
	Token          string        `json:"token"`
	LinkPath       string        `json:"linkPath"`
	DiscountType   string        `json:"discountType"`
	DiscountValue  int64         `json:"discountValue"`
	StartsAt       *time.Time    `json:"startsAt"`
	EndsAt         *time.Time    `json:"endsAt"`
	MaxRedemptions *int          `json:"maxRedemptions"`
	Status         string        `json:"status"`
	TicketTypeIDs  []string      `json:"ticketTypeIds"`
	CreatedAt      time.Time     `json:"createdAt"`
	UpdatedAt      time.Time     `json:"updatedAt"`
	Stats          CampaignStats `json:"stats"`
}

type CampaignInput struct {
	Name           string     `json:"name"`
	DiscountType   string     `json:"discountType"`
	DiscountValue  int64      `json:"discountValue"`
	StartsAt       *time.Time `json:"startsAt"`
	EndsAt         *time.Time `json:"endsAt"`
	MaxRedemptions *int       `json:"maxRedemptions"`
	TicketTypeIDs  []string   `json:"ticketTypeIds"`
}

const campaignFrom = `SELECT c.id, c.event_id, c.name, c.code, c.token, c.discount_type, c.discount_value,
 c.starts_at, c.ends_at, c.max_redemptions, c.status, c.created_at, c.updated_at,
 ARRAY(SELECT ticket_type_id::text FROM campaign_ticket_types WHERE campaign_id = c.id ORDER BY 1),
 (SELECT COUNT(*) FROM orders o WHERE o.campaign_id = c.id AND o.status = 'paid'),
 (SELECT COUNT(*) FROM tickets t JOIN order_items oi ON oi.id = t.order_item_id
  JOIN orders o ON o.id = oi.order_id WHERE o.campaign_id = c.id AND t.status IN ('valid','used')),
 (SELECT COALESCE(SUM(subtotal_amount), 0)::bigint FROM orders WHERE campaign_id = c.id AND status = 'paid'),
 (SELECT COALESCE(SUM(discount_amount), 0)::bigint FROM orders WHERE campaign_id = c.id AND status = 'paid')
 FROM campaigns c JOIN events e ON e.id = c.event_id`

const campaignSelect = campaignFrom + `
 WHERE c.event_id = $1 AND e.organizer_id = $2`

func scanCampaign(row pgx.Row) (Campaign, error) {
	var c Campaign
	err := row.Scan(&c.ID, &c.EventID, &c.Name, &c.Code, &c.Token, &c.DiscountType, &c.DiscountValue,
		&c.StartsAt, &c.EndsAt, &c.MaxRedemptions, &c.Status, &c.CreatedAt, &c.UpdatedAt, &c.TicketTypeIDs,
		&c.Stats.Redemptions, &c.Stats.TicketsSold, &c.Stats.GrossAmount, &c.Stats.DiscountAmount)
	c.Stats.NetAmount = c.Stats.GrossAmount - c.Stats.DiscountAmount
	c.LinkPath = "/events/" + c.EventID + "?campaign=" + c.Token
	return c, notFound(err)
}

func (p *Postgres) ListCampaigns(ctx context.Context, organizerID, eventID string) ([]Campaign, error) {
	if _, err := p.EventByOwner(ctx, organizerID, eventID); err != nil {
		return nil, err
	}
	rows, err := p.pool.Query(ctx, campaignSelect+` ORDER BY c.created_at DESC, c.id`, eventID, organizerID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Campaign, error) { return scanCampaign(row) })
}

func (p *Postgres) campaign(ctx context.Context, q rowQuerier, organizerID, eventID, id string) (Campaign, error) {
	return scanCampaign(q.QueryRow(ctx, campaignSelect+` AND c.id = $3`, eventID, organizerID, id))
}

func setCampaignTicketTypes(ctx context.Context, tx pgx.Tx, eventID, campaignID string, ids []string) error {
	var matched int
	if err := tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM ticket_types WHERE event_id = $1 AND id = ANY($2::text[]::uuid[])`,
		eventID, ids).Scan(&matched); err != nil {
		return err
	}
	if matched != len(ids) {
		return ErrCampaignTicketTypes
	}
	if _, err := tx.Exec(ctx, `DELETE FROM campaign_ticket_types WHERE campaign_id = $1`, campaignID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx,
		`INSERT INTO campaign_ticket_types (campaign_id, ticket_type_id)
		 SELECT $1::uuid, unnest($2::text[]::uuid[])`, campaignID, ids)
	return err
}

func (p *Postgres) CreateCampaign(ctx context.Context, organizerID, actorID, eventID string, in CampaignInput) (Campaign, error) {
	var c Campaign
	err := p.withEditableEvent(ctx, organizerID, eventID, func(tx pgx.Tx) error {
		var id string
		if err := tx.QueryRow(ctx,
			`INSERT INTO campaigns (event_id, name, code, token, discount_type, discount_value,
			 starts_at, ends_at, max_redemptions)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id`,
			eventID, in.Name, rand.Text()[:10], "cmp_"+rand.Text(), in.DiscountType, in.DiscountValue,
			in.StartsAt, in.EndsAt, in.MaxRedemptions).Scan(&id); err != nil {
			return err
		}
		if err := setCampaignTicketTypes(ctx, tx, eventID, id, in.TicketTypeIDs); err != nil {
			return err
		}
		if err := writeAudit(ctx, tx, auditEntry{eventID: eventID, actorID: actorID, action: "campaign.created",
			entityType: "campaign", entityID: id, details: map[string]any{"name": in.Name}}); err != nil {
			return err
		}
		var err error
		c, err = p.campaign(ctx, tx, organizerID, eventID, id)
		return err
	})
	return c, err
}

func (p *Postgres) UpdateCampaign(ctx context.Context, organizerID, actorID, eventID, id string, in CampaignInput) (Campaign, error) {
	var c Campaign
	err := p.withEditableEvent(ctx, organizerID, eventID, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx,
			`UPDATE campaigns SET name = $3, discount_type = $4, discount_value = $5, starts_at = $6,
			 ends_at = $7, max_redemptions = $8, updated_at = now()
			 WHERE id = $1 AND event_id = $2`,
			id, eventID, in.Name, in.DiscountType, in.DiscountValue, in.StartsAt, in.EndsAt, in.MaxRedemptions)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		if err := setCampaignTicketTypes(ctx, tx, eventID, id, in.TicketTypeIDs); err != nil {
			return err
		}
		if err := writeAudit(ctx, tx, auditEntry{eventID: eventID, actorID: actorID, action: "campaign.updated",
			entityType: "campaign", entityID: id, details: map[string]any{"name": in.Name}}); err != nil {
			return err
		}
		c, err = p.campaign(ctx, tx, organizerID, eventID, id)
		return err
	})
	return c, err
}

func (p *Postgres) SetCampaignStatus(ctx context.Context, organizerID, actorID, eventID, id, status string) (Campaign, error) {
	var c Campaign
	err := pgx.BeginFunc(ctx, p.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx,
			`UPDATE campaigns c SET status = $4, updated_at = now() FROM events e
			 WHERE c.id = $1 AND c.event_id = $2 AND e.id = c.event_id AND e.organizer_id = $3`,
			id, eventID, organizerID, status)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		action := "campaign.disabled"
		if status == "active" {
			action = "campaign.enabled"
		}
		if err := writeAudit(ctx, tx, auditEntry{eventID: eventID, actorID: actorID, action: action,
			entityType: "campaign", entityID: id, details: map[string]any{}}); err != nil {
			return err
		}
		c, err = p.campaign(ctx, tx, organizerID, eventID, id)
		return err
	})
	return c, err
}

func (p *Postgres) Campaign(ctx context.Context, organizerID, eventID, id string) (Campaign, error) {
	return p.campaign(ctx, p.pool, organizerID, eventID, id)
}
