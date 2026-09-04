package consolidation

import (
	"errors"
	"net/http"
	"strconv"
)

type addressBookPageData struct {
	Entries []AddressBookEntry
	Error   string
}

// addressBookErrorMessage maps a short "?error=" redirect code to its
// Chinese message — same short-code-in-URL pattern internal/auth uses
// (see its messages.go), keeping arbitrary text out of query strings.
func addressBookErrorMessage(reason string) string {
	switch reason {
	case "invalid_format":
		return "地址格式不正確"
	case "duplicate":
		return "此地址已在地址簿中"
	case "full":
		return "地址簿已達30筆上限"
	case "not_found":
		return "找不到指定的地址簿項目"
	case "bad_request":
		return "表單格式錯誤"
	default:
		return ""
	}
}

func addressBookErrorReason(err error) string {
	switch {
	case errors.Is(err, errInvalidAddressFormat):
		return "invalid_format"
	case errors.Is(err, errAddressAlreadyExists):
		return "duplicate"
	case errors.Is(err, errAddressBookFull):
		return "full"
	case errors.Is(err, errAddressBookEntryNotFound):
		return "not_found"
	default:
		return "bad_request"
	}
}

func addressBookPageHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		entries, err := ListAddressBook(r.Context(), deps)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		render(w, http.StatusOK, "consolidation_address_book.html", addressBookPageData{
			Entries: entries,
			Error:   addressBookErrorMessage(r.URL.Query().Get("error")),
		})
	}
}

// addressBookSubmitHandler implements POST /admin/consolidation/
// address-book. A blank "id" field means create; a non-blank one means
// rename (見本模組計畫「判斷點4」：兩者共用同一個路由，對應架構文件路由表只列出
// POST/DELETE 兩個HTTP方法).
func addressBookSubmitHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Redirect(w, r, "/admin/consolidation/address-book?error=bad_request", http.StatusSeeOther)
			return
		}
		ctx := r.Context()
		idStr := r.PostFormValue("id")
		address := r.PostFormValue("address")
		label := r.PostFormValue("label")

		var opErr error
		if idStr == "" {
			_, opErr = CreateAddressBookEntry(ctx, deps, address, label)
		} else {
			id, parseErr := strconv.ParseInt(idStr, 10, 64)
			if parseErr != nil {
				http.Redirect(w, r, "/admin/consolidation/address-book?error=bad_request", http.StatusSeeOther)
				return
			}
			opErr = RenameAddressBookEntry(ctx, deps, id, label)
		}

		if opErr != nil {
			http.Redirect(w, r, "/admin/consolidation/address-book?error="+addressBookErrorReason(opErr), http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, "/admin/consolidation/address-book", http.StatusSeeOther)
	}
}

// addressBookDeleteHandler implements DELETE /admin/consolidation/
// address-book/{id}, called by web/static/js/admin.js's fetch() (plain
// HTML forms can't submit DELETE — 見本模組計畫「判斷點2」).
func addressBookDeleteHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		if err := DeleteAddressBookEntry(r.Context(), deps, id); err != nil {
			if errors.Is(err, errAddressBookEntryNotFound) {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}
}
