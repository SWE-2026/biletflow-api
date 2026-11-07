package httpapi

import (
	"bytes"
	"net/http"

	"github.com/SWE-2026/biletflow-api/internal/store"
	"github.com/SWE-2026/biletflow-api/internal/tickets"
	"github.com/labstack/echo/v4"
)

var errNotPrintable = newError(http.StatusConflict, "ticket_not_printable", "Only valid tickets can be downloaded or printed.")

func sendFile(c echo.Context, contentType, filename string, data []byte) error {
	h := c.Response().Header()
	h.Set(echo.HeaderCacheControl, "no-store")
	if filename != "" {
		h.Set(echo.HeaderContentDisposition, `attachment; filename="`+filename+`"`)
	}
	return c.Blob(http.StatusOK, contentType, data)
}

func sendTicketPDF(c echo.Context, filename string, list []store.Ticket) error {
	var buf bytes.Buffer
	if err := tickets.WritePDF(&buf, list); err != nil {
		return err
	}
	return sendFile(c, "application/pdf", filename, buf.Bytes())
}

func (s *Server) ticketPDF(c echo.Context) error {
	id, err := pathID(c, "id")
	if err != nil {
		return err
	}
	t, err := s.db.UserTicket(c.Request().Context(), currentUser(c).ID, id)
	if err != nil {
		return err
	}
	if t.Status != "valid" {
		return errNotPrintable
	}
	return sendTicketPDF(c, "biletflow-ticket-"+t.ID+".pdf", []store.Ticket{t})
}

func (s *Server) orderTicketsPDF(c echo.Context) error {
	id, err := pathID(c, "id")
	if err != nil {
		return err
	}
	_, list, err := s.db.UserOrder(c.Request().Context(), currentUser(c).ID, id)
	if err != nil {
		return err
	}
	valid := make([]store.Ticket, 0, len(list))
	for _, t := range list {
		if t.Status == "valid" {
			valid = append(valid, t)
		}
	}
	if len(valid) == 0 {
		return errNotPrintable
	}
	return sendTicketPDF(c, "biletflow-order-"+id+".pdf", valid)
}

func (s *Server) ticketQR(c echo.Context) error {
	id, err := pathID(c, "id")
	if err != nil {
		return err
	}
	t, err := s.db.UserTicket(c.Request().Context(), currentUser(c).ID, id)
	if err != nil {
		return err
	}
	png, err := tickets.QRPNG(t.QRToken, 512)
	if err != nil {
		return err
	}
	return sendFile(c, "image/png", "", png)
}

func (s *Server) campaignQR(c echo.Context) error {
	id, campaignID, err := campaignPath(c)
	if err != nil {
		return err
	}
	campaign, err := s.db.Campaign(c.Request().Context(), organizerID(c), id, campaignID)
	if err != nil {
		return err
	}
	svg, err := tickets.CampaignSVG(s.opts.WebURL+campaign.LinkPath, campaign.Code, campaign.Name)
	if err != nil {
		return err
	}
	return sendFile(c, "image/svg+xml", "campaign-"+campaign.Code+".svg", svg)
}
