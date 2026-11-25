package store

import "testing"

func TestOrderIDPrefix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		give string
		want string
	}{
		{name: "short order number", give: "CB15E497", want: "cb15e497%"},
		{name: "full id", give: "cb15e497-a712-425d-8881-cf91c01fc43b", want: "cb15e497-a712-425d-8881-cf91c01fc43b%"},
		{name: "too short", give: "cb15e49", want: ""},
		{name: "email", give: "qa-att-c@example.com", want: ""},
		{name: "like wildcards", give: "cb15e497%", want: ""},
		{name: "empty", give: "", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := orderIDPrefix(tt.give); got != tt.want {
				t.Fatalf("orderIDPrefix(%q) = %q, want %q", tt.give, got, tt.want)
			}
		})
	}
}

func TestRefundDescriptionNamesOrder(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		give map[string]any
		want string
	}{
		{name: "with order", give: map[string]any{"amount": 6000, "orderId": "383ea4d8-d39c-4b6d-81a4-cd6f3beb637e"},
			want: "Demo refund of 6000 KZT issued for order 383EA4D8"},
		{name: "older entry without order", give: map[string]any{"amount": 6000}, want: "Demo refund of 6000 KZT issued"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := describeActivity("refund.succeeded", tt.give); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}
