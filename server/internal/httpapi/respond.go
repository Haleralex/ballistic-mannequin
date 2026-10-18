package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
)

// Error codes and the Russian messages shown to the user.
const (
	codeBadJSON       = "bad_json"
	codeBadType       = "unsupported_media_type"
	codeTooLarge      = "too_large"
	codeNotFound      = "not_found"
	codeMethod        = "method_not_allowed"
	codeRateLimited   = "rate_limited"
	codeInternal      = "internal"
	codeInvalid       = "invalid"
	codeUnavailable   = "unavailable"
	codeUnauthorized  = "unauthorized"
	msgBadJSON        = "Не удалось прочитать запрос: это не JSON."
	msgBadType        = "Запрос должен быть в формате JSON (Content-Type: application/json)."
	msgTooLarge       = "Слишком большой запрос."
	msgNotFound       = "Не найдено."
	msgMethod         = "Этот метод здесь не поддерживается."
	msgRateLimited    = "Слишком много запросов. Подождите немного и попробуйте снова."
	msgInternal       = "Что-то пошло не так на сервере. Попробуйте ещё раз чуть позже."
	msgUnavailable    = "Сервер временно недоступен."
	msgCaseNotFound   = "Такой реакции нет: возможно, ссылка неверная или устарела."
	msgVisitorID      = "Не удалось определить посетителя. Обновите страницу."
	msgSessionID      = "Не удалось определить сессию. Обновите страницу."
	msgUnauthorized   = "Нужен логин и пароль администратора."
	msgUnknownMetric  = "Неизвестная метрика."
	msgExperimentGone = "Такого эксперимента нет."
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

type apiError struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, apiError{Error: code, Message: msg})
}

func invalid(w http.ResponseWriter, code, msg string) {
	writeError(w, http.StatusUnprocessableEntity, code, msg)
}

// decodeJSON reads a JSON body of at most limit bytes into dst. It writes the error response itself and returns
// false on failure: 413 too large, 400 unreadable or wrong Content-Type, 422 a field of the wrong type.
// allowText also accepts text/plain (and no Content-Type), which is what navigator.sendBeacon sends.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any, limit int64, allowText bool) bool {
	ct := r.Header.Get("Content-Type")
	mt, _, err := mime.ParseMediaType(ct)
	switch {
	case ct == "" && allowText:
	case err == nil && mt == "application/json":
	case err == nil && mt == "text/plain" && allowText:
	default:
		writeError(w, http.StatusBadRequest, codeBadType, msgBadType)
		return false
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
	if err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			writeError(w, http.StatusRequestEntityTooLarge, codeTooLarge, msgTooLarge)
		} else {
			writeError(w, http.StatusBadRequest, codeBadJSON, msgBadJSON)
		}
		return false
	}
	if err := json.Unmarshal(body, dst); err != nil {
		var te *json.UnmarshalTypeError
		if errors.As(err, &te) {
			field := te.Field
			if field == "" {
				writeError(w, http.StatusBadRequest, codeBadJSON, "Запрос должен быть JSON-объектом.")
				return false
			}
			invalid(w, "invalid_"+strings.ReplaceAll(field, ".", "_"), fmt.Sprintf("Поле %s имеет неверный тип.", field))
			return false
		}
		writeError(w, http.StatusBadRequest, codeBadJSON, msgBadJSON)
		return false
	}
	return true
}
