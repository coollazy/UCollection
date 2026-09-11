package admin

import (
	"strings"
	"testing"

	"github.com/coollazy/UCollection/internal/audit"
)

func TestDescribeAuditLog(t *testing.T) {
	cases := []struct {
		name  string
		entry auditLogEntryView
		want  string
	}{
		{
			name: "illegal state transition by admin (override rejected)",
			entry: auditLogEntryView{Entry: audit.Entry{
				Actor: "admin", ActionType: "ILLEGAL_STATE_TRANSITION",
				Detail: map[string]any{"to_status": "COMPLETED", "note": "測試理由"},
			}},
			want: "被系統拒絕",
		},
		{
			name: "illegal state transition by system (late finality after terminal)",
			entry: auditLogEntryView{Entry: audit.Entry{
				Actor: "system", ActionType: "ILLEGAL_STATE_TRANSITION",
				Detail: map[string]any{
					"reason":       "late_finality_confirmation_after_terminal",
					"order_status": "COMPLETED", "tx_hash": "abc123",
					"event_amount": float64(30_000000), "confirmed_total_now": float64(30_000000),
				},
			}},
			want: "系統未自動處置，請人工查證",
		},
		{
			name: "consolidation prepared stage",
			entry: auditLogEntryView{Entry: audit.Entry{
				Actor: "admin", ActionType: "CONSOLIDATION",
				Detail: map[string]any{"stage": "prepared", "batch_id": float64(1), "order_id": float64(5), "amount": float64(30_000000), "tx_hash": "tx1"},
			}},
			want: "準備歸集",
		},
		{
			name: "consolidation broadcast failure includes reason",
			entry: auditLogEntryView{Entry: audit.Entry{
				Actor: "admin", ActionType: "CONSOLIDATION",
				Detail: map[string]any{"stage": "broadcast", "status": "failed", "order_id": float64(5), "tx_hash": "tx1", "error_detail": "INSUFFICIENT_ENERGY"},
			}},
			want: "失敗原因：INSUFFICIENT_ENERGY",
		},
		{
			name: "fee topup prepared stage uses TRX unit",
			entry: auditLogEntryView{Entry: audit.Entry{
				Actor: "admin", ActionType: "FEE_TOPUP",
				Detail: map[string]any{"stage": "prepared", "batch_id": float64(2), "order_id": float64(9), "amount": float64(13_045000), "tx_hash": "tx2"},
			}},
			want: "13.045 TRX",
		},
		{
			name: "consolidation batch created joins order ids",
			entry: auditLogEntryView{Entry: audit.Entry{
				Actor: "admin", ActionType: "CONSOLIDATION_BATCH_CREATED",
				Detail: map[string]any{"destination_address": "TDest", "order_ids": []any{float64(1), float64(2), float64(3)}},
			}},
			want: "涵蓋訂單 #1、2、3",
		},
		{
			name: "unrecognized action_type falls back to raw k=v dump",
			entry: auditLogEntryView{Entry: audit.Entry{
				Actor: "admin", ActionType: "SOME_FUTURE_ACTION_TYPE",
				Detail: map[string]any{"foo": "bar"},
			}},
			want: "foo=bar",
		},
		{
			name:  "nil detail on a recognized zero-detail action_type",
			entry: auditLogEntryView{Entry: audit.Entry{Actor: "admin", ActionType: "LOGOUT"}},
			want:  "登出",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := describeAuditLog(c.entry)
			if !strings.Contains(got, c.want) {
				t.Errorf("describeAuditLog() = %q, want substring %q", got, c.want)
			}
		})
	}
}
