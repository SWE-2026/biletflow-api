package store

import "testing"

func TestSupportLink(t *testing.T) {
	t.Parallel()
	event := "e1"
	attendeeCase := lockedCase{kind: "attendee", eventID: &event}
	organizerCase := lockedCase{kind: "organizer"}
	tests := []struct {
		name     string
		giveCase lockedCase
		giveSide supportSide
		want     string
	}{
		{name: "attendee opens own case", giveCase: attendeeCase, giveSide: supportRequester, want: "/account/support/c1"},
		{name: "organizer handles attendee case", giveCase: attendeeCase, giveSide: supportOrganizer, want: "/organizer/events/e1/support/c1"},
		{name: "admin assigned attendee case", giveCase: attendeeCase, giveSide: supportAdmin, want: "/admin/support/c1"},
		{name: "organizer opens own case", giveCase: organizerCase, giveSide: supportRequester, want: "/organizer/support/c1"},
		{name: "admin handles organizer case", giveCase: organizerCase, giveSide: supportAdmin, want: "/admin/support/c1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := supportLink(tt.giveCase, "c1", tt.giveSide); got != tt.want {
				t.Fatalf("supportLink() = %q, want %q", got, tt.want)
			}
		})
	}
}
