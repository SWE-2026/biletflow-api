package tickets

import (
	"fmt"
	"html"
	"strings"

	"github.com/skip2/go-qrcode"
)

const campaignColor = "#1d3a8a"

func QRPNG(content string, size int) ([]byte, error) {
	q, err := qrcode.New(content, qrcode.High)
	if err != nil {
		return nil, err
	}
	return q.PNG(size)
}

func CampaignSVG(link, code, name string) ([]byte, error) {
	q, err := qrcode.New(link, qrcode.Medium)
	if err != nil {
		return nil, err
	}
	bits := q.Bitmap()
	const cell, pad, header, footer = 8, 24, 56, 72
	side := len(bits) * cell
	width, height := side+2*pad, side+2*pad+header+footer
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d" role="img" aria-label="%s">`,
		width, height, width, height, html.EscapeString("Promotional QR code "+code+", not an admission ticket"))
	fmt.Fprintf(&b, `<rect width="%d" height="%d" fill="#ffffff"/>`, width, height)
	fmt.Fprintf(&b, `<rect x="4" y="4" width="%d" height="%d" fill="none" stroke="%s" stroke-width="6" stroke-dasharray="18 10" rx="16"/>`,
		width-8, height-8, campaignColor)
	fmt.Fprintf(&b, `<text x="%d" y="40" text-anchor="middle" font-family="sans-serif" font-size="22" font-weight="700" fill="%s">PROMO · NOT A TICKET</text>`,
		width/2, campaignColor)
	for y, row := range bits {
		for x, on := range row {
			if on {
				fmt.Fprintf(&b, `<rect x="%d" y="%d" width="%d" height="%d" fill="%s"/>`,
					pad+x*cell, header+pad+y*cell, cell, cell, campaignColor)
			}
		}
	}
	base := header + 2*pad + side
	fmt.Fprintf(&b, `<text x="%d" y="%d" text-anchor="middle" font-family="sans-serif" font-size="24" font-weight="700" fill="%s">%s</text>`,
		width/2, base+24, campaignColor, html.EscapeString(code))
	fmt.Fprintf(&b, `<text x="%d" y="%d" text-anchor="middle" font-family="sans-serif" font-size="16" fill="%s">%s</text>`,
		width/2, base+52, campaignColor, html.EscapeString(name))
	b.WriteString(`</svg>`)
	return []byte(b.String()), nil
}
