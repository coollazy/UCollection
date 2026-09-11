package consolidation

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/coollazy/UCollection/internal/audit"
)

// 手續費來源地址簿的 handlers（需求書5.9 v0.39）。與 handlers_addressbook.go 的
// 目的地地址簿 handlers 為兩套獨立路由，但共用其 addressBookPageData 頁面資料
// 結構、addressBookErrorMessage/addressBookErrorReason 錯誤短碼、totpReverified
// Notice——這些與地址簿用途無關、純粹是表單/錯誤呈現輔助，不重複一份。

func feeSourceBookPageHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		entries, err := ListFeeSourceBook(r.Context(), deps)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		render(w, http.StatusOK, "fee_source_address_book.html", addressBookPageData{
			Entries: entries,
			Error:   addressBookErrorMessage(r.URL.Query().Get("error")),
			Notice:  totpReverifiedNotice(r),
		})
	}
}

// feeSourceBookSubmitHandler implements POST /admin/consolidation/
// fee-source-book. A blank "id" field means create; a non-blank one means
// rename — 完全比照 addressBookSubmitHandler.
func feeSourceBookSubmitHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Redirect(w, r, "/admin/consolidation/fee-source-book?error=bad_request", http.StatusSeeOther)
			return
		}
		ctx := r.Context()
		idStr := r.PostFormValue("id")
		address := r.PostFormValue("address")
		label := r.PostFormValue("label")

		targetType := "fee_source_book_entry"
		var opErr error
		if idStr == "" {
			entry, err := CreateFeeSourceBookEntry(ctx, deps, address, label)
			opErr = err
			if err == nil {
				_ = audit.Log(ctx, deps.Pool, "admin", "FEE_SOURCE_BOOK_ENTRY_CREATED", &targetType, &entry.ID, map[string]any{
					"address": address,
					"label":   label,
				})
			}
		} else {
			id, parseErr := strconv.ParseInt(idStr, 10, 64)
			if parseErr != nil {
				http.Redirect(w, r, "/admin/consolidation/fee-source-book?error=bad_request", http.StatusSeeOther)
				return
			}
			opErr = RenameFeeSourceBookEntry(ctx, deps, id, label)
			if opErr == nil {
				_ = audit.Log(ctx, deps.Pool, "admin", "FEE_SOURCE_BOOK_ENTRY_RENAMED", &targetType, &id, map[string]any{
					"label": label,
				})
			}
		}

		if opErr != nil {
			http.Redirect(w, r, "/admin/consolidation/fee-source-book?error="+addressBookErrorReason(opErr), http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, "/admin/consolidation/fee-source-book", http.StatusSeeOther)
	}
}

// feeSourceBookDeleteHandler implements DELETE /admin/consolidation/
// fee-source-book/{id}, called by web/static/js/admin.js's
// deleteFeeSourceBookEntry() fetch (plain HTML forms can't submit DELETE).
func feeSourceBookDeleteHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		ctx := r.Context()
		if err := DeleteFeeSourceBookEntry(ctx, deps, id); err != nil {
			if errors.Is(err, errAddressBookEntryNotFound) {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		targetType := "fee_source_book_entry"
		_ = audit.Log(ctx, deps.Pool, "admin", "FEE_SOURCE_BOOK_ENTRY_DELETED", &targetType, &id, nil)
		w.WriteHeader(http.StatusOK)
	}
}
