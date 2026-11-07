package httpapi

import (
	"net/http"
	"strings"

	"github.com/SWE-2026/biletflow-api/internal/store"
	"github.com/labstack/echo/v4"
)

const maxCampaignTicketTypes = 50

func decodeCampaign(c echo.Context) (store.CampaignInput, error) {
	var in store.CampaignInput
	if err := decodeJSON(c, &in); err != nil {
		return in, err
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.TicketTypeIDs == nil {
		in.TicketTypeIDs = []string{}
	}
	for i := range in.TicketTypeIDs {
		in.TicketTypeIDs[i] = strings.ToLower(in.TicketTypeIDs[i])
	}
	return in, validateCampaign(in)
}

func validateCampaign(in store.CampaignInput) error {
	fe := fieldErrors{}
	fe.check(len(in.Name) > 0 && len(in.Name) <= 100, "name", "Enter a name (up to 100 characters).")
	fe.check(in.DiscountType == "percent" || in.DiscountType == "fixed", "discountType", "Choose percent or fixed.")
	switch in.DiscountType {
	case "percent":
		fe.check(in.DiscountValue >= 1 && in.DiscountValue <= 100, "discountValue", "Choose a percentage from 1 to 100.")
	case "fixed":
		fe.check(in.DiscountValue >= 1 && in.DiscountValue <= maxPriceAmount, "discountValue", "Choose an amount from 1 to 10000000 KZT.")
	}
	fe.check(in.StartsAt == nil || in.EndsAt == nil || in.EndsAt.After(*in.StartsAt), "endsAt", "End must follow start.")
	fe.check(in.MaxRedemptions == nil || (*in.MaxRedemptions >= 1 && *in.MaxRedemptions <= 1000000), "maxRedemptions", "Choose a limit from 1 to 1000000, or none.")
	fe.check(len(in.TicketTypeIDs) <= maxCampaignTicketTypes, "ticketTypeIds", "Choose up to 50 ticket types.")
	seen := map[string]bool{}
	for _, id := range in.TicketTypeIDs {
		fe.check(eventIDPattern.MatchString(id) && !seen[id], "ticketTypeIds", "Choose each ticket type once.")
		seen[id] = true
	}
	return fe.err()
}

func campaignPath(c echo.Context) (string, string, error) {
	id, err := eventID(c)
	if err != nil {
		return "", "", err
	}
	campaignID, err := pathID(c, "campaignId")
	return id, campaignID, err
}

func (s *Server) listCampaigns(c echo.Context) error {
	id, err := eventID(c)
	if err != nil {
		return err
	}
	campaigns, err := s.db.ListCampaigns(c.Request().Context(), organizerID(c), id)
	if err != nil {
		return err
	}
	if campaigns == nil {
		campaigns = []store.Campaign{}
	}
	return c.JSON(http.StatusOK, map[string]any{"campaigns": campaigns})
}

func (s *Server) createCampaign(c echo.Context) error {
	id, err := eventID(c)
	if err != nil {
		return err
	}
	in, err := decodeCampaign(c)
	if err != nil {
		return err
	}
	campaign, err := s.db.CreateCampaign(c.Request().Context(), organizerID(c), currentUser(c).ID, id, in)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, map[string]any{"campaign": campaign})
}

func (s *Server) updateCampaign(c echo.Context) error {
	id, campaignID, err := campaignPath(c)
	if err != nil {
		return err
	}
	in, err := decodeCampaign(c)
	if err != nil {
		return err
	}
	campaign, err := s.db.UpdateCampaign(c.Request().Context(), organizerID(c), currentUser(c).ID, id, campaignID, in)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"campaign": campaign})
}

func (s *Server) enableCampaign(c echo.Context) error { return s.setCampaignStatus(c, "active") }

func (s *Server) disableCampaign(c echo.Context) error { return s.setCampaignStatus(c, "disabled") }

func (s *Server) setCampaignStatus(c echo.Context, status string) error {
	id, campaignID, err := campaignPath(c)
	if err != nil {
		return err
	}
	campaign, err := s.db.SetCampaignStatus(c.Request().Context(), organizerID(c), currentUser(c).ID, id, campaignID, status)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"campaign": campaign})
}

func normalizePromoCode(code string) string { return strings.ToUpper(strings.TrimSpace(code)) }

func (s *Server) getPromo(c echo.Context) error {
	id, err := eventID(c)
	if err != nil {
		return err
	}
	code := normalizePromoCode(c.QueryParam("code"))
	token := strings.TrimSpace(c.QueryParam("campaign"))
	fe := fieldErrors{}
	fe.check((code == "") != (token == ""), "code", "Send either a promo code or a campaign token.")
	fe.check(len(code) <= 32 && len(token) <= 64, "code", "The code is too long.")
	if err := fe.err(); err != nil {
		return err
	}
	promo, err := s.db.PublicPromo(c.Request().Context(), id, code, token)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"promo": promo})
}
