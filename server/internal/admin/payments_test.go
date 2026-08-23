package admin

import (
	"context"
	"testing"

	"github.com/whatsapp-v2/server/internal/admin/domain"
	"github.com/whatsapp-v2/server/internal/platform/httpx"
)

type fakePayments struct {
	rows    []PaymentRow
	refunds []string
	err     error
}

func (f *fakePayments) AdminList(_ context.Context, status string, _ int) ([]PaymentRow, error) {
	if f.err != nil {
		return nil, f.err
	}
	if status == "" {
		return f.rows, nil
	}
	var out []PaymentRow
	for _, r := range f.rows {
		if r.Status == status {
			out = append(out, r)
		}
	}
	return out, nil
}

func (f *fakePayments) AdminRefund(_ context.Context, id string) error {
	if f.err != nil {
		return f.err
	}
	f.refunds = append(f.refunds, id)
	return nil
}

type fakeAudit struct {
	entries []string
	err     error
}

func (a *fakeAudit) AppendAudit(_ context.Context, e AuditEntry) error {
	if a.err != nil {
		return a.err
	}
	a.entries = append(a.entries, e.Actor+"|"+e.Action+"|"+e.Target+"|"+e.Reason)
	return nil
}

// payWho builds an admin identity with a known subject so the audit assertion
// can check attribution (the package's who() leaves the subject empty).
func payWho(role domain.Role) Identity { return Identity{Subject: "op@example.test", Role: role} }

func newPaymentsConsole() (*PaymentsConsole, *fakePayments, *fakeAudit) {
	be := &fakePayments{rows: []PaymentRow{
		{ID: "p1", Status: "succeeded", AmountCents: 499, Currency: "USD"},
		{ID: "p2", Status: "pending", AmountCents: 999, Currency: "USD"},
	}}
	au := &fakeAudit{}
	return NewPaymentsConsole(be, au), be, au
}

// Reading the ledger is support work: agent and above, not a read-only viewer.
func TestListRequiresAgent(t *testing.T) {
	c, _, _ := newPaymentsConsole()
	ctx := context.Background()

	if _, err := c.List(ctx, payWho(domain.RoleViewer), "", 10); statusOf(t, err) != 403 {
		t.Error("viewer must not read the payment ledger")
	}
	for _, role := range []domain.Role{domain.RoleAgent, domain.RoleOperator, domain.RoleOwner} {
		if _, err := c.List(ctx, payWho(role), "", 10); err != nil {
			t.Errorf("%v should be able to list payments: %v", role, err)
		}
	}
}

func TestListFiltersByStatus(t *testing.T) {
	c, _, _ := newPaymentsConsole()
	got, err := c.List(context.Background(), payWho(domain.RoleAgent), "pending", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "p2" {
		t.Fatalf("status filter did not apply: %+v", got)
	}
}

// A refund moves real money, so it is the highest-privilege action here.
func TestRefundIsOwnerOnly(t *testing.T) {
	c, be, _ := newPaymentsConsole()
	ctx := context.Background()

	for _, role := range []domain.Role{domain.RoleViewer, domain.RoleAgent, domain.RoleOperator} {
		if err := c.Refund(ctx, payWho(role), "p1", "duplicate charge"); statusOf(t, err) != 403 {
			t.Errorf("%v must not be able to refund", role)
		}
	}
	if len(be.refunds) != 0 {
		t.Fatal("a forbidden refund must not reach the payments service")
	}
	if err := c.Refund(ctx, payWho(domain.RoleOwner), "p1", "duplicate charge"); err != nil {
		t.Fatalf("owner should be able to refund: %v", err)
	}
	if len(be.refunds) != 1 || be.refunds[0] != "p1" {
		t.Fatalf("the refund did not reach the service: %+v", be.refunds)
	}
}

// Every refund must be attributable after the fact.
func TestRefundIsAudited(t *testing.T) {
	c, _, au := newPaymentsConsole()
	if err := c.Refund(context.Background(), payWho(domain.RoleOwner), "p1", "customer complaint #42"); err != nil {
		t.Fatal(err)
	}
	if len(au.entries) != 1 {
		t.Fatalf("expected one audit entry, got %d", len(au.entries))
	}
	want := "op@example.test|payment.refund|p1|customer complaint #42"
	if au.entries[0] != want {
		t.Fatalf("audit entry\n got: %s\nwant: %s", au.entries[0], want)
	}
}

func TestRefundRequiresReason(t *testing.T) {
	c, be, _ := newPaymentsConsole()
	if err := c.Refund(context.Background(), payWho(domain.RoleOwner), "p1", ""); statusOf(t, err) != 400 {
		t.Fatal("a refund without a reason must be rejected — the audit trail needs it")
	}
	if len(be.refunds) != 0 {
		t.Fatal("a rejected refund must not reach the payments service")
	}
}

// If the payments service refuses (e.g. the payment is not refundable), nothing
// should be written to the audit log.
func TestFailedRefundIsNotAudited(t *testing.T) {
	c, be, au := newPaymentsConsole()
	be.err = httpx.Reject(409, "STATE_NOT_REFUNDABLE", "nope")
	if err := c.Refund(context.Background(), payWho(domain.RoleOwner), "p1", "reason"); err == nil {
		t.Fatal("the service error should surface")
	}
	if len(au.entries) != 0 {
		t.Fatal("a refund that did not happen must not appear in the audit log")
	}
}
