package store

// Web app paths used in notification links and emails (PUBLIC_WEB_URL + path). They must match the
// routes in biletflow-frontend/src/app/router/router.tsx; keep them all here so they change together.

func eventLink(eventID string) string { return "/events/" + eventID }

func orderLink(orderID string) string { return "/account/orders/" + orderID }

const payoutsLink = "/organizer/profile"

// Who opens a support case link decides the page: the person who opened the case, the organizer
// team handling an attendee case, or a platform admin.
type supportSide int

const (
	supportRequester supportSide = iota
	supportOrganizer
	supportAdmin
)

func supportLink(lc lockedCase, caseID string, side supportSide) string {
	switch {
	case side == supportAdmin:
		return "/admin/support/" + caseID
	case side == supportOrganizer && lc.eventID != nil:
		return "/organizer/events/" + *lc.eventID + "/support/" + caseID
	case lc.kind == "attendee" && side == supportRequester:
		return "/account/support/" + caseID
	default:
		return "/organizer/support/" + caseID
	}
}
