package tickets

import (
	"bytes"
	_ "embed"
	"fmt"
	"io"
	"time"

	"github.com/SWE-2026/biletflow-api/internal/store"
	"github.com/go-pdf/fpdf"
)

//go:embed fonts/NotoSans-Regular.ttf
var regularFont []byte

//go:embed fonts/NotoSans-Bold.ttf
var boldFont []byte

const (
	pageWidth = 210.0
	margin    = 20.0
	qrSize    = 85.0
	family    = "NotoSans"
)

func WritePDF(w io.Writer, tickets []store.Ticket) error {
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetTitle("BiletFlow tickets", true)
	pdf.SetCreator("BiletFlow", true)
	pdf.SetMargins(margin, margin, margin)
	pdf.SetAutoPageBreak(false, margin)
	pdf.AddUTF8FontFromBytes(family, "", regularFont)
	pdf.AddUTF8FontFromBytes(family, "B", boldFont)
	for _, t := range tickets {
		if err := ticketPage(pdf, t); err != nil {
			return err
		}
	}
	if err := pdf.Error(); err != nil {
		return err
	}
	return pdf.Output(w)
}

func eventTime(t store.Ticket) string {
	loc, err := time.LoadLocation(t.Event.Timezone)
	if err != nil {
		loc = time.UTC
	}
	start, end := t.Event.StartsAt.In(loc), t.Event.EndsAt.In(loc)
	endFormat := "15:04"
	if start.YearDay() != end.YearDay() || start.Year() != end.Year() {
		endFormat = "Mon, 2 Jan 2006, 15:04"
	}
	return fmt.Sprintf("%s – %s (%s)", start.Format("Mon, 2 Jan 2006, 15:04"), end.Format(endFormat), t.Event.Timezone)
}

func ticketPage(pdf *fpdf.Fpdf, t store.Ticket) error {
	pdf.AddPage()
	width := pageWidth - 2*margin
	pdf.SetTextColor(0, 0, 0)
	pdf.SetDrawColor(0, 0, 0)
	pdf.SetFont(family, "B", 11)
	pdf.CellFormat(width, 6, "BILETFLOW · ADMISSION TICKET", "", 1, "L", false, 0, "")
	pdf.Ln(4)
	pdf.SetFont(family, "B", 22)
	pdf.MultiCell(width, 10, t.Event.Title, "", "L", false)
	pdf.Ln(2)
	field(pdf, width, "Date and time", eventTime(t))
	field(pdf, width, "Venue", t.Event.VenueName+"\n"+t.Event.VenueAddress)
	field(pdf, width, "Ticket type", t.TicketTypeName)
	field(pdf, width, "Attendee", t.AttendeeName)
	png, err := QRPNG(t.QRToken, 1024)
	if err != nil {
		return err
	}
	name := "qr-" + t.ID
	pdf.RegisterImageOptionsReader(name, fpdf.ImageOptions{ImageType: "PNG"}, bytes.NewReader(png))
	y := pdf.GetY() + 6
	x := (pageWidth - qrSize) / 2
	pdf.ImageOptions(name, x, y, qrSize, qrSize, false, fpdf.ImageOptions{ImageType: "PNG"}, 0, "")
	pdf.SetLineWidth(0.4)
	pdf.Rect(x-2, y-2, qrSize+4, qrSize+4, "D")
	pdf.SetXY(margin, y+qrSize+6)
	pdf.SetFont(family, "B", 11)
	pdf.CellFormat(width, 6, "Ticket ID: "+t.ID, "", 1, "C", false, 0, "")
	pdf.SetFont(family, "", 9)
	pdf.MultiCell(width, 5, "Show this QR code at the entrance. It admits one person once. "+
		"The printed and digital copies are the same ticket and share this ticket ID. "+
		"Do not share the QR code.", "", "C", false)
	return pdf.Error()
}

func field(pdf *fpdf.Fpdf, width float64, label, value string) {
	pdf.SetFont(family, "", 9)
	pdf.CellFormat(width, 5, label, "", 1, "L", false, 0, "")
	pdf.SetFont(family, "B", 13)
	pdf.MultiCell(width, 6.5, value, "", "L", false)
	pdf.Ln(2)
}
